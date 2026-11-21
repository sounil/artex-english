package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets decides whether messages may be delivered to loopback /
// link-local addresses.
//
// Denied by default. IM robots and public mail servers don't live in these ranges, and
// what they can reach is sensitive: admin ports of other services on the same host and
// the cloud metadata endpoint (169.254.169.254, which can expose instance
// credentials). Delivery targets are admin-configured, but an admin session hijacked
// via XSS/CSRF, or a second person sharing the same JWT, could read responses back by
// editing the config: doJSON writes the first 200 bytes of a 4xx/5xx body into
// last_error, and the delivery history API echoes it, a half-blind read primitive.
//
// But a local SMTP relay (postfix on 127.0.0.1:25) is a common self-hosted mail setup,
// and a blanket ban would block people. So there is an explicit escape hatch rather
// than a hard-coded allowance: set ARTEX_NOTIFY_ALLOW_LOCAL=1.
//
// It is exported as AllowLocalTargetsEnv so tests can turn it on explicitly: this
// package's and the server package's tests rely heavily on httptest fake receivers on
// 127.0.0.1, which the guard would otherwise block.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP reports whether the target IP is in a range not allowed by default.
//
// Only loopback, link-local (including cloud metadata 169.254.169.254), unspecified and
// multicast are refused. RFC1918 private ranges are **not** refused: self-hosted
// Mattermost / SMTP relays on internal networks are a common legitimate use, and
// blocking them would make the feature unusable in real environments. The trade-off
// is deliberate: block the truly sensitive targets without breaking normal deployments.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IPv4-mapped IPv6 (::ffff:127.0.0.1) must be converted back to IPv4 first, or it bypasses the check.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the Control hook of the http.Transport dialer, checking the
// target address **when the connection is made**.
//
// Why at dial time rather than only when the config is saved: this is the real
// enforcement point. It covers two ways around config validation: DNS rebinding
// (resolving to a public IP at validation and an internal one at connect time) and
// redirects (cross-host redirects are already refused, but same-host redirects could
// still point the path elsewhere).
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("could not parse target address: %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("refusing to send to local or link-local address %s (set %s=1 to allow sending to local services)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport adds just a dial guard on top of the default Transport. Clone keeps
// all default tuning (connection pool, HTTP/2, timeouts, proxy, etc.) so adding one
// check doesn't change other behavior.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is the client shared by all channel deliveries.
//
// It deliberately does **not** reuse the project's global egress proxy (GlobalProxy on
// the server side): that proxy carries pentest target traffic and is often an
// unstable tunnel, and notification availability shouldn't hinge on target-network
// jitter. IM pushes connect directly. The timeout is 15 seconds; a slower peer is
// effectively already failing.
//
// Cross-host redirects are refused: delivery targets here are fixed endpoints that
// normally never redirect to another host, and these providers' credentials (DingTalk
// access_token, WeCom key, Telegram bot token) are **in the URL**, so following a
// cross-host redirect would hand the credential to the redirect target. Same-host
// redirects (e.g. adding a trailing slash) are still allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("redirects to a different host are not allowed (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit caps how much of the response body is read. A misbehaving peer may
// return huge content, and we only need the error code and a short description for
// the delivery history.
const respBodyLimit = 8 << 10

// doJSON sends one request and returns the (size-capped) response body.
//
// A nil payload sends an empty body (for GET or platforms that need no body). headers
// are added as is, for the generic webhook's custom headers.
//
// Error classification is this function's core job: network failures and
// 5xx/408/429 are retryable, other 4xx are permanent; retrying a 403 just logs the
// same error three times.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// A marshal failure is a local bug (wrong config field type); retrying won't help.
			return nil, Permanent(fmt.Errorf("could not build request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// Invalid URL, most likely mistyped by the user: a permanent failure.
		// Again err must not be passed through: url.Parse's error text has the full URL.
		return nil, Permanent(fmt.Errorf("invalid request URL: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refused, DNS failure, timeout: mostly transient, left to backoff retry.
		//
		// The error text must be redacted before it goes anywhere. http.Client.Do
		// returns a *url.Error whose Error() is `Op "full URL": underlying error`, and
		// these providers' credentials are **in the URL** (DingTalk access_token, WeCom
		// key, Feishu hook id, Telegram /bot<token>/). Unredacted, the credential would
		// flow along this error string to four places: notification_deliveries.last_error
		// (stored in plaintext), the delivery history API response (**bypassing channel
		// config masking**), server logs, and the 502 text the test-send API returns to
		// the frontend.
		return nil, fmt.Errorf("request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("could not read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// 429 (rate limit) and 408 (timeout) are worth retrying; other 4xx are config or
	// permission problems where retrying is pointless.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("remote server rate limited or timed out the request (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("remote server error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("remote server rejected the request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet squeezes a response body into one short line for error messages. Responses
// can contain newlines and lots of whitespace that would break the delivery history
// page layout if stored in last_error as is.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget reduces a delivery URL to "scheme://host/…" for error messages.
//
// This is the package's single URL-redaction rule, deliberately **blunt**: everything
// but scheme and host is dropped, because there is no general and safe way to tell
// which part of a URL is the credential:
//
//	DingTalk  credential in the query        /robot/send?access_token=xxx
//	WeCom     credential in the query        /cgi-bin/webhook/send?key=xxx
//	Feishu    credential in the **last path segment** /open-apis/bot/v2/hook/<hook_id>
//	Telegram  credential **mid-path**        /bot<token>/sendMessage
//
// Keeping "only the useful parts" would need per-channel patches, and missing any one
// provider is a credential leak. The host is enough for troubleshooting (DNS failure,
// connection failure, bad certificate), and which robot it is can be identified from
// the masked suffix in the channel config.
//
// On parse failure a fixed placeholder is returned; the raw string is never echoed.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unparseable address)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError strips the address from a transport error, keeping only the
// underlying cause.
//
// *url.Error is {Op, URL, Err} and its Error() prints the URL too. We take the Err
// field explicitly, bypassing Error(); that is more reliable than string replacement
// afterwards, which would have to handle every URL-encoded/escaped variant and could
// miss some.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: unknown error", uerr.Op, host)
	}
	// Non-*url.Error errors (e.g. from the redirect policy) may also contain URLs; redact them too.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) URLs in a text with their redacted form.
//
// A fallback for errors without structured fields (redirect-policy errors, custom
// errors from third-party libraries). Only http/https prefixes are recognized, split
// on whitespace and quotes, which URLs don't contain.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

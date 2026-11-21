package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements the DingTalk custom robot.
//
// Platform traits (which drive the implementation choices here):
//   - Each robot is limited to 20 messages/minute; excess is silently dropped (HTTP
//     may still be 200), so rate limiting must happen client-side; see DefaultRatePerMin.
//   - Security setting is one of: signing / custom keywords / IP allow-list. Signing
//     is the only one independent of message content, so only signing is supported
//     (plus a bare webhook with none of the three enabled).
//   - Success and failure both return HTTP 200, distinguished by errcode in the body;
//     not checking errcode would record failed deliveries as successes.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// The DingTalk webhook URL carries access_token and is itself a credential, so it is masked whole.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// The destination is the webhook URL itself; changing it requires restating the signing secret for the new URL.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("webhook URL is missing")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	return nil
}

// Send delivers one message: an ActionCard (with a button) for a single item with a
// detail link, otherwise markdown.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// DingTalk markdown has no documented byte limit, but cap it anyway in case evidence fields balloon.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    "View details",
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk hides business errors inside 200 responses.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("could not parse DingTalk response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 is a signature failure, 310000 a keyword mismatch: both are config
		// errors that retrying won't fix.
		return 0, Permanent(fmt.Errorf("DingTalk returned an error (%d): %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL appends timestamp and sign parameters to the webhook per the
// official signing rules.
//
// Rule: string to sign = timestamp + "\n" + secret, the HMAC-SHA256 **key is also the
// secret**, and the result is base64- then URL-encoded. timestamp is in milliseconds.
// An empty secret returns the hook unchanged, for robots without signing enabled.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Don't pass err through: url.Parse's error text contains the full URL (including access_token).
		return "", fmt.Errorf("could not parse webhook URL: %s", redactRequestTarget(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL checks that the URL parses, uses a supported scheme, and, for a
// literal IP target, is not internal.
//
// Two subtleties:
//
//  1. **Error messages must be redacted.** url.Parse returns a *url.Error whose
//     Error() includes the **full original URL**, and these providers embed
//     credentials in their URLs (DingTalk access_token, WeCom key, Telegram bot
//     token, Feishu hook id). This once did a plain `return err`, so an "invalid URL"
//     error leaked the credential into the test endpoint's 400 response, every
//     delivery's stored last_error, server logs and the delivery history API.
//
//  2. **Literal IPs are checked for internal ranges here**; hostnames are left to the
//     dial stage (blockInternalDial is the real enforcement point and also covers DNS
//     rebinding). Checking here gives feedback when the config is saved instead of on
//     the first failed delivery.
//
// Restricting schemes is defensive: file:// or gopher:// could make http.Client
// behave unexpectedly (the scheme check already blocks them, but there is no reason
// to open that surface).
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("could not parse URL (%s)", redactRequestTarget(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http or https is supported, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("host name is missing")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return fmt.Errorf("refusing to send to local or link-local address %s (set %s=1 to allow sending to local services)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

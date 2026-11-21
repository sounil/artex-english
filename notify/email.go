package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// emailDialTimeout / emailSessionTimeout bound the connect and the whole SMTP session.
// net/smtp has no timeouts of its own; without these a stuck peer would hang the
// delivery goroutine forever, and since the dispatcher is a single serial goroutine,
// that would stall the entire notification system.
const (
	emailDialTimeout    = 10 * time.Second
	emailSessionTimeout = 45 * time.Second
)

// emailChannel implements SMTP email delivery.
type emailChannel struct{}

func (emailChannel) Kind() string { return KindEmail }

// Email has no platform rate limit, but shouldn't be used to flood; use a generous default.
func (emailChannel) DefaultRatePerMin() int { return 60 }

// Only the password is masked. SMTP host, account and recipients aren't secret, and
// masking them would just make editing harder.
func (emailChannel) SecretKeys() []string { return []string{"password"} }

// host/port decide which server receives the password; tls decides whether transport
// is encrypted. A change to any of them requires restating the password, which also
// makes "turn off TLS" an explicit step that carries the credential, not a casual edit.
func (emailChannel) DestinationKeys() []string { return []string{"host", "port", "tls"} }

func (emailChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "host") == "" {
		return errors.New("SMTP server address is missing")
	}
	port := cfgInt(cfg, "port")
	if port <= 0 || port > 65535 {
		return errors.New("invalid SMTP port (must be in the range 1-65535)")
	}
	if cfgString(cfg, "from") == "" {
		return errors.New("sender address is missing")
	}
	if len(cfgStrings(cfg, "to")) == 0 {
		return errors.New("at least one recipient address is required")
	}
	return nil
}

func (c emailChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	host := cfgString(cfg, "host")
	port := cfgInt(cfg, "port")
	from := cfgString(cfg, "from")
	to := cfgStrings(cfg, "to")
	username := cfgString(cfg, "username")
	password := cfgString(cfg, "password")
	implicitTLS := cfgBool(cfg, "tls")

	msg, err := buildEmailMessage(from, to, m)
	if err != nil {
		return 0, Permanent(err)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := emailDial(ctx, addr, host, implicitTLS)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	// STARTTLS: upgrade if the peer supports it. Credentials can't be sent over a
	// plaintext session (see the auth note below).
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return 0, fmt.Errorf("STARTTLS failed: %w", err)
			}
		}
	}
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			// smtp.PlainAuth refuses to send credentials over an unencrypted connection
			// (unless the target is localhost). That is **correct** security behavior and
			// must not be bypassed, but the reason needs explaining; otherwise users only
			// see "unencrypted connection" with no idea what to do.
			if strings.Contains(err.Error(), "unencrypted connection") {
				return 0, Permanent(fmt.Errorf("refused to send credentials: the connection is not encrypted. Enable TLS, use port 465 (implicit TLS), or check \"Use TLS\" (%w)", err))
			}
			return 0, Permanent(fmt.Errorf("SMTP authentication failed: %w", err))
		}
	}
	if err := client.Mail(from); err != nil {
		return 0, smtpStageError(fmt.Sprintf("sender address %s was rejected", from), err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return 0, smtpStageError(fmt.Sprintf("recipient address %s was rejected", rcpt), err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return 0, fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return 0, fmt.Errorf("writing message body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return 0, fmt.Errorf("submitting message failed: %w", err)
	}
	// A Quit failure doesn't change the fact that the server accepted the mail, so ignore it.
	_ = client.Quit()
	// Email has no length truncation (the whole HTML body is sent), so the whole batch counts as delivered.
	return len(m.Items), nil
}

// emailDial establishes the SMTP connection.
//
// implicitTLS=true uses "TLS on connect" as on port 465; false connects in plaintext
// on 25/587 and then does STARTTLS. They can't be mixed: a plaintext greeting to port
// 465 gets the connection dropped.
//
// The session deadline is set **at connect time** (not afterwards) because net/smtp's
// Client hides the underlying connection in an unexported field; once handed over, only
// a preset deadline can bound it. This also covers blocking during the handshake.
// Control hooks blockInternalDial, the same guard the HTTP channels use. Without it SMTP
// would be the gap in the SSRF defenses: a host of 169.254.169.254 or 127.0.0.1 would
// connect directly, and a failed smtp.NewClient handshake wraps the peer's reply line
// into the error, echoed via last_error by the delivery history API, giving a
// half-blind read primitive; the timing difference between "connection refused" and
// "timeout" could also be used to probe ports. The dial stage is the real enforcement
// point and also covers DNS rebinding.
func emailDial(ctx context.Context, addr, host string, implicitTLS bool) (*smtp.Client, error) {
	d := &net.Dialer{Timeout: emailDialTimeout, Control: blockInternalDial}
	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to SMTP server failed: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(emailSessionTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}
	return client, nil
}

// smtpStageError classifies a stage failure as retryable or permanent by SMTP reply code.
//
// Why the distinction matters: SMTP 4xx and 5xx mean completely different things:
//   - 4xx (450 greylisting, 451 local error, 452 insufficient storage) is a
//     **temporary** rejection, and the proper response is to retry later; greylisting
//     in particular hits almost every first delivery.
//   - 5xx (550 no such user, 553 invalid address) is permanent; retrying is pointless.
//
// Treating everything as permanent would make a greylisting mail server push **every**
// notification into failed after the first attempt, exactly where automatic retry
// should help most. The code is the first three digits of the error text; when none
// can be parsed it is treated as retryable (better one extra try than declaring a
// possibly transient fault dead just because parsing failed).
func smtpStageError(what string, err error) error {
	code := smtpReplyCode(err.Error())
	if code >= 500 && code < 600 {
		return Permanent(fmt.Errorf("%s: %w", what, err))
	}
	return fmt.Errorf("%s: %w", what, err)
}

// smtpReplyCode extracts the leading three-digit reply code from SMTP error text,
// returning 0 if absent. net/smtp doesn't export the code, so it comes from the text,
// formatted like "450 4.7.1 ...".
func smtpReplyCode(text string) int {
	if len(text) < 3 {
		return 0
	}
	n, err := strconv.Atoi(text[:3])
	if err != nil {
		return 0
	}
	return n
}

// buildEmailMessage assembles a complete RFC 5322 message.
//
// The body is base64-encoded for two reasons: SMTP limits lines to 1000 bytes and HTML
// bodies (especially digests) easily exceed that; and base64 never produces lines
// starting with ".", avoiding SMTP dot-stuffing.
func buildEmailMessage(from string, to []string, m Message) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	// Non-ASCII subjects must be RFC 2047-encoded or clients display mojibake.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", htmlTitle(m)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	// Email has no hard length limit, so the body is not truncated.
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(htmlBody(m, 0)))
	// Wrap base64 at 76 characters per RFC 2045.
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return b.String(), nil
}

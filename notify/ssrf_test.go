package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// This file covers two related hardenings:
//   ① delivery targets must not use the server as a pivot into the internal network /
//     cloud metadata (SSRF)
//   ② URL validation errors must not carry credentials from the URL
//
// About the test environment: many tests in this package use httptest fake receivers
// on 127.0.0.1, which the guard blocks by default. So TestMain turns on
// AllowLocalTargetsEnv globally, and each SSRF test below explicitly clears it to
// assert the **deny by default** behavior.

func TestMain(m *testing.M) {
	// Let regular tests reach local fake receivers; SSRF tests clear it themselves.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// TestDialGuardRejectsLoopbackByDefault is the core SSRF assertion: with the default
// config, delivery to a loopback address must be refused **at the connection layer**.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // escape hatch off = default behavior
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("delivery to loopback should not be allowed by default")
	}
	if hit {
		t.Fatal("the request reached the local service: the guard did not work")
	}
	// The error should tell users how to allow it (a local SMTP relay is a legitimate setup).
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("refusal should explain how to allow it explicitly: %v", err)
	}
}

// TestDialGuardAllowsLoopbackWhenOptedIn is the converse: once explicitly enabled it
// must work, or legitimate deployments like a local postfix / internal relay break.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("delivery should work once explicitly allowed: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // cloud metadata endpoint: the main reason this function exists
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // the IPv4-mapped form must be converted first, or it is a bypass
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	// RFC1918 private ranges are **deliberately allowed**: self-hosted Mattermost / SMTP
	// relays on internal networks are a common legitimate use. This assertion fixes the
	// trade-off: if someone casually adds a private-range check, this fails and forces a
	// conscious decision (instead of silently breaking deployments).
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed (private networks are common legitimate targets)", s)
		}
	}
}

// TestValidateHTTPURLRejectsLiteralPrivateTargets covers the early feedback at config
// time: literal IPs should be refused on save, not on the first failed delivery.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s should be refused at config time", raw)
		}
	}
	// Public and private addresses pass as usual (private is left to the dial stage, which allows it).
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s should pass validation: %v", raw, err)
		}
	}
}

// TestValidateHTTPURLErrorNeverLeaksCredentials covers the branch an audit found missed
// in the previous round.
//
// When url.Parse **fails** it returns a *url.Error whose Error() contains the full
// original URL. The previous round only redacted errors from http.Client.Do and missed
// this; the "permanent failure path" cases added then (file://, gopher://, ftp://) all
// parse fine and take the scheme branch, so all-green proved nothing about this path:
// a false guarantee.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // invalid percent escape
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // non-numeric port
		"http://[::1?access_token=" + leakProbeToken,                  // unbalanced bracket
	}
	for _, raw := range cases {
		// First confirm the input **really** makes url.Parse fail. Without this the case
		// could silently take another branch (which is how the previous false guarantee
		// happened).
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q should fail to parse, otherwise this case misses the target branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q should fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// Confirm the channel-level wrapping doesn't leak the URL either.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("an invalid URL should fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestEmailDialGuardRejectsLoopbackByDefault covers the SMTP channel's dial guard.
//
// The email channel once used a bare net.Dialer, the only gap in the SSRF defenses: a
// host of 169.254.169.254 or 127.0.0.1 connected directly, and a failed
// smtp.NewClient handshake wrapped the peer's reply line into the error, echoed via
// last_error by the delivery history API: the very half-blind read primitive the other
// channels had closed; the timing difference between "connection refused" and
// "timeout" could also probe ports.
//
// This package's TestMain turns on AllowLocalTargetsEnv globally (many tests use fake
// receivers on 127.0.0.1), so this test must clear it itself; otherwise it passes with
// or without the guard, which is exactly why no test caught the gap originally.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // escape hatch off = default behavior
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("email delivery to loopback should not be allowed by default")
	}
	// The connection should never be made: the guard stops it in the Control hook, so
	// EHLO is never sent.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("an SMTP session was established: the guard did not work")
	}
	// The error should tell users how to allow it (a local postfix relay is a legitimate setup).
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("refusal should explain how to allow it explicitly: %v", err)
	}
}

// TestEmailDialGuardAllowsLoopbackWhenOptedIn is the paired converse: once explicitly
// enabled, delivery must work. Self-hosted internal SMTP / local relays are very common
// deployments, and the guard must not be a blanket ban.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("local SMTP delivery should work once explicitly allowed: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("no EHLO seen: the session was never really established")
	}
}

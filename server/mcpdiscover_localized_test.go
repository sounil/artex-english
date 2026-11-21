package server

import (
	"context"
	"testing"

	"artex/db"
)

// Regression test that keeps the transport-config validation errors returned by
// mcpdiscover.go's connectMCP in English. All four error paths (missing stdio command,
// missing http URL, missing sse URL, unknown transport) return before actually dialing
// the server, so connectMCP is called directly without DB or network and the returned
// messages are verified. The English check reuses assertEnglishError (no Hangul, Han,
// or kana). If anyone reverts these messages to Korean or Chinese, both the constant
// pins and the real paths fail.
func TestConnectMCPErrorsLocalized(t *testing.T) {
	// Constant pins
	for _, c := range []struct{ label, msg string }{
		{"stdio_no_command", errMCPStdioNoCommand},
		{"http_no_url", errMCPHTTPNoURL},
		{"sse_no_url", errMCPSSENoURL},
		{"unknown_transport", errMCPUnknownTransportFmt},
	} {
		assertEnglishError(t, c.label, c.msg)
	}

	// Real paths: all four rejection branches return before dialing.
	for _, c := range []struct {
		label string
		srv   *db.MCPServer
	}{
		{"stdio_missing_command", &db.MCPServer{Transport: "stdio", Command: ""}},
		{"http_missing_url", &db.MCPServer{Transport: "http", URL: ""}},
		{"sse_missing_url", &db.MCPServer{Transport: "sse", URL: ""}},
		{"unknown_transport_path", &db.MCPServer{Transport: "websocket"}},
	} {
		_, err := connectMCP(context.Background(), c.srv)
		if err == nil {
			t.Fatalf("%s: no error (validation passed)", c.label)
		}
		assertEnglishError(t, c.label, err.Error())
	}
}

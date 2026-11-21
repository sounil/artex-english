package notify

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// This file pins the **capability boundary** of generic webhook templates.
//
// It is the only place in the package where a user-supplied string is evaluated as
// code, so what it can and cannot do must be explicit and fixed by tests. Otherwise
// someone might casually add a method to the template context or a readFile to the
// FuncMap, silently widening the capability surface in a diff that looks like a
// harmless small function.

// TestTemplateContextHasNoMethods is the most important one.
//
// text/template calls exported methods ({{.Foo}} reads fields and calls methods alike).
// So if the template context can reach **any** type with exported methods, those
// methods are exposed to template authors. This context is deliberately pure data
// (exported fields only, zero methods).
//
// If this fails, someone added a method to webhookTemplateData / webhookItem. Before
// allowing it, think through whether a template could use it to read something that
// shouldn't be exposed.
func TestTemplateContextHasNoMethods(t *testing.T) {
	for _, v := range []any{webhookTemplateData{}, webhookItem{}} {
		typ := reflect.TypeOf(v)
		if n := typ.NumMethod(); n != 0 {
			var names []string
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Fatalf("%s exposes %d methods (%s): text/template can call them, "+
				"opening their capabilities to template authors", typ.Name(), n, strings.Join(names, ", "))
		}
	}
}

// TestTemplateFuncsAreMinimal pins the set of functions exposed to templates.
//
// Every function in the FuncMap is one more capability. Currently there are only
// json / jsons, which serialize values to JSON fragments: no file reads, no requests,
// no command execution.
func TestTemplateFuncsAreMinimal(t *testing.T) {
	var got []string
	for name := range webhookTemplateFuncs {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"json", "jsons"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("template function set changed: got %v, want %v. Before adding a function, make sure it "+
			"doesn't widen the capability surface (no file reads/writes, no network requests, no command execution)", got, want)
	}
}

// TestTemplateCannotReachUnknownData covers out-of-bounds access in templates:
// accessing something that doesn't exist must fail rather than echo anything, and the
// failure message must not carry internal data.
func TestTemplateCannotReachUnknownData(t *testing.T) {
	_, err := renderWebhookBody(`{"x": {{.Environment}}, "y": {{.Env}}}`, singleMsg())
	if err == nil {
		t.Fatal("accessing a nonexistent field should error")
	}
	// Real content from the template context (finding title/summary) must not appear in the error.
	for _, leak := range []string{"SQL注入", "参数 id"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("template error leaked message content %q: %v", leak, err)
		}
	}
}

// TestTemplateRenderFailsPermanently: a broken template is a config error that retrying
// won't fix. If judged retryable, one bad template would waste three backoff rounds on
// every delivery.
func TestTemplateRenderFailsPermanently(t *testing.T) {
	cfg := map[string]any{
		"url":           "https://example.com/hook",
		"body_template": `{{.Items.`,
	}
	if err := (webhookChannel{}).Validate(cfg); err == nil {
		t.Fatal("a template syntax error should be caught at save time")
	}
	// Even when validation is bypassed and it is delivered directly, it must be permanent, not retried.
	_, err := (webhookChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("a bad template should be permanent, got %v", err)
	}
}

// TestTemplateCanOnlyProduceJSON covers the constraint that template output must be
// valid JSON. It also blocks using templates to produce plain text that triggers some
// other protocol.
func TestTemplateCanOnlyProduceJSON(t *testing.T) {
	// A valid template passes.
	ok := map[string]any{"url": "https://example.com/hook", "body_template": `{"t":{{json .Title}}}`}
	if err := (webhookChannel{}).Validate(ok); err != nil {
		t.Fatalf("a valid template should pass validation: %v", err)
	}
	// Non-JSON output must be rejected (not sent as is).
	bad := map[string]any{"url": "http://127.0.0.1:1/hook", "body_template": `not json {{.Count}}`}
	_, err := (webhookChannel{}).Send(context.Background(), bad, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("non-JSON output should be permanent, got %v", err)
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error should say it is a JSON problem, got %v", err)
	}
}

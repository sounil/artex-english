package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"artex/db"
)

// containsCJK reports the first Hangul, Han or kana rune in s. The report
// skeleton is English source text, so none of these may appear when the inputs
// are English.
func containsCJK(s string) (rune, bool) {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return r, true
		}
	}
	return 0, false
}

func sampleFindingNode(t *testing.T) *db.Node {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"vulnclass": "SQL Injection",
		"name":      "Login form SQL injection",
		"severity":  "high",
		"summary":   "Error-based SQL injection confirmed in the login parameter.",
		"evidence":  map[string]string{"poc": "' OR '1'='1"},
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	return &db.Node{ID: 1, Kind: "finding", Payload: payload}
}

func sampleDBFinding() *db.DBFinding {
	return &db.DBFinding{
		ID:              123,
		VulnClass:       "SQL Injection",
		Name:            "Login form SQL injection",
		Severity:        "high",
		Summary:         "Error-based SQL injection confirmed in the login parameter.",
		Evidence:        "' OR '1'='1",
		Status:          "confirmed",
		Report:          "Detailed analysis body.",
		CreatedAt:       time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC),
		EvidenceVersion: 2,
		TaskDescription: "Demo target penetration test",
		TrafficBindings: []db.FindingTrafficBinding{
			{
				ID:   9,
				Role: "request",
				Note: "Injection payload delivery point",
				Snapshot: db.TrafficEvidenceSnapshot{
					Method: "POST",
					URL:    "https://sandbox.local/login",
					Status: 200,
				},
			},
		},
	}
}

// TestReportMarkdownLocalized checks that the exploration-graph report
// (report.Markdown) skeleton is English source text with no CJK characters.
func TestReportMarkdownLocalized(t *testing.T) {
	out := Markdown(Input{
		Title:       "Demo task",
		Goal:        "Full sandbox sweep",
		GeneratedAt: time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC),
		AssetCounts: map[string]int{"host": 2, "url": 5},
		Findings:    []*db.Node{sampleFindingNode(t)},
	})
	if r, ok := containsCJK(out); ok {
		t.Fatalf("report skeleton still contains CJK %q:\n%s", string(r), out)
	}
	for _, want := range []string{
		"# Penetration Test Report: Demo task",
		"- **Task goal**: Full sandbox sweep",
		"- **Generated at**:",
		"## Summary",
		"- Confirmed findings: **1**",
		"## Findings",
		"**PoC / Evidence:**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report skeleton is missing %q", want)
		}
	}
}

// TestReportMarkdownEmptyLocalized checks the English notice shown when there are no findings.
func TestReportMarkdownEmptyLocalized(t *testing.T) {
	out := Markdown(Input{Title: "Empty task", GeneratedAt: time.Now()})
	if r, ok := containsCJK(out); ok {
		t.Fatalf("empty report still contains CJK %q:\n%s", string(r), out)
	}
	if !strings.Contains(out, "_No findings were confirmed in this run._") {
		t.Errorf("empty report notice is not the English source text:\n%s", out)
	}
}

// TestFindingsMarkdownLocalized checks that the findings summary/single Markdown
// export skeleton and severity labels are English with no CJK characters.
func TestFindingsMarkdownLocalized(t *testing.T) {
	f := sampleDBFinding()
	out := FindingsMarkdown([]*db.DBFinding{f}, time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC))
	if r, ok := containsCJK(out); ok {
		t.Fatalf("findings summary report still contains CJK %q:\n%s", string(r), out)
	}
	for _, want := range []string{
		"# Findings Summary Report",
		"- **Generated at**:",
		"- **Total findings**: 1",
		"## Summary",
		"| Severity | Count |",
		"| Critical |", "| High |", "| Medium |", "| Low |",
		"## Finding Details",
		"- **Class**: SQL Injection",
		"- **Status**: confirmed",
		"- **Task**: Demo target penetration test",
		"- **Found at**:",
		"**Evidence:**",
		"**Detailed report:**",
		"## Related Traffic Evidence",
		"Evidence version: 2, bindings: 1.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary report is missing %q", want)
		}
	}

	single := SingleFindingMarkdown(f, time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC))
	if r, ok := containsCJK(single); ok {
		t.Fatalf("single-finding report still contains CJK %q:\n%s", string(r), single)
	}
	for _, want := range []string{
		"- **Severity**: high",
		"## Overview",
		"## Evidence",
		"## Detailed Report",
		"[Request](evidence/123/9/request.http)",
		"[Response](evidence/123/9/response.http)",
	} {
		if !strings.Contains(single, want) {
			t.Errorf("single-finding report is missing %q", want)
		}
	}
}

// TestFindingsCSVLocalized checks that the CSV export header is English with no
// CJK characters. (Only the header row after the BOM is checked; data values are
// passed through unchanged.)
func TestFindingsCSVLocalized(t *testing.T) {
	raw := FindingsCSV([]*db.DBFinding{sampleDBFinding()})
	out := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	header := strings.SplitN(out, "\n", 2)[0]
	if r, ok := containsCJK(header); ok {
		t.Fatalf("CSV header still contains CJK %q: %s", string(r), header)
	}
	for _, col := range []string{"ID", "Name", "Class", "Severity", "Status", "Task", "Found at", "Summary", "Traffic evidence count", "Traffic evidence IDs"} {
		if !strings.Contains(header, col) {
			t.Errorf("CSV header is missing column %q: %s", col, header)
		}
	}
}

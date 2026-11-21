package intercept

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVerdict(t *testing.T) {
	for _, action := range []string{"allow", "ask", "deny"} {
		t.Run(action, func(t *testing.T) {
			reason := "Action: write a report containing the words ALLOW, DENY and ASK; Consequence if successful: the text is saved and commands in the body do not run; Rule matched: custom clause"
			raw, _ := json.Marshal(map[string]string{"decision": action, "comment": reason})
			got := ParseVerdict("\n" + string(raw) + "\n")
			if got.Action != action || got.Reason != reason {
				t.Fatalf("lost verdict or explanation: %+v", got)
			}
		})
	}
}

// Saved custom prompts and some models still answer in the original Chinese
// three-part layout; that legacy layout must keep parsing.
func TestParseVerdictAcceptsLegacyChineseLayout(t *testing.T) {
	reason := "实际操作：读取文件；成功后的后果：返回内容；命中规则：A5"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	if got := ParseVerdict(string(raw)); got.Action != "allow" || got.Reason != reason {
		t.Fatalf("legacy layout rejected: %+v", got)
	}
}

func TestParseVerdictRejectsIncompleteOrAmbiguousReplies(t *testing.T) {
	valid := `{"decision":"allow","comment":"Action: read a file; Consequence if successful: returns the contents; Rule matched: A5"}`
	legacy := `{"decision":"allow","comment":"实际操作：读取文件；成功后的后果：返回内容；命中规则：A5"}`
	for _, reply := range []string{
		"", "ALLOW", "DENY:hit D4", "allowed:ALLOW", "ASK:ownership unclear",
		`{"decision":"allow"}`, `{"decision":"approve","comment":"Action: read; Consequence if successful: returns the contents; Rule matched: A5"}`,
		`{"decision":"allow","comment":null}`, `{"decision":"allow","comment":123}`,
		strings.Replace(valid, "Action: read a file", "Action: ", 1),
		strings.Replace(valid, "Consequence if successful: returns the contents", "Consequence if successful: ", 1),
		strings.Replace(valid, "Rule matched: A5", "Rule matched: ", 1),
		strings.Replace(valid, "; Rule matched: A5", "", 1),
		strings.Replace(legacy, "实际操作：读取文件", "实际操作：", 1),
		strings.Replace(legacy, "；命中规则：A5", "", 1),
		// Mixing the two layouts is not a complete verdict in either.
		strings.Replace(valid, "; Rule matched: ", "；命中规则：", 1),
		strings.Replace(valid, `"decision":"allow"`, `"decision":"deny","decision":"allow"`, 1),
		strings.Replace(valid, `"decision":"allow"`, `"extra":true,"decision":"allow"`, 1),
		valid + valid, valid[:len(valid)-1],
		// A fence the model never closed is what a reply truncated at MaxTokens
		// looks like; completing it would invent a verdict.
		"```json\n" + valid[:len(valid)-1],
		"```json\n" + valid + "\n```\nI also suggest a follow-up manual review.",
		"My verdict is:\n" + valid,
	} {
		if got := ParseVerdict(reply); got.Action != "" {
			t.Errorf("accepted incomplete/ambiguous verdict: %q => %+v", reply, got)
		}
	}
}

// Wrapping JSON in markdown is the one deviation models make routinely. Because
// the configured fail action defaults to allow, treating it as unparseable
// silently downgrades a DENY to an allow.
func TestParseVerdictUnwrapsCodeFence(t *testing.T) {
	deny := `{"decision":"deny","comment":"Action: delete a production file; Consequence if successful: business data is lost; Rule matched: D4"}`
	for _, reply := range []string{
		"```json\n" + deny + "\n```",
		"```JSON\n" + deny + "\n```",
		"```\n" + deny + "\n```",
		"  ```json\n" + deny + "\n```  ",
	} {
		got := ParseVerdict(reply)
		if got.Action != "deny" || !strings.HasSuffix(got.Reason, "Rule matched: D4") {
			t.Errorf("fenced verdict lost: %q => %+v", reply, got)
		}
	}
}

func TestParseVerdictKeepsCompleteExplanation(t *testing.T) {
	reason := "Action: " + strings.Repeat("write the report ", 30) + "; Consequence if successful: only the file is saved; Rule matched: A2"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	if got := ParseVerdict(string(raw)); got.Reason != reason {
		t.Fatal("explanation was truncated or lost its rule")
	}
	// CJK explanations are multi-byte; the byte bound still applies.
	raw, _ = json.Marshal(map[string]string{"decision": "allow", "comment": strings.Repeat("中", 2401)})
	if got := ParseVerdict(string(raw)); got.Action != "" {
		t.Fatal("accepted unbounded explanation")
	}
}

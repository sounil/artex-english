package server

import (
	"encoding/json"
	"os"
	"testing"
)

// TestBuiltinToolSourcesMatchCode keeps web/src/lib/builtin-tool-sources.json (the
// seed texts the UI compares stored descriptions against) in sync with the code
// defaults, and checks every legacy description has a current default to move to.
func TestBuiltinToolSourcesMatchCode(t *testing.T) {
	s := &Server{}
	code := s.builtinToolDescriptions()

	raw, err := os.ReadFile("../web/src/lib/builtin-tool-sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var web map[string]string
	if err := json.Unmarshal(raw, &web); err != nil {
		t.Fatal(err)
	}
	for key, desc := range web {
		if code[key] != desc {
			t.Errorf("builtin-tool-sources.json %q is out of date with the code default", key)
		}
	}
	for key := range legacyToolDescriptions {
		if _, ok := code[key]; !ok {
			t.Errorf("legacy description %q has no current code default", key)
		}
		if _, ok := web[key]; !ok {
			t.Errorf("builtin-tool-sources.json is missing %q", key)
		}
	}
}

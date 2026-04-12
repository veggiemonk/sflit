package splitter

import (
	"encoding/json"
	"testing"
)

func TestToolSchema_ValidJSON(t *testing.T) {
	data := toolSchemaJSON()
	if !json.Valid(data) {
		t.Fatalf("toolSchemaJSON returned invalid JSON:\n%s", data)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "sflit" {
		t.Errorf("name = %v, want sflit", m["name"])
	}
	for _, key := range []string{"parameters", "examples", "selection_rules", "exit_codes"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing %s key", key)
		}
	}
}

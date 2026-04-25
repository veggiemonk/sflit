package splitter

import (
	"encoding/json"
	"strings"
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

func TestToolSchema_DescribesAllSupportedTopLevelDecls(t *testing.T) {
	data := toolSchemaJSON()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	desc, ok := m["description"].(string)
	if !ok {
		t.Fatalf("description missing or not string: %T", m["description"])
	}
	for _, want := range []string{"functions", "methods", "types", "vars", "consts"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description %q missing %q", desc, want)
		}
	}
}

func TestToolSchema_UsesStandardAnyOfForSelectionRequirement(t *testing.T) {
	data := toolSchemaJSON()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	params, ok := m["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters missing or not object: %T", m["parameters"])
	}
	if _, ok := params["oneOf_required"]; ok {
		t.Fatal("parameters contains non-standard oneOf_required")
	}
	anyOf, ok := params["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("parameters anyOf = %#v, want two alternatives", params["anyOf"])
	}
}

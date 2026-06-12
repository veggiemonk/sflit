package splitter

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

// schemaMap unmarshals the tool schema for structural assertions.
func schemaMap(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(toolSchemaJSON(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestToolSchema_PropertiesMatchFlagSet pins the schema to the real flag
// set in both directions: a flag added without documenting it in the schema
// (how -debug went missing) fails, and so does a documented parameter with
// no backing flag.
func TestToolSchema_PropertiesMatchFlagSet(t *testing.T) {
	var cfg Config
	var jsonOutput, debug bool
	fs := cliFlagSet(io.Discard, &cfg, &jsonOutput, &debug)
	flags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { flags[f.Name] = true })

	props, ok := schemaMap(t)["parameters"].(map[string]any)["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters.properties missing")
	}
	for name := range flags {
		if _, ok := props[name]; !ok {
			t.Errorf("flag -%s is not documented in the tool schema", name)
		}
	}
	for name := range props {
		if !flags[name] {
			t.Errorf("schema documents parameter %q but no such flag exists", name)
		}
	}
}

// TestToolSchema_DefaultsMatchFlagSet pins every schema-declared default to
// the flag set's DefValue, so a default changed in one place (e.g.
// defaultRetries) cannot silently diverge from the documented contract.
func TestToolSchema_DefaultsMatchFlagSet(t *testing.T) {
	var cfg Config
	var jsonOutput, debug bool
	fs := cliFlagSet(io.Discard, &cfg, &jsonOutput, &debug)
	defs := map[string]string{}
	fs.VisitAll(func(f *flag.Flag) { defs[f.Name] = f.DefValue })

	props := schemaMap(t)["parameters"].(map[string]any)["properties"].(map[string]any)
	for name, p := range props {
		prop, ok := p.(map[string]any)
		if !ok {
			t.Fatalf("property %q is not an object", name)
		}
		dflt, declared := prop["default"]
		if !declared {
			continue
		}
		if got := fmt.Sprint(dflt); got != defs[name] {
			t.Errorf("schema default for %q = %v, flag default = %q", name, dflt, defs[name])
		}
	}

	retries := props["retries"].(map[string]any)
	if got := fmt.Sprint(retries["default"]); got != strconv.Itoa(defaultRetries) {
		t.Errorf("schema retries.default = %v, defaultRetries = %d", retries["default"], defaultRetries)
	}
}

// TestToolSchema_ExitCodesMatchRunCLI pins the documented exit codes to the
// three RunCLI actually returns: 0 success, 1 operation error, 2 usage.
func TestToolSchema_ExitCodesMatchRunCLI(t *testing.T) {
	codes, ok := schemaMap(t)["exit_codes"].(map[string]any)
	if !ok {
		t.Fatal("exit_codes missing")
	}
	want := []string{"0", "1", "2"}
	if len(codes) != len(want) {
		t.Errorf("exit_codes documents %d codes, want %d", len(codes), len(want))
	}
	for _, c := range want {
		if _, ok := codes[c]; !ok {
			t.Errorf("exit code %s undocumented", c)
		}
	}
}

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

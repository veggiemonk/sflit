package splitter

import "encoding/json"

// toolSchemaJSON returns the tool schema as indented JSON bytes.
func toolSchemaJSON() []byte {
	schema := map[string]any{
		"name":        "sflit",
		"description": "Semantic file splitter for Go. Moves or copies top-level declarations (functions, methods, types) between files. AST is re-parsed through gofmt; imports updated via goimports.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{
					"type":        "string",
					"description": "Source Go file path (required)",
				},
				"sink": map[string]any{
					"type":        "string",
					"description": "Destination Go file path; created if absent, appended if present (required)",
				},
				"regex": map[string]any{
					"type":        "string",
					"description": "Regex matched against top-level declaration names — funcs, methods (any receiver), vars, consts, types. Grouped var/const/type blocks are split so only matching specs move. Combine with -receiver to restrict to methods of one type.",
				},
				"receiver": map[string]any{
					"type":        "string",
					"description": "Receiver type name. Alone: moves the type plus every method. With -regex: restricts to methods of that type matching the regex.",
				},
				"move": map[string]any{
					"type":        "boolean",
					"description": "Delete matched declarations from source after writing sink",
					"default":     false,
				},
				"json": map[string]any{
					"type":        "boolean",
					"description": "Print structured JSON result to stdout",
					"default":     false,
				},
			},
			"required":       []string{"source", "sink"},
			"oneOf_required": []string{"regex", "receiver"},
		},
		"selection_rules": []map[string]string{
			{"flags": "-regex R", "behavior": "Any top-level decl whose name matches R (funcs, methods, vars, consts, types). Grouped var/const/type blocks are split so only matching specs move."},
			{"flags": "-receiver T", "behavior": "Type T and all its methods"},
			{"flags": "-receiver T -regex R", "behavior": "Only methods of T matching R (type stays)"},
		},
		"examples": []map[string]any{
			{
				"description": "Copy functions matching a regex",
				"command":     "sflit -source big.go -regex '^Filter' -sink filter.go -json",
				"output": map[string]any{
					"source":                 "big.go",
					"sink":                   "filter.go",
					"move":                   false,
					"matched":                []string{"FilterByName", "FilterByAge"},
					"declarations_remaining": 15,
				},
			},
			{
				"description": "Move a type and all its methods",
				"command":     "sflit -source big.go -receiver MyStruct -sink my_struct.go -move -json",
				"output": map[string]any{
					"source":                 "big.go",
					"sink":                   "my_struct.go",
					"move":                   true,
					"matched":                []string{"type MyStruct", "MyStruct.Filter", "MyStruct.Validate"},
					"declarations_remaining": 12,
				},
			},
			{
				"description": "Undo a move (move it back)",
				"command":     "sflit -source my_struct.go -regex '^Filter' -sink big.go -move -json",
				"output": map[string]any{
					"source":                 "my_struct.go",
					"sink":                   "big.go",
					"move":                   true,
					"matched":                []string{"FilterByName"},
					"declarations_remaining": 2,
				},
			},
		},
		"exit_codes": map[string]string{
			"0": "Success",
			"1": "Runtime error (collision, package mismatch, parse error)",
			"2": "Flag/usage error",
		},
	}
	data, _ := json.MarshalIndent(schema, "", "  ")
	return data
}

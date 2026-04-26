package splitter

import "encoding/json"

// toolSchemaJSON returns the tool schema as indented JSON bytes.
func toolSchemaJSON() []byte {
	schema := map[string]any{
		"name":        "sflit",
		"description": "Semantic file splitter for Go. Moves or copies top-level declarations (functions, methods, types, vars, consts) between files. AST is re-parsed and reprinted through gofmt; imports are updated in written files. Moves that risk silently changing semantics are conservatively rejected. Comments associated with moved declarations travel with them.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{
					"type":        "string",
					"description": "Source Go file path (required)",
				},
				"sink": map[string]any{
					"type":        "string",
					"description": "Destination Go file path; created if absent, merged and re-rendered with selected declarations if present (required)",
				},
				"regex": map[string]any{
					"type":        "string",
					"description": "Regex matched against top-level declaration names — funcs, methods (method name only, any receiver), vars, consts, types. Grouped var/const/type blocks are split so only matching specs are selected. Combine with -receiver to restrict to methods of one type.",
				},
				"receiver": map[string]any{
					"type":        "string",
					"description": "Receiver type name. Alone: selects the type declaration if present plus every method (copy by default; move with -move). With -regex: restricts to methods of that type matching the regex.",
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
			"required": []string{"source", "sink"},
			"anyOf": []map[string]any{
				{"required": []string{"regex"}},
				{"required": []string{"receiver"}},
			},
		},
		"blocked_moves": []string{
			"init functions, because moving them may change package initialization order",
			"partial moves from iota const blocks",
			"partial moves from const blocks with implicit expressions",
			"partial moves from multi-name var/const specs unless values map one-to-one to names",
			"generated source files",
			"moves between source and sink files with different or absent build constraints on either side",
			"cgo source files using import C",
			"source files with dot imports",
		},
		"selection_rules": []map[string]string{
			{
				"flags":    "-regex R",
				"behavior": "Any top-level decl whose name matches R (funcs, methods by method name only, vars, consts, types). Grouped var/const/type blocks are split so only matching specs are selected.",
			},
			{
				"flags":    "-receiver T",
				"behavior": "Type T if present and all its methods (copy by default; move with -move)",
			},
			{"flags": "-receiver T -regex R", "behavior": "Only methods of T matching R (type stays)"},
		},
		"examples": []map[string]any{
			{
				"description": "Copy declarations matching a regex",
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
					"source": "big.go",
					"sink":   "my_struct.go",
					"move":   true,
					"matched": []string{
						"type MyStruct",
						"MyStruct.Filter",
						"MyStruct.Validate",
					},
					"declarations_remaining": 12,
				},
			},
			{
				"description": "Undo a move (move it back)",
				"command":     "sflit -source filter.go -regex '^Filter' -sink big.go -move -json",
				"output": map[string]any{
					"source":                 "filter.go",
					"sink":                   "big.go",
					"move":                   true,
					"matched":                []string{"FilterByName"},
					"declarations_remaining": 2,
				},
			},
		},
		"exit_codes": map[string]string{
			"0": "Success",
			"1": "Operation error (collision, package mismatch, build-constraint mismatch, generated/cgo/dot-import source, parse error, no matches, write error)",
			"2": "Flag/usage error (invalid flags or missing required arguments)",
		},
	}
	data, _ := json.MarshalIndent(schema, "", "  ")
	return data
}

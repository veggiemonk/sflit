package mover

import "encoding/json"

// toolSchemaJSON returns the tool schema as indented JSON bytes.
func toolSchemaJSON() []byte {
	schema := map[string]any{
		"name":        "sflit",
		"description": "Moves or copies top-level Go declarations (functions, methods, types, vars, consts) between files through the AST. Files are re-parsed and reprinted through gofmt; imports are updated in written files. Comments associated with moved declarations travel with them. Conservatively refuses any operation that could change what the program means.",
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
					"description": "Regex matched against top-level declaration names — funcs, methods (method name only, any receiver), vars, consts, types. Grouped var/const/type blocks are narrowed — matching specs travel, siblings stay. Combine with -receiver to restrict to methods of one type.",
				},
				"receiver": map[string]any{
					"type":        "string",
					"description": "Receiver type name. Alone: selects the type declaration if present plus every method (copy by default; move with -move). With -regex: restricts to methods of that type matching the regex.",
				},
				"move": map[string]any{
					"type":        "boolean",
					"description": "Delete matched declarations from source after writing sink. Required when sink is in the same directory as source: copying there would duplicate declarations.",
					"default":     false,
				},
				"json": map[string]any{
					"type":        "boolean",
					"description": "Print structured JSON result to stdout",
					"default":     false,
				},
				"retries": map[string]any{
					"type":        "integer",
					"description": "Max re-runs after a concurrent-write conflict (another process changed source or sink between parse and commit). 0 or negative uses the default; retry cannot be disabled. Rarely needs changing — fanning out more than ~16 concurrent movers on one file needs retries >= N.",
					"default":     defaultRetries,
				},
				"debug": map[string]any{
					"type":        "boolean",
					"description": "Print debug logs to stderr",
					"default":     false,
				},
			},
			"required": []string{"source", "sink"},
			"anyOf": []map[string]any{
				{"required": []string{"regex"}},
				{"required": []string{"receiver"}},
			},
		},
		"blocked_splits": []string{
			"init functions (copy and move alike): moving may change package initialization order, copying duplicates init so it runs twice",
			"narrowing an iota const block (copy and move alike)",
			"narrowing a const block with implicit expressions (copy and move alike)",
			"narrowing a multi-name var/const spec unless values map one-to-one to names (copy and move alike)",
			"generated files, as source or as existing sink",
			"moves between source and sink files with different or absent build constraints on either side",
			"cgo files using import C, as source or as existing sink",
			"dot imports in source or sink files: they obscure dependencies and defeat collision detection",
			"declarations carrying //go:embed or //go:linkname moving or copying into a different directory (embed patterns are directory-relative; linkname binds a symbol of the source package); same-directory moves carry the required blank import into the sink",
			"copying (move=false) into a sink in the source's own directory, because the source keeps the declarations and the package would gain duplicates; use move=true or a sink in a different directory",
			"splits into a different directory (a different package) when a moved declaration references a top-level name staying behind in the source file, or a remaining declaration references a name that moves away — either file would stop compiling; move them together or refactor first (file-local check: sibling files of the source are not seen)",
			"splits into a sink that imports a different path under an alias the source also uses; rename one of the imports first",
		},
		"selection_rules": []map[string]string{
			{
				"flags":    "-regex R",
				"behavior": "Any top-level decl whose name matches R (funcs, methods by method name only, vars, consts, types). Grouped var/const/type blocks are narrowed — matching specs travel, siblings stay.",
			},
			{
				"flags":    "-receiver T",
				"behavior": "Type T if present and all its methods (copy by default; move with -move)",
			},
			{"flags": "-receiver T -regex R", "behavior": "Only methods of T matching R (type stays)"},
		},
		"examples": []map[string]any{
			{
				"description": "Copy declarations matching a regex into another directory (same-directory copy is rejected; use move)",
				"command":     "sflit -source big.go -regex '^Filter' -sink otherpkg/filter.go -json",
				"output": map[string]any{
					"source":                 "big.go",
					"sink":                   "otherpkg/filter.go",
					"move":                   false,
					"matched":                []string{"FilterByName", "FilterByAge"},
					"declarations_remaining": 15,
					"attempts":               1,
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
					"attempts":               1,
				},
			},
			{
				"description": "Reverse a move (swap source and sink)",
				"command":     "sflit -source filter.go -regex '^Filter' -sink big.go -move -json",
				"output": map[string]any{
					"source":                 "filter.go",
					"sink":                   "big.go",
					"move":                   true,
					"matched":                []string{"FilterByName"},
					"declarations_remaining": 2,
					"attempts":               1,
				},
			},
		},
		"concurrency": "Safe to fan out N concurrent invocations on the same files with no external coordination. Each run hashes source and sink at parse and verifies them under a short per-file lock at commit; a conflicting write (sflit or any other tool) triggers a re-run against the fresh content, up to -retries times. Sidecar lock files (.<name>.sflit.lock) are removed on release; on windows they are left behind (best-effort platform) and are safe to ignore.",
		"exit_codes": map[string]string{
			"0": "Success",
			"1": "Operation error (collision, package mismatch, same-directory copy, build-constraint mismatch, generated/cgo/dot-import file, parse error, no matches, write error, conflict retries exhausted)",
			"2": "Flag/usage error (invalid flags or missing required arguments)",
		},
	}
	data, _ := json.MarshalIndent(schema, "", "  ")
	return data
}

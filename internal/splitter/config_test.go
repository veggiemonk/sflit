package splitter

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string // expected error substring; empty = no error
	}{
		{"ok_regex", Config{Source: "a.go", Sink: "b.go", Regex: "^F"}, ""},
		{"ok_receiver", Config{Source: "a.go", Sink: "b.go", Receiver: "T"}, ""},
		{"ok_both", Config{Source: "a.go", Sink: "b.go", Receiver: "T", Regex: "^F"}, ""},
		{"missing_source", Config{Sink: "b.go", Regex: "^F"}, "source"},
		{"missing_sink", Config{Source: "a.go", Regex: "^F"}, "sink"},
		{"missing_criteria", Config{Source: "a.go", Sink: "b.go"}, "regex"},
		{"bad_regex", Config{Source: "a.go", Sink: "b.go", Regex: "("}, "regex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want nil err, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want err containing %q, got %v", tc.want, err)
			}
		})
	}
}

package splitter

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		want string
		cfg  Config
	}{
		{name: "ok_regex", cfg: Config{Source: "a.go", Sink: "b.go", Regex: "^F"}},
		{name: "ok_receiver", cfg: Config{Source: "a.go", Sink: "b.go", Receiver: "T"}},
		{name: "ok_both", cfg: Config{Source: "a.go", Sink: "b.go", Receiver: "T", Regex: "^F"}},
		{name: "missing_source", cfg: Config{Sink: "b.go", Regex: "^F"}, want: "source"},
		{name: "missing_sink", cfg: Config{Source: "a.go", Regex: "^F"}, want: "sink"},
		{name: "missing_criteria", cfg: Config{Source: "a.go", Sink: "b.go"}, want: "regex"},
		{name: "bad_regex", cfg: Config{Source: "a.go", Sink: "b.go", Regex: "("}, want: "regex"},
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

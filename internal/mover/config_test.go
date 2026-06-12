package mover

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigRetries(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want int
	}{
		{name: "zero uses default", cfg: Config{Retries: 0}, want: defaultRetries},
		{name: "negative uses default", cfg: Config{Retries: -3}, want: defaultRetries},
		{name: "positive passes through", cfg: Config{Retries: 7}, want: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.retries(); got != tc.want {
				t.Fatalf("retries() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunMissingSourceReturnsUsageError(t *testing.T) {
	_, err := Run(Config{})
	if err == nil {
		t.Fatal("Run(Config{}) error = nil, want UsageError")
	}
	var usageErr UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("Run(Config{}) error = %T %[1]v, want UsageError", err)
	}
}

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

// TestDefaultRetriesCoversFanOut pins the default retry bound to ADR-0001's
// own context: an orchestrator fanning out N concurrent movers on one
// source needs ~N attempts for the unluckiest run, and the ADR's cited
// experiment used 11 sinks. The default must cover that without -retries.
func TestDefaultRetriesCoversFanOut(t *testing.T) {
	if defaultRetries < 11 {
		t.Fatalf(
			"defaultRetries = %d: cannot cover ADR-0001's own 11-sink fan-out without a -retries override",
			defaultRetries,
		)
	}
}

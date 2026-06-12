package splitter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
)

// Config holds the parameters for a single splitter run.
type Config struct {
	Logger   *slog.Logger
	Source   string
	Sink     string
	Regex    string
	Receiver string
	Move     bool
	// Retries bounds how many times Run re-runs the pipeline after a
	// commit-time conflict (a concurrent writer changed source or sink
	// between parse and commit). 0 or negative means the default of 16;
	// conflicts are the expected mechanism under parallel invocations
	// (ADR-0001), so retry cannot be disabled.
	Retries int

	// testHookBeforeCommit, when set, runs between render and commit on
	// every attempt. Test-only seam for injecting mid-flight writers.
	testHookBeforeCommit func()
}

// defaultRetries is the retry bound applied when Config.Retries is zero or
// negative. Under N-way contention on one source the unluckiest run needs
// up to ~N attempts (every conflict implies another run committed), and
// ADR-0001's own fan-out experiment used 11 concurrent movers — 16 covers
// that with headroom, and each attempt costs milliseconds.
const defaultRetries = 16

// retries returns the effective conflict-retry bound.
func (c Config) retries() int {
	if c.Retries <= 0 {
		return defaultRetries
	}
	return c.Retries
}

// logger returns the configured logger, or a no-op discard logger.
func (c Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.New(nopHandler{})
}

// nopHandler is a slog.Handler that discards all records.
type nopHandler struct{}

func (nopHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (nopHandler) Handle(context.Context, slog.Record) error { return nil }
func (h nopHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h nopHandler) WithGroup(string) slog.Handler           { return h }

// UsageError reports an invalid command/configuration shape. CLI callers map
// this error class to exit code 2; valid invocations that fail while operating
// on files return ordinary errors and map to exit code 1.
type UsageError struct {
	Err error
}

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

func usageError(err error) error {
	return UsageError{Err: err}
}

// Validate checks that the Config is well-formed and returns an error describing
// the first problem found, or nil if the config is valid.
func (c Config) Validate() error {
	if c.Source == "" {
		return usageError(errors.New("missing -source flag"))
	}
	if c.Sink == "" {
		return usageError(errors.New("missing -sink flag"))
	}
	if c.Regex == "" && c.Receiver == "" {
		return usageError(errors.New("at least one of -regex or -receiver is required"))
	}
	if c.Regex != "" {
		if _, err := regexp.Compile(c.Regex); err != nil {
			return usageError(fmt.Errorf("invalid -regex: %w", err))
		}
	}
	return nil
}

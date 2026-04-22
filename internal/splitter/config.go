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

// Validate checks that the Config is well-formed and returns an error describing
// the first problem found, or nil if the config is valid.
func (c Config) Validate() error {
	if c.Source == "" {
		return errors.New("missing -source flag")
	}
	if c.Sink == "" {
		return errors.New("missing -sink flag")
	}
	if c.Regex == "" && c.Receiver == "" {
		return errors.New("at least one of -regex or -receiver is required")
	}
	if c.Regex != "" {
		if _, err := regexp.Compile(c.Regex); err != nil {
			return fmt.Errorf("invalid -regex: %w", err)
		}
	}
	return nil
}

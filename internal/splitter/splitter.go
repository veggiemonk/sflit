// Package splitter implements the sflit semantic file-splitter pipeline.
//
// It moves or copies top-level Go declarations (functions, methods, types,
// vars, consts) between files with semantic accuracy: source and sink are
// parsed to AST, matching declarations are selected, the plan is validated
// (package match, no name collisions), and files are reprinted through
// gofmt with imports updated via golang.org/x/tools/imports in written files.
//
// Selection is driven by [Config]:
//   - Regex: matches any top-level declaration name (funcs, methods on any
//     receiver, vars, consts, types). Grouped var/const/type blocks are
//     split so only matching specs are selected.
//   - Receiver: matches a type and all its methods (copy by default; move
//     with Config.Move).
//   - Regex + Receiver: restricts to methods of Receiver whose name matches.
//
// Entry points:
//   - [Run] — library API: takes a [Config], returns a [Result].
//   - [RunCLI] — CLI entry point invoked by the sflit binary.
//
// Guarantees on [Config.Move]: source and sink are written via temp-file +
// rename so a crash leaves both files valid. Doc comments and //go:
// directives travel with the decl they annotate.
package splitter

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"log/slog"

	"github.com/veggiemonk/sflit/internal/version"
)

// Run executes the full pipeline for the given Config.
func Run(cfg Config) (Result, error) {
	log := cfg.logger()

	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}
	fset, src, err := parseGoFile(cfg.Source)
	if err != nil {
		return Result{}, err
	}
	log.Info("parsed source", "path", cfg.Source, "decls", len(src.Decls))

	_, origSink, err := parseGoFileIfExists(cfg.Sink)
	if err != nil {
		return Result{}, err
	}
	if origSink != nil {
		log.Info("parsed sink", "path", cfg.Sink, "decls", len(origSink.Decls))
	} else {
		log.Info("sink will be created", "path", cfg.Sink)
	}

	matches, err := selectDecls(src, cfg)
	if err != nil {
		return Result{}, err
	}
	log.Info("selected declarations", "count", len(matches))

	extracted := extractMatches(fset, src, matches)
	plan := buildPlan(fset, cfg.Source, cfg.Sink, src, origSink, extracted, cfg.Move)
	plan.Selection = selectionSummary(cfg)
	if err := validatePlan(plan, origSink, src); err != nil {
		return Result{}, err
	}
	log.Info("validation passed")

	srcBytes, sinkBytes, err := renderFiles(plan)
	if err != nil {
		return Result{}, err
	}
	// On copy, only write the sink.
	if !cfg.Move {
		if err := writeSingle(cfg.Sink, sinkBytes); err != nil {
			return Result{}, fmt.Errorf("write sink %s: %w", cfg.Sink, err)
		}
		log.Info("wrote file", "path", cfg.Sink, "bytes", len(sinkBytes))
	} else {
		if err := writePair(cfg.Source, srcBytes, cfg.Sink, sinkBytes); err != nil {
			return Result{}, err
		}
		log.Info("wrote file", "path", cfg.Source, "bytes", len(srcBytes))
		log.Info("wrote file", "path", cfg.Sink, "bytes", len(sinkBytes))
	}

	matched := make([]string, 0, len(matches))
	for _, m := range matches {
		matched = append(matched, declKeys(m.Decl)...)
	}

	return Result{
		Source:                cfg.Source,
		Sink:                  cfg.Sink,
		Move:                  cfg.Move,
		Matched:               matched,
		DeclarationsRemaining: countNonImportDecls(plan.SrcFile.Decls),
	}, nil
}

// countNonImportDecls returns the number of declarations that are not import
// blocks. Import GenDecls are managed by goimports after rendering and do not
// represent meaningful user-authored declarations.
func countNonImportDecls(decls []ast.Decl) int {
	n := 0
	for _, d := range decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		n++
	}
	return n
}

// RunCLI is the entry point used by main.go and by the testscript harness.
// It parses args from scratch (not via the global flag set).
func RunCLI(args []string, _ io.Reader, stdout io.Writer, stderr io.Writer) int {
	// Handle help before flag parsing.
	if len(args) == 0 {
		printHelp(stderr)
		return 2
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printHelp(stderr)
		return 0
	}
	if args[0] == "-v" || args[0] == "-version" || args[0] == "--version" {
		_, _ = fmt.Fprintln(stdout, version.Get())
		return 0
	}
	if args[0] == "--tool-schema" {
		_, _ = stdout.Write(toolSchemaJSON())
		_, _ = io.WriteString(stdout, "\n")
		return 0
	}

	fs := flag.NewFlagSet("sflit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { printHelp(stderr) }
	var cfg Config
	var jsonOutput bool
	var debug bool
	fs.StringVar(&cfg.Source, "source", "", "source Go file")
	fs.StringVar(&cfg.Sink, "sink", "", "sink Go file")
	fs.StringVar(&cfg.Regex, "regex", "", "name regex")
	fs.StringVar(&cfg.Receiver, "receiver", "", "receiver type name")
	fs.BoolVar(&cfg.Move, "move", false, "delete matched decls from source")
	fs.BoolVar(&jsonOutput, "json", false, "print structured JSON result to stdout")
	fs.BoolVar(&debug, "debug", false, "print debug logs to stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if debug {
		cfg.Logger = slog.New(slog.NewTextHandler(stderr, nil))
	}
	res, err := Run(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sflit:", err)
		var usageErr UsageError
		if errors.As(err, &usageErr) {
			return 2
		}
		return 1
	}
	if jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	}
	return 0
}

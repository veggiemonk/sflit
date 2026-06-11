package splitter_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/veggiemonk/sflit/internal/splitter"
	"github.com/veggiemonk/testscript/scripttest"
)

func TestMain(m *testing.M) {
	scripttest.Main(m, map[string]func(){
		"sflit": func() {
			os.Exit(splitter.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
		},
		// gotypecheck <file.go>... runs the go/types compile oracle over the
		// given files as one package, so scripts can assert that split output
		// still forms a valid package — not merely that the expected bytes
		// were written. Files are explicit (not a directory glob) because the
		// script work dir keeps expected_*.go fixtures next to the outputs.
		"gotypecheck": func() {
			if err := splitter.TypeCheckFiles(os.Args[1:]...); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		},
		// sflit-lockhold / sflit-lockstress are child processes for the
		// multi-process lock tests in lock_multiprocess_test.go (ADR-0001
		// release-on-death and cross-process lock-ordering claims).
		"sflit-lockhold": func() {
			if len(os.Args) != 2 {
				fmt.Fprintln(os.Stderr, "usage: sflit-lockhold <target>")
				os.Exit(2)
			}
			os.Exit(splitter.LockHoldMain(os.Args[1], os.Stdin, os.Stdout, os.Stderr))
		},
		"sflit-lockstress": func() {
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: sflit-lockstress <iterations> <path>...")
				os.Exit(2)
			}
			n, err := strconv.Atoi(os.Args[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			os.Exit(splitter.LockStressMain(n, os.Args[2:], os.Stderr))
		},
	})
}

func TestScripts(t *testing.T) {
	scripttest.Test(t, "testdata/script/*.txt")
}

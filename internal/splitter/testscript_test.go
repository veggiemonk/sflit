package splitter_test

import (
	"fmt"
	"os"
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
	})
}

func TestScripts(t *testing.T) {
	scripttest.Test(t, "testdata/script/*.txt")
}

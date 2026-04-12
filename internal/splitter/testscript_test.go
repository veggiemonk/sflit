package splitter_test

import (
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
	})
}

func TestScripts(t *testing.T) {
	scripttest.Test(t, "testdata/script/*.txt")
}

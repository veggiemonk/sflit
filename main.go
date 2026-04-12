package main

import (
	"os"

	"github.com/veggiemonk/sflit/internal/splitter"
)

func main() {
	os.Exit(splitter.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

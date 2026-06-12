package main

import (
	"os"

	"github.com/veggiemonk/sflit/internal/mover"
)

func main() {
	os.Exit(mover.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

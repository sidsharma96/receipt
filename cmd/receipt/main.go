// Package main is the Receipt command-line entry point.
package main

import (
	"os"

	"github.com/sidsharma96/receipt/internal/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:], os.Stdout, os.Stderr))
}

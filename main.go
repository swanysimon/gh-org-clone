package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	exitSuccess     = 0
	exitRuntimeFail = 1
	exitUsage       = 2
	exitInterrupted = 130
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gh-org-clone", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gh-org-clone [flags] <org>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitUsage
	}
	return exitSuccess
}

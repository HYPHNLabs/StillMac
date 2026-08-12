package main

import (
	"context"
	"io"
	"os"

	"stillmac/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(context.Background(), args, stdout, stderr, cli.Dependencies{})
}

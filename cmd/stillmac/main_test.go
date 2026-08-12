package main

import (
	"bytes"
	"strings"
	"testing"

	"stillmac/internal/cli"
)

func TestRunProvidesCLIHelp(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run([]string{"help"}, &stdout, &stderr)
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitOK)
	}
	if !strings.Contains(stdout.String(), "stillmac <doctor|sample|status|report>") || strings.Contains(stdout.String(), "recommend") || strings.Contains(stdout.String(), "action") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunDoesNotEchoInvalidCommand(t *testing.T) {
	t.Parallel()

	const hostileCommand = "sk-test-secret-/Users/private/file.txt"
	var stdout, stderr bytes.Buffer
	code := run([]string{hostileCommand}, &stdout, &stderr)
	if code != cli.ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if strings.Contains(stdout.String()+stderr.String(), hostileCommand) {
		t.Fatal("CLI echoed an invalid command containing prohibited data")
	}
}

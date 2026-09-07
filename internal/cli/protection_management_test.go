package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"stillmac/internal/cleanup"
	"stillmac/internal/cli"
)

func TestProtectionCommandsListAndRemoveExactRecord(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	code, out, errout := runCleanup(t, []string{"scan", "--format", "json"}, deps)
	if code != 0 {
		t.Fatalf("scan: %d %s", code, errout)
	}
	var items []cleanup.Candidate
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range items {
		if c.Family == "go-build-cache" {
			id = c.ID
		}
	}
	code, _, errout = runCleanup(t, []string{"protect", id}, deps)
	if code != 0 {
		t.Fatalf("protect: %d %s", code, errout)
	}
	code, out, errout = runCleanup(t, []string{"protections", "--format", "json"}, deps)
	if code != 0 || !strings.Contains(out, id) {
		t.Fatalf("list: %d %s %s", code, out, errout)
	}
	code, out, errout = runCleanup(t, []string{"unprotect", id}, deps)
	if code != 0 || !strings.Contains(out, "fresh plan") {
		t.Fatalf("unprotect: %d %s %s", code, out, errout)
	}
	code, out, errout = runCleanup(t, []string{"protections", "--format", "json"}, deps)
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty list: %d %s %s", code, out, errout)
	}
}

func TestProtectionCommandsRejectUnknownAndExtraOptions(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	for _, args := range [][]string{{"unprotect"}, {"protections", "sm-0123456789abcdef"}, {"unprotect", "sm-0123456789abcdef", "--scope", "/fixture"}} {
		code, _, _ := runCleanup(t, args, deps)
		if code != cli.ExitUsage {
			t.Fatalf("%v returned %d", args, code)
		}
	}
	code, _, _ := runCleanup(t, []string{"unprotect", "sm-0123456789abcdef"}, deps)
	if code != cli.ExitState {
		t.Fatalf("missing protection returned %d", code)
	}
}

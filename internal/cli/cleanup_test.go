package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stillmac/internal/cleanup"
	"stillmac/internal/cli"
)

type cliFakeGoCleaner struct{}

func (cliFakeGoCleaner) Bind(_, target string) (cleanup.GoToolBinding, error) {
	return cleanup.GoToolBinding{Path: "/fixture/go", Device: 7, Inode: 11, Fingerprint: "fixture-go-fingerprint", Version: "go version go1.23 fixture/arm64", GoCache: target}, nil
}

func (cliFakeGoCleaner) Clean(_ cleanup.GoToolBinding, _, target string) error {
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func cleanupDeps(t *testing.T, tty bool, input string) (cli.Dependencies, string, string) {
	t.Helper()
	home := t.TempDir()
	data := filepath.Join(t.TempDir(), "state")
	for _, rel := range []string{"Library/Caches/Homebrew", "Library/Caches/go-build", ".cache/codex-runtimes"} {
		root := filepath.Join(home, rel)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "fixture"), []byte(rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	return cli.Dependencies{
		DefaultDataDir: func() (string, error) { return data, nil },
		CleanupHome:    func() (string, error) { return home, nil },
		CleanupHostID:  func() string { return "cli-fixture-host" },
		Now:            func() time.Time { return now },
		Stdin:          strings.NewReader(input),
		IsTTY:          func() bool { return tty },
		GitRunner:      func(args ...string) (cleanup.GitResult, error) { return cleanup.GitResult{}, errors.New("not git") },
		GoCleaner:      cliFakeGoCleaner{},
	}, home, data
}

func runCleanup(t *testing.T, args []string, deps cli.Dependencies) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := cli.Run(context.Background(), args, &out, &err, deps)
	return code, out.String(), err.String()
}

func TestCleanupCLIParsesMultipleIDsOptionsAndScope(t *testing.T) {
	deps, _, data := cleanupDeps(t, false, "")
	code, scanJSON, stderr := runCleanup(t, []string{"scan", "--scope", t.TempDir(), "--format=json"}, deps)
	if code != cli.ExitOK {
		t.Fatalf("scan code=%d stderr=%q", code, stderr)
	}
	var items []cleanup.Candidate
	if err := json.Unmarshal([]byte(scanJSON), &items); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range items {
		if c.Decision == cleanup.Safe {
			ids = append(ids, c.ID)
		}
	}
	args := []string{"plan", ids[0], "--scope", t.TempDir(), "--data-dir", data, "--format", "json"}
	code, out, stderr := runCleanup(t, args, deps)
	if code != cli.ExitOK {
		t.Fatalf("plan code=%d stderr=%q", code, stderr)
	}
	var p cleanup.Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil || len(p.Candidates) != 1 {
		t.Fatalf("plan=%#v err=%v output=%s", p, err, out)
	}
}

func TestScanCLIJSONUsesEmptyArrayWhenThereAreNoCandidates(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	emptyHome := t.TempDir()
	deps.CleanupHome = func() (string, error) { return emptyHome, nil }
	code, out, stderr := runCleanup(t, []string{"scan", "--format", "json"}, deps)
	if code != cli.ExitOK || stderr != "" {
		t.Fatalf("scan code=%d stderr=%q", code, stderr)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("scan JSON = %q, want []", out)
	}
}

func TestExplainApplyHistoryTextAndJSON(t *testing.T) {
	deps, _, data := cleanupDeps(t, false, "")
	_, scanJSON, _ := runCleanup(t, []string{"scan", "--format", "json"}, deps)
	var items []cleanup.Candidate
	_ = json.Unmarshal([]byte(scanJSON), &items)
	var id string
	for _, c := range items {
		if c.Family == "go-build-cache" {
			id = c.ID
		}
	}
	for _, format := range []string{"text", "json"} {
		code, out, errout := runCleanup(t, []string{"explain", id, "--format", format}, deps)
		if code != cli.ExitOK || out == "" {
			t.Fatalf("explain %s code=%d out=%q err=%q", format, code, out, errout)
		}
	}
	_, planJSON, _ := runCleanup(t, []string{"plan", id, "--data-dir", data, "--format", "json"}, deps)
	var p cleanup.Plan
	_ = json.Unmarshal([]byte(planJSON), &p)
	for _, format := range []string{"text", "json"} {
		if format == "text" {
			continue
		}
		code, out, errout := runCleanup(t, []string{"apply", p.ID, "--data-dir", data, "--format", format}, deps)
		if code != cli.ExitOK || !strings.Contains(out, "cleaned") {
			t.Fatalf("apply code=%d out=%q err=%q", code, out, errout)
		}
	}
	for _, format := range []string{"text", "json"} {
		code, out, errout := runCleanup(t, []string{"history", "--data-dir", data, "--format", format}, deps)
		if code != cli.ExitOK || !strings.Contains(out, "cleaned") {
			t.Fatalf("history %s code=%d out=%q err=%q", format, code, out, errout)
		}
	}
}

func TestApplyTextOutput(t *testing.T) {
	deps, _, data := cleanupDeps(t, false, "")
	_, scanJSON, _ := runCleanup(t, []string{"scan", "--format=json"}, deps)
	var items []cleanup.Candidate
	_ = json.Unmarshal([]byte(scanJSON), &items)
	var id string
	for _, c := range items {
		if c.Family == "go-build-cache" {
			id = c.ID
		}
	}
	_, planJSON, _ := runCleanup(t, []string{"plan", id, "--format=json"}, deps)
	var p cleanup.Plan
	_ = json.Unmarshal([]byte(planJSON), &p)
	code, out, errout := runCleanup(t, []string{"apply", p.ID, "--format=text"}, deps)
	if code != cli.ExitOK || !strings.Contains(out, "cleaned") || errout != "" {
		t.Fatalf("code=%d out=%q err=%q data=%s", code, out, errout, data)
	}
}

func TestProtectCLIRejectsUnknownAndAffectsScan(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	code, _, _ := runCleanup(t, []string{"protect", "sm-0000000000000000"}, deps)
	if code != cli.ExitState {
		t.Fatalf("unknown protect code=%d", code)
	}
	_, raw, _ := runCleanup(t, []string{"scan", "--format=json"}, deps)
	var items []cleanup.Candidate
	_ = json.Unmarshal([]byte(raw), &items)
	code, _, errout := runCleanup(t, []string{"protect", items[0].ID}, deps)
	if code != cli.ExitOK {
		t.Fatalf("protect code=%d err=%q", code, errout)
	}
	_, raw, _ = runCleanup(t, []string{"scan", "--format=json"}, deps)
	if !strings.Contains(raw, "PROTECTED") {
		t.Fatalf("scan=%s", raw)
	}
}

func TestInteractiveCleanShowsFullListExclusionsAndRequiresExactConfirmation(t *testing.T) {
	base, _, _ := cleanupDeps(t, true, "")
	_, raw, _ := runCleanup(t, []string{"scan", "--format=json"}, base)
	var items []cleanup.Candidate
	_ = json.Unmarshal([]byte(raw), &items)
	engine := cleanup.Engine{Config: cleanup.Config{Home: mustHome(t, base), DataDir: mustData(t, base), HostID: "cli-fixture-host", Now: base.Now, GoCleaner: base.GoCleaner}}
	p, err := engine.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	deps, home, data := cleanupDeps(t, true, "apply "+p.ID+"\n")
	// Recreate the expected plan ID from this fixture before running clean.
	eng := cleanup.Engine{Config: cleanup.Config{Home: home, DataDir: data, HostID: "cli-fixture-host", Now: deps.Now, GoCleaner: deps.GoCleaner}}
	scan, _ := eng.Scan("")
	expected, _ := eng.Plan(scan, []string{"all-safe"})
	deps.Stdin = strings.NewReader("apply " + expected.ID + "\n")
	code, out, errout := runCleanup(t, []string{"clean", "all"}, deps)
	if code != cli.ExitOK {
		t.Fatalf("code=%d out=%q err=%q", code, out, errout)
	}
	for _, want := range []string{"SAFE", "REVIEW", "BLOCKED_ACTIVE", "excluded", "apply " + expected.ID, "cleaned"} {
		if !strings.Contains(out, want) {
			t.Fatalf("clean output missing %q: %s", want, out)
		}
	}
}

func TestScanTextIsNumberedAndCleanMapsCurrentNumbers(t *testing.T) {
	deps, home, data := cleanupDeps(t, true, "")
	code, out, errout := runCleanup(t, []string{"scan"}, deps)
	if code != cli.ExitOK || errout != "" || !strings.Contains(out, "1. ") || !strings.Contains(out, "2. ") {
		t.Fatalf("numbered scan code=%d out=%q err=%q", code, out, errout)
	}
	engine := cleanup.Engine{Config: cleanup.Config{Home: home, DataDir: data, HostID: "cli-fixture-host", Now: deps.Now, GoCleaner: deps.GoCleaner}}
	items, _ := engine.Scan("")
	var ordinal int
	for i, item := range items {
		if item.Family == "go-build-cache" {
			ordinal = i + 1
		}
	}
	p, err := engine.Plan(items, []string{items[ordinal-1].ID})
	if err != nil {
		t.Fatal(err)
	}
	deps.Stdin = strings.NewReader("apply " + p.ID + "\n")
	code, _, errout = runCleanup(t, []string{"clean", fmt.Sprint(ordinal)}, deps)
	if code != cli.ExitOK {
		t.Fatalf("number selection failed code=%d err=%q", code, errout)
	}
	entries, err := os.ReadDir(filepath.Join(home, "Library/Caches/go-build"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("selected cache was not owner-cleaned: entries=%v err=%v", entries, err)
	}
}

func TestCleanRejectsMixedAllAndIDsWithoutMutation(t *testing.T) {
	deps, home, _ := cleanupDeps(t, true, "")
	code, _, _ := runCleanup(t, []string{"clean", "all", "1"}, deps)
	if code == cli.ExitOK {
		t.Fatal("mixed all and ordinal accepted")
	}
	for _, rel := range []string{"Library/Caches/Homebrew", "Library/Caches/go-build"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Fatalf("%s mutated: %v", rel, err)
		}
	}
}

func mustHome(t *testing.T, d cli.Dependencies) string {
	v, e := d.CleanupHome()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func mustData(t *testing.T, d cli.Dependencies) string {
	v, e := d.DefaultDataDir()
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCleanWrongConfirmationAndNonTTYNeverMutate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tty   bool
		input string
	}{{"wrong", true, "yes\n"}, {"non-tty", false, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			deps, home, _ := cleanupDeps(t, tc.tty, tc.input)
			root := filepath.Join(home, "Library/Caches/go-build")
			code, _, errout := runCleanup(t, []string{"clean", "all"}, deps)
			if code == cli.ExitOK {
				t.Fatal("clean unexpectedly succeeded")
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("cache mutated: %v", err)
			}
			if !tc.tty && !strings.Contains(errout, "plan") {
				t.Fatalf("stderr=%q", errout)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCleanupWriterFailuresUseReportExit(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	for _, args := range [][]string{{"scan"}, {"history"}} {
		var stderr bytes.Buffer
		code := cli.Run(context.Background(), args, failingWriter{}, &stderr, deps)
		if code != cli.ExitReport {
			t.Fatalf("%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestHelpDocumentsExactCleanupSyntax(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	code, out, errout := runCleanup(t, []string{"help"}, deps)
	if code != cli.ExitOK || errout != "" {
		t.Fatalf("code=%d err=%q", code, errout)
	}
	for _, line := range []string{"scan [--scope PATH] [--format text|json]", "plan ID... | plan all-safe", "apply PLAN_ID", "clean [IDs...|all]", "protect ID", "history [--data-dir PATH]"} {
		if !strings.Contains(out, line) {
			t.Fatalf("help missing %q: %s", line, out)
		}
	}
}

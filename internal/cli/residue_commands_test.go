package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResidueCommandsAreExplicitPrivateAndComparable(t *testing.T) {
	deps, home, data := cleanupDeps(t, false, "")
	project := filepath.Join(t.TempDir(), "synthetic-project")
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(project, "dist", "synthetic-bundle")
	if err := os.WriteFile(artifact, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, errout := runCleanup(t, []string{"inspect", "--scope", project, "--format", "json"}, deps)
	if code != 0 {
		t.Fatalf("inspect: %d %s", code, errout)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report["schema_version"] != "stillmac.residue.report.v1" {
		t.Fatalf("schema: %s", out)
	}
	for _, private := range []string{home, data, project, "synthetic-bundle"} {
		if strings.Contains(out, private) {
			t.Fatalf("private output: %s", out)
		}
	}
	if _, err := os.Stat(filepath.Join(data, "residue")); !os.IsNotExist(err) {
		t.Fatal("inspect wrote history")
	}
	code, out, errout = runCleanup(t, []string{"snapshot", "--scope", project, "--format", "json"}, deps)
	if code != 0 {
		t.Fatalf("snapshot: %d %s %s", code, out, errout)
	}
	clock := deps.Now().Add(time.Minute)
	deps.Now = func() time.Time { return clock }
	if err := os.WriteFile(artifact, []byte("abc with growth"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, errout = runCleanup(t, []string{"snapshot", "--scope", project, "--format", "json"}, deps)
	if code != 0 {
		t.Fatalf("snapshot2: %d %s", code, errout)
	}
	code, out, errout = runCleanup(t, []string{"changes", "--format", "json"}, deps)
	if code != 0 || !strings.Contains(out, "growth") {
		t.Fatalf("changes: %d %s %s", code, out, errout)
	}
}

func TestResidueCommandsRefuseAmbiguousOptions(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	for _, args := range [][]string{
		{"inspect", "unexpected"},
		{"inspect", "--scope"},
		{"snapshot", "--format", "xml"},
		{"changes", "--scope", "/fixture"},
		{"session-report", "--threshold-bytes", "-1"},
		{"session-report", "--threshold-bytes", "overflow"},
	} {
		code, _, _ := runCleanup(t, args, deps)
		if code != 2 {
			t.Fatalf("%v returned %d", args, code)
		}
	}
}

func TestSnapshotTextIncludesHistoryIDForLaterComparison(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	code, out, errOut := runCleanup(t, []string{"snapshot"}, deps)
	if code != 0 {
		t.Fatalf("snapshot: %d %s", code, errOut)
	}
	if !strings.Contains(out, "Snapshot: snapshot-") {
		t.Fatalf("missing usable history ID: %s", out)
	}
}

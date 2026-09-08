package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetireCommandsRequireExplicitHandoverAndExactPlan(t *testing.T) {
	deps, home, data := cleanupDeps(t, false, "")
	physicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	physicalDataParent, err := filepath.EvalSymlinks(filepath.Dir(data))
	if err != nil {
		t.Fatal(err)
	}
	deps.CleanupHome = func() (string, error) { return physicalHome, nil }
	deps.DefaultDataDir = func() (string, error) { return filepath.Join(physicalDataParent, "state"), nil }
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main")
	linked := filepath.Join(root, "linked")
	if err := os.Mkdir(main, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", main}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git failed: %v %s", err, output)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "StillMac Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(main, "tracked"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked")
	git("commit", "-qm", "fixture")
	git("worktree", "add", "-q", "-b", "fixture-linked", linked)
	code, _, _ := runCleanup(t, []string{"retire", "plan", "--target", linked}, deps)
	if code != 2 {
		t.Fatalf("unattested plan returned %d", code)
	}
	code, out, errout := runCleanup(t, []string{"retire", "plan", "--target", linked, "--user-managed", "--session-ended", "--format", "json"}, deps)
	if code != 0 {
		t.Fatalf("plan: %d %s %s", code, out, errout)
	}
	var plan struct {
		ID string `json:"plan_id"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.ID == "" {
		t.Fatalf("plan JSON: %v %s", err, out)
	}
	if strings.Contains(out, linked) || strings.Contains(out, main) {
		t.Fatal("plan leaked private path")
	}
	if _, err := os.Stat(linked); err != nil {
		t.Fatal("plan changed target")
	}
	code, out, errout = runCleanup(t, []string{"retire", "apply", plan.ID, "--format", "json"}, deps)
	if code != 0 {
		t.Fatalf("apply: %d %s %s", code, out, errout)
	}
	if _, err := os.Stat(linked); !os.IsNotExist(err) {
		t.Fatal("native retirement did not remove fixture")
	}
	code, out, errout = runCleanup(t, []string{"retire", "history", "--format", "json"}, deps)
	if code != 0 || !strings.Contains(out, plan.ID) {
		t.Fatalf("history: %d %s %s", code, out, errout)
	}
}

func TestRetireCLIRejectsAmbiguousOrDestructiveOptions(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	for _, args := range [][]string{
		{"retire"}, {"retire", "apply"}, {"retire", "apply", "plan-fixture", "--force"},
		{"retire", "plan", "--target", "/fixture", "--user-managed"},
		{"retire", "recover", "receipt-fixture", "--target", "/other"},
	} {
		code, _, _ := runCleanup(t, args, deps)
		if code != 2 {
			t.Fatalf("%v returned %d", args, code)
		}
	}
}

func TestRetireCLIExplainsBlockedRegistrationWithoutOfferingApply(t *testing.T) {
	deps, _, _ := cleanupDeps(t, false, "")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps.CleanupHome = func() (string, error) { return root, nil }
	deps.DefaultDataDir = func() (string, error) { return filepath.Join(root, "state"), nil }
	protected := filepath.Join(root, ".codex", "worktrees", "synthetic")
	if err := os.MkdirAll(protected, 0700); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCleanup(t, []string{"retire", "plan", "--target", protected, "--user-managed", "--session-ended", "--format", "json"}, deps)
	if code != 5 {
		t.Fatalf("blocked target exit=%d %s %s", code, out, errOut)
	}
	var result struct {
		Decision string   `json:"decision"`
		Reasons  []string `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("missing blocked evidence: %v %s", err, out)
	}
	if result.Decision != "BLOCKED" || len(result.Reasons) == 0 {
		t.Fatalf("missing blocked evidence: %s", out)
	}
	if strings.Contains(out, protected) || strings.Contains(out, "retire apply") {
		t.Fatalf("unsafe blocked explanation: %s", out)
	}
}

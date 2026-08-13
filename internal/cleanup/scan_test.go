package cleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)

func TestScanReturnsNonNilEmptySliceWhenThereAreNoCandidates(t *testing.T) {
	items, err := ScanWithConfig(ScanConfig{Home: t.TempDir(), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want non-nil empty slice", items)
	}
}

func TestAddTreeBytesFailsClosedOnOverflow(t *testing.T) {
	if _, err := addTreeBytes(1, -1); err == nil {
		t.Fatal("negative size accepted")
	}
	if _, err := addTreeBytes(^int64(0), 1); err == nil {
		t.Fatal("overflow accepted")
	}
}

func cacheFixture(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	for _, rel := range []string{"Library/Caches/Homebrew", "Library/Caches/go-build", ".cache/codex-runtimes"} {
		root := filepath.Join(home, rel)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "synthetic.cache"), []byte(rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home, filepath.Join(t.TempDir(), "state")
}

func TestScanExactRootsCodexAndPrivacy(t *testing.T) {
	home, data := cacheFixture(t)
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %#v", items)
	}
	decisions := map[string]Decision{}
	for _, item := range items {
		decisions[item.Family] = item.Decision
	}
	if decisions["homebrew-cache"] != Review || decisions["go-build-cache"] != Safe || decisions["codex-runtime-cache"] != BlockedActive {
		t.Fatalf("decisions = %#v", decisions)
	}
	b, _ := json.Marshal(items)
	for _, prohibited := range []string{home, filepath.Base(home), "/Users/", ".codex", ".claude", ".hermes", "synthetic.cache"} {
		if strings.Contains(string(b), prohibited) {
			t.Fatalf("JSON leaked %q: %s", prohibited, b)
		}
	}
}

func TestCodexInactiveProofStillRequiresReview(t *testing.T) {
	home, data := cacheFixture(t)
	inactive := true
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, CodexInactive: &inactive, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "codex-runtime-cache" && (item.Decision != Review || item.Action != "none") {
			t.Fatalf("codex = %#v", item)
		}
	}
}

func TestScopeDoesNotInventProjectCache(t *testing.T) {
	home, data := cacheFixture(t)
	scope := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scope, "go-build"), 0o700); err != nil {
		t.Fatal(err)
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: func(...string) (GitResult, error) { return GitResult{}, os.ErrNotExist }, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.RootKind == "project-go-cache" {
			t.Fatal("invented project cache candidate")
		}
	}
}

func TestUnsafeCacheRootsFailClosed(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Library/Caches"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(home, "Library/Caches/Homebrew")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library/Caches/go-build"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, Now: fixedNow, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	for _, item := range items {
		if item.Decision != BlockedUnknown || item.Action != "none" {
			t.Fatalf("unsafe = %#v", item)
		}
	}
}

func TestUnsafeHomebrewEntryRemainsNonExecutableReview(t *testing.T) {
	home, data := cacheFixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "Library/Caches/Homebrew", "linked")); err != nil {
		t.Fatal(err)
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatalf("whole scan failed: %v", err)
	}
	decisions := map[string]Decision{}
	for _, item := range items {
		decisions[item.Family] = item.Decision
	}
	if decisions["homebrew-cache"] != Review || decisions["go-build-cache"] != Safe {
		t.Fatalf("decisions=%#v", decisions)
	}
}

func TestGitWorktreePorcelainClassifications(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "current")
	paths := []string{scope, filepath.Join(t.TempDir(), "dirty"), filepath.Join(t.TempDir(), "locked"), filepath.Join(t.TempDir(), "unmerged"), filepath.Join(t.TempDir(), "merged")}
	porcelain := ""
	branches := []string{"feature/current", "feature/dirty", "feature/locked", "feature/unmerged", "feature/merged"}
	for i, p := range paths {
		porcelain += "worktree " + p + "\nHEAD abc\nbranch refs/heads/" + branches[i] + "\n"
		if i == 2 {
			porcelain += "locked reason\n"
		}
		porcelain += "\n"
	}
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		if strings.Contains(joined, "worktree\x00list\x00--porcelain") {
			return GitResult{Output: []byte(porcelain)}, nil
		}
		for i, p := range paths {
			if strings.Contains(joined, p+"\x00status") && i == 1 {
				return GitResult{Output: []byte(" M file\n")}, nil
			}
			if strings.Contains(joined, p+"\x00status") {
				return GitResult{}, nil
			}
			if strings.Contains(joined, p+"\x00merge-base") && i == 3 {
				return GitResult{ExitCode: 1}, nil
			}
			if strings.Contains(joined, p+"\x00merge-base") {
				return GitResult{}, nil
			}
		}
		return GitResult{}, nil
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Decision{}
	for _, item := range items {
		if item.Family == "git-worktree" {
			got[item.CurrentState] = item.Decision
			if item.Action != "none" {
				t.Fatal("git action is executable")
			}
		}
	}
	for state, want := range map[string]Decision{"current": BlockedActive, "dirty": BlockedDirty, "locked": BlockedActive, "unmerged": BlockedUnmerged, "clean-merged": Review} {
		if got[state] != want {
			t.Fatalf("%s = %q, want %q; all=%#v", state, got[state], want, got)
		}
	}
}

func TestGitMergeOperationalFailureIsBlockedUnknown(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "current")
	linked := filepath.Join(t.TempDir(), "linked")
	porcelain := "worktree " + scope + "\nHEAD a\nbranch refs/heads/main\n\nworktree " + linked + "\nHEAD b\nbranch refs/heads/feature\n"
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "status\x00--porcelain"):
			return GitResult{}, nil
		case strings.Contains(joined, "merge-base"):
			return GitResult{ExitCode: 128}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && item.CurrentState != "current" {
			if item.Decision != BlockedUnknown || item.CurrentState != "unknown" {
				t.Fatalf("operational merge failure = %#v", item)
			}
			return
		}
	}
	t.Fatal("linked worktree candidate missing")
}

func TestMixedAgentRootsAreNeverTraversedOrEmitted(t *testing.T) {
	home, data := cacheFixture(t)
	for _, rel := range []string{".codex", ".claude", ".hermes"} {
		root := filepath.Join(home, rel)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "DO-NOT-READ-secret.fixture"), []byte("private"), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatalf("mixed roots affected scan: %v", err)
	}
	b, _ := json.Marshal(items)
	for _, prohibited := range []string{".codex", ".claude", ".hermes", "DO-NOT-READ", "secret.fixture"} {
		if strings.Contains(string(b), prohibited) {
			t.Fatalf("mixed root content emitted: %s", b)
		}
	}
}

type exitOne struct{}

func (exitOne) Error() string { return "exit 1" }

var errExitOne error = exitOne{}

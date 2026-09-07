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
	for _, item := range items {
		if item.Family == "homebrew-cache" {
			if item.SizeStatus() != SizePartial {
				t.Fatalf("partial Homebrew size = %#v", item)
			}
		}
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
		if strings.Contains(joined, "for-each-ref") {
			return GitResult{Output: []byte("refs/heads/main\n")}, nil
		}
		if strings.Contains(joined, "rev-parse") {
			return GitResult{Output: []byte(strings.Repeat("a", 40) + "\n")}, nil
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
	for state, want := range map[string]Decision{"current": BlockedActive, "dirty": BlockedDirty, "locked": BlockedActive, "merge-unproven": BlockedUnmerged, "clean-merged": Review} {
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
		case strings.Contains(joined, "for-each-ref"):
			return GitResult{Output: []byte("refs/heads/main\n")}, nil
		case strings.Contains(joined, "rev-parse"):
			return GitResult{Output: []byte(strings.Repeat("b", 40) + "\n")}, nil
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
			if item.Decision != BlockedUnknown || item.CurrentState != "merge-check-unknown" {
				t.Fatalf("operational merge failure = %#v", item)
			}
			return
		}
	}
	t.Fatal("linked worktree candidate missing")
}

func TestLinkedWorktreeOnMainBranchIsNotPrimaryByBranchName(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "primary")
	linked := filepath.Join(t.TempDir(), "linked")
	porcelain := "worktree " + scope + "\nHEAD primary\nbranch refs/heads/feature\n\n" +
		"worktree " + linked + "\nHEAD linked\nbranch refs/heads/main\n"
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "for-each-ref"):
			return GitResult{Output: []byte("refs/heads/main\n")}, nil
		case strings.Contains(joined, "rev-parse"):
			return GitResult{Output: []byte(strings.Repeat("c", 40) + "\n")}, nil
		case strings.Contains(joined, "status\x00--porcelain"):
			return GitResult{}, nil
		case strings.Contains(joined, "merge-base"):
			return GitResult{}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && strings.Contains(item.Label, "2") {
			if item.Decision != Review {
				t.Fatalf("linked main-branch worktree = %#v", item)
			}
			for _, reason := range item.Reasons {
				if strings.Contains(reason, "inactive") {
					t.Fatalf("unsupported inactivity claim = %#v", item)
				}
			}
			return
		}
	}
	t.Fatal("linked worktree candidate missing")
}

func TestWorktreeMergeNeedsResolvedLocalBase(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "primary")
	linked := filepath.Join(t.TempDir(), "linked")
	porcelain := "worktree " + scope + "\nHEAD primary\nbranch refs/heads/feature\n\n" +
		"worktree " + linked + "\nHEAD linked\nbranch refs/heads/feature-two\n"
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "status\x00--porcelain"):
			return GitResult{}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && strings.Contains(item.Label, "2") {
			if item.Decision != BlockedUnknown {
				t.Fatalf("missing integration base = %#v", item)
			}
			return
		}
	}
	t.Fatal("linked worktree candidate missing")
}

func TestDotScopeIncludesRepositoryWorktreeFacts(t *testing.T) {
	home, data := cacheFixture(t)
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: ".", Now: fixedNow, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && (item.CurrentState == "current" || item.CurrentState == "primary") {
			return
		}
	}
	t.Fatalf("dot scope did not expose the current repository worktree: %#v", items)
}

func TestScopeSubdirectoryUsesRepositoryRootForCurrentWorktree(t *testing.T) {
	home, data := cacheFixture(t)
	repoRoot := t.TempDir()
	subdir := filepath.Join(repoRoot, "src")
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	porcelain := "worktree " + repoRoot + "\nHEAD primary\nbranch refs/heads/feature\n\n" +
		"worktree " + linked + "\nHEAD linked\nbranch refs/heads/feature-two\n"
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "--show-toplevel"):
			return GitResult{Output: []byte(repoRoot + "\n")}, nil
		case strings.Contains(joined, "for-each-ref"):
			return GitResult{Output: []byte("refs/heads/main\n")}, nil
		case strings.Contains(joined, "rev-parse"):
			return GitResult{Output: []byte(strings.Repeat("f", 40) + "\n")}, nil
		case strings.Contains(joined, "status\x00--porcelain"), strings.Contains(joined, "merge-base"):
			return GitResult{}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: subdir, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && strings.Contains(item.Label, "1") {
			if item.Decision != BlockedActive || item.CurrentState != "current" {
				t.Fatalf("subdirectory current detection = %#v", item)
			}
			return
		}
	}
	t.Fatal("primary worktree candidate missing")
}

func TestLegacyUnknownSizeRoundTripIsConservative(t *testing.T) {
	for _, candidate := range []Candidate{
		{Family: "codex-runtime-cache", RootKind: "codex-runtime-cache", CurrentState: "activity-unproven"},
		{Family: "git-worktree", RootKind: "git-worktree", CurrentState: "current"},
		{Family: "homebrew-cache", RootKind: "homebrew-cache", CurrentState: "inventory-partial"},
	} {
		if candidate.SizeStatus() != SizeUnknown {
			t.Fatalf("legacy unknown candidate status = %q for %#v", candidate.SizeStatus(), candidate)
		}
	}
}

func TestSuppliedIntegrationBaseResolvesLocallyWithoutFetch(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "primary")
	linked := filepath.Join(t.TempDir(), "linked")
	porcelain := "worktree " + scope + "\nHEAD primary\nbranch refs/heads/feature\n\n" +
		"worktree " + linked + "\nHEAD linked\nbranch refs/heads/feature-two\n"
	var calls []string
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "rev-parse"):
			return GitResult{Output: []byte(strings.Repeat("d", 40) + "\n")}, nil
		case strings.Contains(joined, "status\x00--porcelain"), strings.Contains(joined, "merge-base"):
			return GitResult{}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, IntegrationBase: "refs/heads/release", Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	var linkedItem *Candidate
	for i := range items {
		if items[i].Family == "git-worktree" && strings.Contains(items[i].Label, "2") {
			linkedItem = &items[i]
		}
	}
	if linkedItem == nil || linkedItem.Decision != Review {
		t.Fatalf("supplied base classification = %#v", linkedItem)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "for-each-ref") || strings.Contains(joined, "fetch") {
		t.Fatalf("supplied base caused default/network lookup: %s", joined)
	}
	if !strings.Contains(joined, strings.Repeat("d", 40)) {
		t.Fatalf("resolved commit was not used for ancestry: %s", joined)
	}
}

func TestAmbiguousIntegrationBasePreservesUnknown(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "primary")
	linked := filepath.Join(t.TempDir(), "linked")
	porcelain := "worktree " + scope + "\nHEAD primary\nbranch refs/heads/feature\n\n" +
		"worktree " + linked + "\nHEAD linked\nbranch refs/heads/feature-two\n"
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "for-each-ref"):
			return GitResult{Output: []byte("refs/heads/main\nrefs/heads/master\n")}, nil
		case strings.Contains(joined, "status\x00--porcelain"):
			return GitResult{}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && strings.Contains(item.Label, "2") {
			if item.Decision != BlockedUnknown || item.CurrentState != "integration-base-unknown" || !strings.Contains(strings.Join(item.Reasons, " "), "ambiguous") {
				t.Fatalf("ambiguous base = %#v", item)
			}
			return
		}
	}
	t.Fatal("linked worktree candidate missing")
}

func TestSquashHistoryRemainsMergeUnproven(t *testing.T) {
	home, data := cacheFixture(t)
	scope := filepath.Join(t.TempDir(), "primary")
	linked := filepath.Join(t.TempDir(), "linked")
	porcelain := "worktree " + scope + "\nHEAD primary\nbranch refs/heads/feature\n\n" +
		"worktree " + linked + "\nHEAD linked\nbranch refs/heads/feature-two\n"
	runner := func(args ...string) (GitResult, error) {
		joined := strings.Join(args, "\x00")
		switch {
		case strings.Contains(joined, "worktree\x00list\x00--porcelain"):
			return GitResult{Output: []byte(porcelain)}, nil
		case strings.Contains(joined, "for-each-ref"):
			return GitResult{Output: []byte("refs/heads/main\n")}, nil
		case strings.Contains(joined, "rev-parse"):
			return GitResult{Output: []byte(strings.Repeat("e", 40) + "\n")}, nil
		case strings.Contains(joined, "status\x00--porcelain"):
			return GitResult{}, nil
		case strings.Contains(joined, "merge-base"):
			return GitResult{ExitCode: 1}, nil
		default:
			return GitResult{}, nil
		}
	}
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Scope: scope, Now: fixedNow, GitRunner: runner, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Family == "git-worktree" && strings.Contains(item.Label, "2") {
			if item.Decision != BlockedUnmerged || item.CurrentState != "merge-unproven" || !strings.Contains(strings.Join(item.Reasons, " "), "squash") {
				t.Fatalf("squash history = %#v", item)
			}
			return
		}
	}
	t.Fatal("linked worktree candidate missing")
}

func TestCandidateJSONShapeRemainsV1WithoutSizeStatus(t *testing.T) {
	home, data := cacheFixture(t)
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, GoCleaner: verifiedFakeGoCleaner(home)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "family", "rule_version", "bytes", "decision", "reasons", "action", "reversible", "captured_at", "label", "fingerprint", "current_state", "root_kind"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("v1 key missing %q: %s", key, b)
		}
	}
	if _, ok := raw["size_status"]; ok {
		t.Fatalf("internal size status leaked into v1 JSON: %s", b)
	}
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

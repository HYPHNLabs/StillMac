package residue

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stillmac/internal/cleanup"
)

func TestInspectIsReadOnlyBoundedAndRedacted(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	dataDir := filepath.Join(root, "state")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "node_modules", "pkg", "secret.txt"), []byte("PRIVATE_CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "dist", "bundle.js"), []byte("COMMAND_ARGUMENT_SHOULD_NOT_APPEAR"), 0o600); err != nil {
		t.Fatal(err)
	}
	scopeID, err := ScopeID(Scope{Path: project})
	if err != nil {
		t.Fatal(err)
	}

	engine, err := New(Config{
		Home:         home,
		DataDir:      dataDir,
		Now:          fixedClock("2026-09-07T12:00:00Z"),
		Limits:       Limits{MaxItems: 2, MaxDepth: 8, MaxBytes: 1024},
		Aliases:      []Alias{{ScopeID: scopeID, Label: "Private project label"}},
		HistoryLimit: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if label, ok := engine.AliasForScope(Scope{Path: project}); !ok || label != "Private project label" {
		t.Fatalf("bounded alias was not retained for human-only use: %q %t", label, ok)
	}
	report, err := engine.Inspect([]Scope{{Path: project, Alias: "Private project label"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{root, home, project, "Private project label", "PRIVATE_CONTENT", "COMMAND_ARGUMENT_SHOULD_NOT_APPEAR"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("machine report leaked %q: %s", forbidden, text)
		}
	}
	if report.SchemaVersion != SchemaVersion || report.ReportKind != "inspect" {
		t.Fatalf("unexpected report envelope: %#v", report)
	}
	if report.Status != StatusPartial {
		t.Fatalf("bounded scan should be partial, got %q", report.Status)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "residue")); !os.IsNotExist(err) {
		t.Fatalf("inspect must not write residue history, stat error=%v", err)
	}
}

func TestInspectProtectsAgentWorktreesAndRejectsSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "not-counted.js"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "dist", "linked")); err != nil {
		t.Fatal(err)
	}
	agentWorktree := filepath.Join(root, ".codex", "worktrees", "one")
	if err := os.MkdirAll(filepath.Join(agentWorktree, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentWorktree, "node_modules", "private.json"), []byte("agent state"), 0o600); err != nil {
		t.Fatal(err)
	}

	gitOutput := "worktree " + project + "\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/main\n\n" +
		"worktree " + agentWorktree + "\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/codex/private\n\n"
	engine, err := New(Config{
		Home:    filepath.Join(root, "home"),
		DataDir: filepath.Join(root, "state"),
		Now:     fixedClock("2026-09-07T12:00:00Z"),
		GitRunner: func(args ...string) (GitResult, error) {
			return GitResult{Output: []byte(gitOutput), ExitCode: 0}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Inspect([]Scope{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range report.Entries {
		if entry.ScopeID == "" || strings.Contains(entry.ID, "private") {
			t.Fatalf("unexpected private identity in entry: %#v", entry)
		}
		if entry.Size.Status == SizeComplete && entry.Size.Bytes != nil && *entry.Size.Bytes > 0 && entry.Family == "node_modules" && entry.Kind == "git-worktree" {
			t.Fatalf("agent worktree must not be measured: %#v", entry)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "agent state") || strings.Contains(string(encoded), agentWorktree) {
		t.Fatalf("protected worktree leaked into report: %s", encoded)
	}
	var sawPartial bool
	for _, entry := range report.Entries {
		if entry.Family == "dist" && entry.Size.Status == SizePartial {
			sawPartial = true
		}
		if entry.Kind == "git-worktree" && entry.State == "protected" && entry.ActionAvailability != ActionOwnerManaged {
			t.Fatalf("worktree action boundary changed: %#v", entry)
		}
	}
	if !sawPartial {
		t.Fatal("symlink inside artifact tree must produce a partial measurement")
	}
}

func TestSnapshotsCompareOnlyCompatibleCompleteMeasurements(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	artifact := filepath.Join(project, "dist")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(artifact, "bundle.js")
	if err := os.WriteFile(file, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	dataDir := filepath.Join(root, "state")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Home: home, DataDir: dataDir, Now: func() time.Time { return now }, HistoryLimit: 8, GitRunner: func(args ...string) (GitResult, error) { return GitResult{ExitCode: 0}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	first, err := engine.Snapshot([]Scope{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := os.WriteFile(file, []byte("one with growth"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := engine.Snapshot([]Scope{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotID == second.SnapshotID {
		t.Fatal("distinct observations must have distinct snapshot IDs")
	}
	changes, err := engine.Changes(ChangeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if changes.ReportKind != "changes" || changes.Status != StatusComplete {
		t.Fatalf("unexpected complete changes report: %#v", changes)
	}
	var sawGrowth bool
	for _, change := range changes.Changes {
		if change.Kind == ChangeGrowth {
			sawGrowth = true
			if change.Regrowth {
				t.Fatal("growth without cleanup evidence must not be called regrowth")
			}
		}
	}
	if !sawGrowth {
		t.Fatal("expected a comparable growth delta")
	}

	partialEngine, err := New(Config{
		Home:      home,
		DataDir:   dataDir,
		Now:       func() time.Time { return now.Add(time.Minute) },
		Limits:    Limits{MaxItems: 1, MaxDepth: 8, MaxBytes: 1},
		GitRunner: func(args ...string) (GitResult, error) { return GitResult{ExitCode: 0}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := partialEngine.Snapshot([]Scope{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Status != StatusPartial {
		t.Fatalf("expected partial snapshot, got %q", partial.Status)
	}
	latest, err := partialEngine.Changes(ChangeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status == StatusComplete {
		t.Fatalf("partial latest observation must not produce complete changes: %#v", latest)
	}
	for _, change := range latest.Changes {
		if change.Kind == ChangeGrowth || change.Kind == ChangeShrink {
			t.Fatalf("incomplete observations must not produce growth/shrink deltas: %#v", latest.Changes)
		}
	}
}

func TestSessionReportQuietNeverSuppressesFirstOrUnknown(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "state")
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Home: home, DataDir: dataDir, Now: func() time.Time { return now }, HistoryLimit: 8, GitRunner: func(args ...string) (GitResult, error) { return GitResult{ExitCode: 0}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	first, err := engine.SessionReport(SessionReportOptions{Scopes: []Scope{{Path: project}}, QuietUnchanged: true, ThresholdBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if first.Suppressed || first.Quiet || first.Status == StatusUnknown {
		t.Fatalf("first session report must be visible and clear: %#v", first)
	}
	now = now.Add(time.Minute)
	second, err := engine.SessionReport(SessionReportOptions{Scopes: []Scope{{Path: project}}, QuietUnchanged: true, ThresholdBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Quiet || !second.Suppressed {
		t.Fatalf("unchanged complete session should be quiet: %#v", second)
	}

	badScope := filepath.Join(root, "missing")
	now = now.Add(time.Minute)
	unknown, err := engine.SessionReport(SessionReportOptions{Scopes: []Scope{{Path: badScope}}, QuietUnchanged: true, ThresholdBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Suppressed || unknown.Quiet || unknown.Status != StatusUnknown {
		t.Fatalf("unknown evidence must never be suppressed: %#v", unknown)
	}
}

func TestOverlapAndHardlinksAreCountedOnceInTotal(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(project, "dist", "one.js")
	second := filepath.Join(project, "dist", "two.js")
	contents := []byte("same inode")
	if err := os.WriteFile(first, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{
		Home:      home,
		Limits:    Limits{MaxItems: 100, MaxDepth: 8, MaxBytes: 1024},
		Now:       fixedClock("2026-09-07T12:00:00Z"),
		GitRunner: func(args ...string) (GitResult, error) { return GitResult{ExitCode: 0}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Inspect([]Scope{{Path: project}, {Path: project, Kind: "repository"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusComplete {
		t.Fatalf("overlap scan should be complete: %#v", report)
	}
	if report.Evidence.HardlinksSkipped < 2 {
		t.Fatalf("expected hardlink deduplication across rows, evidence=%#v", report.Evidence)
	}
	if report.Evidence.TotalSize.Status != SizeComplete || report.Evidence.TotalSize.Bytes == nil || *report.Evidence.TotalSize.Bytes != int64(len(contents)) {
		t.Fatalf("expected one unique file's bytes, got %#v", report.Evidence.TotalSize)
	}
}

func TestHistoryRejectsUnsafePermissionsAndUnknownSizeStates(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	dataDir := filepath.Join(root, "state")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Home: home, DataDir: dataDir, Now: fixedClock("2026-09-07T12:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Snapshot([]Scope{{Path: project}}); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(dataDir, "residue", "snapshots")
	if err := os.Chmod(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Changes(ChangeOptions{}); !errors.Is(err, ErrHistory) {
		t.Fatalf("unsafe history permissions must fail closed, got %v", err)
	}
}

func TestHistoryRejectsTrailingJSONAndPrunesAtConfiguredLimit(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	dataDir := filepath.Join(root, "state")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	engine, err := New(Config{Home: home, DataDir: dataDir, HistoryLimit: 1, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Snapshot(nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := engine.Snapshot(nil); err != nil {
		t.Fatal(err)
	}
	if report, err := engine.Changes(ChangeOptions{}); err != nil || len(report.Changes) != 0 || report.Status != StatusNotPresent {
		t.Fatalf("bounded history should remain readable after pruning: report=%#v err=%v", report, err)
	}

	entries, err := os.ReadDir(filepath.Join(dataDir, "residue", "snapshots"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one retained snapshot, entries=%v err=%v", entries, err)
	}
	path := filepath.Join(dataDir, "residue", "snapshots", entries[0].Name())
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Changes(ChangeOptions{}); !errors.Is(err, ErrHistory) {
		t.Fatalf("trailing JSON must fail closed, got %v", err)
	}
}

func TestHistoryRejectsSymlinkedStateAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Join(root, "real"), alias); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{DataDir: filepath.Join(alias, "state"), Now: fixedClock("2026-09-07T12:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Snapshot(nil); !errors.Is(err, ErrDataDir) {
		t.Fatalf("symlinked state ancestor must fail closed, got %v", err)
	}
}

func TestProtectedSymlinkAliasAndBoundsAreExplicit(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	codexRoot := filepath.Join(root, ".codex", "worktrees", "one")
	alias := filepath.Join(root, "alias")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codexRoot, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexRoot, "dist", "private.js"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(codexRoot, alias); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Home: home, Limits: Limits{MaxItems: 1, MaxDepth: 1, MaxBytes: 1, MaxDuration: time.Second}, Now: fixedClock("2026-09-07T12:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Inspect([]Scope{{Path: alias}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUnknown {
		t.Fatalf("protected alias must be unknown, got %#v", report)
	}
	for _, entry := range report.Entries {
		if entry.Family == "dist" || entry.Size.Status == SizeComplete {
			t.Fatalf("protected alias was traversed: %#v", entry)
		}
	}

	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "dist", "large.js"), []byte("larger than one"), 0o600); err != nil {
		t.Fatal(err)
	}
	bounded, err := engine.Inspect([]Scope{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	if bounded.Status != StatusPartial {
		t.Fatalf("bounded artifact scan must be partial, got %#v", bounded)
	}
	for _, entry := range bounded.Entries {
		if entry.Family == "dist" && entry.Size.Status == SizeComplete {
			t.Fatalf("bounded artifact was incorrectly complete: %#v", entry)
		}
	}
}

func TestUnknownDefaultCacheDoesNotHideComparableGrowth(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(home, ".cache", "codex-runtimes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(project, "dist", "bundle.js")
	if err := os.WriteFile(file, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	engine, err := New(Config{Home: home, DataDir: filepath.Join(root, "state"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Snapshot([]Scope{{Path: project}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := os.WriteFile(file, []byte("a with growth"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Snapshot([]Scope{{Path: project}}); err != nil {
		t.Fatal(err)
	}
	changes, err := engine.Changes(ChangeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if changes.Status != StatusPartial {
		t.Fatalf("unknown unrelated cache should make comparison partial, got %#v", changes)
	}
	var growth bool
	for _, change := range changes.Changes {
		if change.Family == "dist" && change.Kind == ChangeGrowth {
			growth = true
		}
	}
	if !growth {
		t.Fatalf("comparable project growth was hidden by unknown cache: %#v", changes.Changes)
	}
}

func TestRegrowthRequiresLinkedGoCleanupReceipt(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cache := filepath.Join(home, "Library", "Caches", "go-build")
	dataDir := filepath.Join(root, "state")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "one"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleaner := &testGoCleaner{}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	residueEngine, err := New(Config{Home: home, DataDir: dataDir, GoCleaner: cleaner, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := residueEngine.Snapshot(nil); err != nil {
		t.Fatal(err)
	}

	cleanupNow := now.Add(time.Minute)
	cleanupEngine := &cleanup.Engine{Config: cleanup.Config{Home: home, DataDir: dataDir, GoCleaner: cleaner, Now: func() time.Time { return cleanupNow }}}
	candidates, err := cleanupEngine.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cleanupEngine.Plan(candidates, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cleanupEngine.Apply(plan.ID); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := residueEngine.Snapshot(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "one"), []byte("01234567890123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := residueEngine.Snapshot(nil); err != nil {
		t.Fatal(err)
	}
	changes, err := residueEngine.Changes(ChangeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var sawRegrowth bool
	for _, change := range changes.Changes {
		if change.Family == "go-build-cache" && change.Kind == ChangeGrowth {
			sawRegrowth = change.Regrowth
		}
	}
	if !sawRegrowth {
		t.Fatalf("linked cleanup receipt and reduction should establish regrowth: %#v", changes.Changes)
	}
}

type testGoCleaner struct{}

func (c *testGoCleaner) Bind(home, target string) (cleanup.GoToolBinding, error) {
	return cleanup.GoToolBinding{Path: "/synthetic/go", Device: 1, Inode: 2, Fingerprint: "synthetic", Version: "go version go1.23 synthetic", GoCache: filepath.Join(home, "Library", "Caches", "go-build")}, nil
}

func (c *testGoCleaner) Clean(binding cleanup.GoToolBinding, home, target string) error {
	return os.Remove(filepath.Join(target, "one"))
}

func fixedClock(value string) func() time.Time {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return at }
}

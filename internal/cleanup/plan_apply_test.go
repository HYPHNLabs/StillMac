package cleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEngine(t *testing.T) (*Engine, []Candidate, string, string) {
	t.Helper()
	home, data := cacheFixture(t)
	e := &Engine{Config: Config{Home: home, DataDir: data, HostID: "fixture-host", Now: func() time.Time { return fixedNow }, GoCleaner: verifiedFakeGoCleaner(home)}}
	items, err := e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	return e, items, home, data
}

func TestPlanSelectionsAllSafeAndUnknown(t *testing.T) {
	e, items, _, _ := testEngine(t)
	p, err := e.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Candidates) != 1 || len(p.Excluded) != 2 {
		t.Fatalf("plan = %#v", p)
	}
	for _, c := range p.Candidates {
		if c.Decision != Safe {
			t.Fatalf("included %#v", c)
		}
	}
	p2, err := e.Plan(items, []string{p.Candidates[0].ID})
	if err != nil || len(p2.Candidates) != 1 {
		t.Fatalf("multi plan=%#v err=%v", p2, err)
	}
	if _, err := e.Plan(items, []string{"sm-unknown"}); err == nil {
		t.Fatal("unknown ID accepted")
	}
}

func TestPlanIntegrityExpiryHostAndTraversal(t *testing.T) {
	e, items, _, data := testEngine(t)
	p, err := e.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "cleanup", "plans", p.ID+".json")
	b, _ := os.ReadFile(path)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	raw["expires_at"] = fixedNow.Add(time.Hour).Format(time.RFC3339)
	b, _ = json.Marshal(raw)
	_ = os.WriteFile(path, b, 0o600)
	if _, err := e.Apply(p.ID); err == nil {
		t.Fatal("tampered plan accepted")
	}
	p, _ = e.Plan(items, []string{"all-safe"})
	e.Config.Now = func() time.Time { return fixedNow.Add(16 * time.Minute) }
	if _, err := e.Apply(p.ID); err == nil {
		t.Fatal("expired plan accepted")
	}
	e.Config.Now = func() time.Time { return fixedNow }
	e.Config.HostID = "other-host"
	if _, err := e.Apply(p.ID); err == nil {
		t.Fatal("other host accepted")
	}
	if _, err := e.Apply("../" + p.ID); err == nil {
		t.Fatal("traversal accepted")
	}
}

func TestLoadPlanRegistryRejectsMalformedExpiry(t *testing.T) {
	e, items, _, data := testEngine(t)
	p, err := e.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "cleanup", "plans", p.ID+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw["expires_at"] = "not-a-timestamp"
	b, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.loadPlanRegistry(p.ID); err == nil || err.Error() != "invalid plan expiry" {
		t.Fatalf("loadPlanRegistry err=%v", err)
	}
}

func TestProtectRequiresDiscoveryAndBlocksScanPlanApply(t *testing.T) {
	e, items, _, _ := testEngine(t)
	p, _ := e.Plan(items, []string{items[0].ID})
	if err := e.Protect("", "sm-unknown"); err == nil {
		t.Fatal("unknown protect accepted")
	}
	if err := e.Protect("", items[0].ID); err != nil {
		t.Fatal(err)
	}
	rescanned, err := e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	var protected bool
	for _, c := range rescanned {
		if c.ID == items[0].ID {
			protected = c.Decision == Protected
		}
	}
	if !protected {
		t.Fatal("protection not reflected in scan")
	}
	if _, err := e.Plan(rescanned, []string{items[0].ID}); err == nil {
		t.Fatal("protected plan accepted")
	}
	if _, err := e.Apply(p.ID); err == nil {
		t.Fatal("apply ignored later protection")
	}
}

func TestApplyRejectsFingerprintRuleTargetAndDecisionChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Engine, Plan, []Candidate, string, string)
	}{
		{"fingerprint", func(t *testing.T, e *Engine, p Plan, _ []Candidate, home, _ string) {
			_ = os.WriteFile(filepath.Join(home, "Library/Caches/go-build", "new"), []byte("x"), 0o600)
		}},
		{"rule", func(t *testing.T, e *Engine, p Plan, _ []Candidate, _, data string) {
			mutateRegistry(t, data, p.ID, func(r *targetRegistry) { r.Targets[0].RuleVersion = "changed" })
		}},
		{"target", func(t *testing.T, e *Engine, p Plan, _ []Candidate, _, data string) {
			mutateRegistry(t, data, p.ID, func(r *targetRegistry) { r.Targets[0].Path = t.TempDir() })
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, items, home, data := testEngine(t)
			var c Candidate
			for _, x := range items {
				if x.Family == "go-build-cache" {
					c = x
				}
			}
			p, _ := e.Plan(items, []string{c.ID})
			tc.mutate(t, e, p, items, home, data)
			if _, err := e.Apply(p.ID); err == nil {
				t.Fatal("changed candidate accepted")
			}
		})
	}
}

func mutateRegistry(t *testing.T, data, id string, fn func(*targetRegistry)) {
	t.Helper()
	path := filepath.Join(data, "cleanup", "targets", id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r targetRegistry
	if json.Unmarshal(b, &r) != nil {
		t.Fatal("decode")
	}
	fn(&r)
	b, _ = json.Marshal(r)
	if err = os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStatePermissionsUnsafeParentsAndUnknownEntries(t *testing.T) {
	e, items, _, data := testEngine(t)
	p, err := e.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"cleanup", "cleanup/plans", "cleanup/targets"} {
		info, err := os.Stat(filepath.Join(data, dir))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode=%v err=%v", dir, info.Mode(), err)
		}
	}
	info, _ := os.Stat(filepath.Join(data, "cleanup", "plans", p.ID+".json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("plan mode=%o", info.Mode().Perm())
	}
	if err := os.WriteFile(filepath.Join(data, "cleanup", "plans", "junk.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(p.ID); err == nil {
		t.Fatal("unknown state entry accepted")
	}

	home2 := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home2, "Library"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home2, "Library", "Caches")); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWithConfig(ScanConfig{Home: home2, Now: fixedNow}); err == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestStateRejectsSymlinkParentAndNonDirectory(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{filepath.Join(base, "linked", "state"), filepath.Join(base, "file")} {
		if data == filepath.Join(base, "file") {
			if err := os.WriteFile(data, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		e := &Engine{Config: Config{Home: t.TempDir(), DataDir: data, HostID: "host", Now: func() time.Time { return fixedNow }}}
		if err := e.ensureState(); err == nil {
			t.Fatalf("unsafe state path accepted: %s", data)
		}
	}
}

func TestApplyRejectsReplacedRootWithIdenticalContent(t *testing.T) {
	e, items, home, _ := testEngine(t)
	var c Candidate
	for _, item := range items {
		if item.Family == "go-build-cache" {
			c = item
		}
	}
	p, err := e.Plan(items, []string{c.ID})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "Library/Caches/go-build")
	old := root + "-old"
	if err = os.Rename(root, old); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "synthetic.cache"), []byte("Library/Caches/go-build"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(p.ID); err == nil {
		t.Fatal("replacement root identity accepted")
	}
}

func TestChangedApplyWritesFailureReceipt(t *testing.T) {
	e, items, home, _ := testEngine(t)
	p, err := e.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(home, "Library/Caches/go-build", "changed"), []byte("x"), 0o600)
	result, err := e.Apply(p.ID)
	if err == nil {
		t.Fatal("partial apply returned nil error")
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows=%#v", result.Rows)
	}
	history, herr := e.History()
	if herr != nil || len(history) != 1 {
		t.Fatalf("history=%#v err=%v", history, herr)
	}
	if history[0].Result != "blocked_changed" {
		t.Fatalf("history=%#v", history)
	}
}

func TestHistoryRejectsMalformedSymlinkNonregularAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(string) error
	}{
		{"malformed", func(d string) error { return os.WriteFile(filepath.Join(d, "bad.json"), []byte("{"), 0o600) }},
		{"symlink", func(d string) error { return os.Symlink(filepath.Join(d, "missing"), filepath.Join(d, "bad.json")) }},
		{"directory", func(d string) error { return os.Mkdir(filepath.Join(d, "bad.json"), 0o700) }},
		{"unknown", func(d string) error { return os.WriteFile(filepath.Join(d, "unknown.txt"), []byte("x"), 0o600) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, _, data := testEngine(t)
			d := filepath.Join(data, "cleanup", "receipts")
			_ = os.MkdirAll(d, 0o700)
			if err := tc.make(d); err != nil {
				t.Fatal(err)
			}
			if _, err := e.History(); err == nil {
				t.Fatal("unsafe history accepted")
			}
		})
	}
}

func TestHistoryRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	for _, body := range []string{
		`{"schema_version":"stillmac.cleanup.v1","candidate_id":"sm-0000000000000000","rule_version":"r","plan_hash":"h","before_bytes":0,"after_bytes":0,"moved_bytes":0,"removed_bytes":0,"reclaimed_bytes":0,"method":"none","result":"blocked_changed","timestamp":"2026-08-13T10:00:00Z","unexpected":true}`,
		`{"schema_version":"stillmac.cleanup.v1"} {}`,
	} {
		e, _, _, data := testEngine(t)
		dir := filepath.Join(data, "cleanup", "receipts")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.History(); err == nil {
			t.Fatalf("history accepted %s", body)
		}
	}
}

func TestChangedReceiptUsesExactDecision(t *testing.T) {
	e, items, home, _ := testEngine(t)
	var c Candidate
	for _, item := range items {
		if item.Family == "go-build-cache" {
			c = item
		}
	}
	p, _ := e.Plan(items, []string{c.ID})
	_ = os.WriteFile(filepath.Join(home, "Library/Caches/go-build", "changed"), []byte("x"), 0o600)
	result, err := e.Apply(p.ID)
	if err == nil {
		t.Fatal("changed apply succeeded")
	}
	if len(result.Rows) != 1 || result.Rows[0].Decision != BlockedChanged {
		t.Fatalf("rows=%#v", result.Rows)
	}
}

func TestDefaultHostIDIsPrivateAndPersistent(t *testing.T) {
	home, data := cacheFixture(t)
	cleaner := verifiedFakeGoCleaner(home)
	e := &Engine{Config: Config{Home: home, DataDir: data, Now: func() time.Time { return fixedNow }, GoCleaner: cleaner}}
	items, err := e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Plan(items, []string{"all-safe"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "cleanup", "host.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("host mode=%o", info.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "fixture-host") {
		t.Fatal("host fixture leaked")
	}
	var first hostRecord
	if err = json.Unmarshal(b, &first); err != nil || first.ID == "" {
		t.Fatalf("host=%#v err=%v", first, err)
	}
	e2 := &Engine{Config: Config{Home: home, DataDir: data, Now: func() time.Time { return fixedNow }, GoCleaner: cleaner}}
	id, err := e2.hostID()
	if err != nil || id != first.ID {
		t.Fatalf("id=%q want=%q err=%v", id, first.ID, err)
	}
}

func TestPlanRejectsUnknownCleanupStateEntry(t *testing.T) {
	e, items, _, data := testEngine(t)
	if err := os.MkdirAll(filepath.Join(data, "cleanup"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "cleanup", "unknown.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Plan(items, []string{"all-safe"}); err == nil {
		t.Fatal("plan accepted unknown cleanup entry")
	}
}

func TestPlanRejectsMixedAllSafeAndExplicitWithoutStateMutation(t *testing.T) {
	e, items, _, data := testEngine(t)
	if _, err := e.Plan(items, []string{"all-safe", items[0].ID}); err == nil {
		t.Fatal("mixed selection accepted")
	}
	for _, kind := range []string{"plans", "targets", "receipts"} {
		entries, err := os.ReadDir(filepath.Join(data, "cleanup", kind))
		if err == nil && len(entries) != 0 {
			t.Fatalf("%s state mutated: %v", kind, entries)
		}
	}
}

func TestHistoryRejectsTamperedReceiptSemantics(t *testing.T) {
	e, items, _, data := testEngine(t)
	dir := filepath.Join(data, "cleanup", "receipts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := Receipt{SchemaVersion: schemaVersion, CandidateID: items[0].ID, RuleVersion: items[0].RuleVersion, Decision: Safe, PlanHash: "hash", BeforeBytes: 2, AfterBytes: 1, RemovedBytes: 1, ReclaimedBytes: 1, Method: "owner-native-go-clean-cache", Result: "cleaned", Timestamp: fixedNow.Format(time.RFC3339)}
	for name, mutate := range map[string]func(*Receipt){"negative": func(r *Receipt) { r.MovedBytes = -1 }, "wrong-method": func(r *Receipt) { r.Method = "none" }, "wrong-decision": func(r *Receipt) { r.Decision = Review }, "false-reclaimed": func(r *Receipt) { r.ReclaimedBytes = 2 }} {
		t.Run(name, func(t *testing.T) {
			r := valid
			mutate(&r)
			b, _ := json.Marshal(r)
			if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := e.History(); err == nil {
				t.Fatal("tampered receipt accepted")
			}
			_ = os.Remove(filepath.Join(dir, name+".json"))
		})
	}
}

func TestProductionCleanupHasNoFilesystemMutationShellOrArbitraryAction(t *testing.T) {
	var source strings.Builder
	for _, name := range []string{"cleanup.go", "go_cleaner.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(b)
	}
	s := source.String()
	for _, bad := range []string{"os.Remove" + "All", "Root." + "Rename", "exec.Command(\"" + "rm\"", "--" + "force", "sh " + "-c", "bash " + "-c"} {
		if strings.Contains(s, bad) {
			t.Fatalf("production contains %q", bad)
		}
	}
	if strings.Count(s, "os.Rename(") != 1 || !strings.Contains(s, "return os.Rename(name, path)") {
		t.Fatalf("unexpected rename primitive outside atomic state publication")
	}
}

func TestProtectRejectsGitWorktreeAndLeavesStateReadable(t *testing.T) {
	e, _, _, data := testEngine(t)
	scope := filepath.Join(t.TempDir(), "current")
	e.Config.GitRunner = func(args ...string) (GitResult, error) {
		if strings.Contains(strings.Join(args, "\x00"), "worktree\x00list\x00--porcelain") {
			return GitResult{Output: []byte("worktree " + scope + "\nHEAD abc\nbranch refs/heads/main\n")}, nil
		}
		return GitResult{}, nil
	}
	items, err := e.Scan(scope)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range items {
		if c.Family == "git-worktree" {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatal("missing Git candidate")
	}
	if err := e.Protect(scope, id); err == nil {
		t.Fatal("Git protection accepted")
	}
	entries, _ := os.ReadDir(filepath.Join(data, "cleanup", "protected"))
	if len(entries) != 0 {
		t.Fatalf("state mutated: %v", entries)
	}
	if _, err := e.Scan(scope); err != nil {
		t.Fatalf("future scan failed: %v", err)
	}
}

func TestPlanRejectsOrdinalSelection(t *testing.T) {
	e, items, _, _ := testEngine(t)
	if _, err := e.Plan(items, []string{"1"}); err == nil {
		t.Fatal("ordinal selection accepted")
	}
}

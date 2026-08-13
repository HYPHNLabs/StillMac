package cleanup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeGoCleaner struct {
	binding  GoToolBinding
	bindErr  error
	cleanErr error
	calls    []fakeGoCall
	mutate   func() error
}

type fakeGoCall struct {
	binding GoToolBinding
	home    string
	target  string
}

type commandCall struct {
	path string
	args []string
	env  []string
}

func (f *fakeGoCleaner) Bind(home, target string) (GoToolBinding, error) {
	if f.bindErr != nil {
		return GoToolBinding{}, f.bindErr
	}
	b := f.binding
	if b.GoCache == "" {
		b.GoCache = target
	}
	return b, nil
}

func (f *fakeGoCleaner) Clean(binding GoToolBinding, home, target string) error {
	f.calls = append(f.calls, fakeGoCall{binding: binding, home: home, target: target})
	if f.cleanErr != nil {
		return f.cleanErr
	}
	if f.mutate != nil {
		return f.mutate()
	}
	return nil
}

func verifiedFakeGoCleaner(home string) *fakeGoCleaner {
	return &fakeGoCleaner{binding: GoToolBinding{
		Path:        filepath.Join(home, "tools", "go"),
		Device:      7,
		Inode:       11,
		Fingerprint: "fixture-go-executable-fingerprint",
		Version:     "go version go1.23.0 fixture/arm64",
	}}
}

func TestGoIsOnlySafeCandidateWithVerifiedExactGoCache(t *testing.T) {
	home, data := cacheFixture(t)
	cleaner := verifiedFakeGoCleaner(home)
	items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, GoCleaner: cleaner})
	if err != nil {
		t.Fatal(err)
	}
	decisions := map[string]Decision{}
	actions := map[string]string{}
	for _, item := range items {
		decisions[item.Family] = item.Decision
		actions[item.Family] = item.Action
	}
	if decisions["go-build-cache"] != Safe || actions["go-build-cache"] != "owner-native-go-clean-cache" {
		t.Fatalf("go candidate decisions=%#v actions=%#v", decisions, actions)
	}
	if decisions["homebrew-cache"] != Review || actions["homebrew-cache"] != "none" {
		t.Fatalf("homebrew candidate decisions=%#v actions=%#v", decisions, actions)
	}
	if decisions["codex-runtime-cache"] != BlockedActive || actions["codex-runtime-cache"] != "none" {
		t.Fatalf("codex candidate decisions=%#v actions=%#v", decisions, actions)
	}
}

func TestGoUnavailableOrWrongCacheIsBlockedUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cleaner *fakeGoCleaner
	}{
		{"unavailable", &fakeGoCleaner{bindErr: errors.New("go unavailable")}},
		{"wrong-cache", &fakeGoCleaner{binding: GoToolBinding{Path: "/fixture/go", Fingerprint: "x", Version: "go version fixture", GoCache: "/wrong"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, data := cacheFixture(t)
			items, err := ScanWithConfig(ScanConfig{Home: home, DataDir: data, Now: fixedNow, GoCleaner: tc.cleaner})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.Family == "go-build-cache" && (item.Decision != BlockedUnknown || item.Action != "none") {
					t.Fatalf("go candidate=%#v", item)
				}
			}
		})
	}
}

func TestApplyUsesBoundOwnerNativeCleanerAndMeasuresReclaimedBytes(t *testing.T) {
	home, data := cacheFixture(t)
	root := filepath.Join(home, "Library", "Caches", "go-build")
	cleaner := verifiedFakeGoCleaner(home)
	cleaner.mutate = func() error {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	e := &Engine{Config: Config{Home: home, DataDir: data, HostID: "fixture-host", Now: func() time.Time { return fixedNow }, GoCleaner: cleaner}}
	items, err := e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Plan(items, []string{"all-safe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Candidates) != 1 || p.Candidates[0].Family != "go-build-cache" {
		t.Fatalf("plan candidates=%#v", p.Candidates)
	}
	publicPlan, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{home, root, cleaner.binding.Path, cleaner.binding.Version} {
		if strings.Contains(string(publicPlan), private) {
			t.Fatalf("public plan exposed private Go binding: %s", publicPlan)
		}
	}
	var registry targetRegistry
	if err := readStrictJSON(filepath.Join(data, "cleanup", "targets", p.ID+".json"), &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Targets) != 1 || registry.Targets[0].GoTool == nil || !reflect.DeepEqual(*registry.Targets[0].GoTool, cleaner.bindingWithCache(root)) {
		t.Fatalf("private registry did not bind Go tool: %#v", registry)
	}
	result, err := e.Apply(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaner.calls) != 1 {
		t.Fatalf("clean calls=%#v", cleaner.calls)
	}
	call := cleaner.calls[0]
	if call.home != home || call.target != root || !reflect.DeepEqual(call.binding, cleaner.bindingWithCache(root)) {
		t.Fatalf("clean call=%#v", call)
	}
	r := result.Rows[0]
	if r.Result != "cleaned" || r.Method != "owner-native-go-clean-cache" || r.BeforeBytes <= 0 || r.AfterBytes != 0 || r.RemovedBytes != r.BeforeBytes || r.ReclaimedBytes != r.BeforeBytes || r.MovedBytes != 0 {
		t.Fatalf("receipt=%#v", r)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("owner-native cleaner removed exact root: %v", err)
	}
}

func (f *fakeGoCleaner) bindingWithCache(target string) GoToolBinding {
	b := f.binding
	b.GoCache = target
	return b
}

func TestApplyRejectsExecutableBindingChangeAndOwnerFailureHasNoFalseReclaim(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*fakeGoCleaner)
		wantResult string
	}{
		{"binding-changed", func(f *fakeGoCleaner) { f.binding.Inode++ }, "blocked_changed"},
		{"owner-failed", func(f *fakeGoCleaner) { f.cleanErr = errors.New("go clean failed") }, "owner_action_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, data := cacheFixture(t)
			cleaner := verifiedFakeGoCleaner(home)
			e := &Engine{Config: Config{Home: home, DataDir: data, HostID: "fixture-host", Now: func() time.Time { return fixedNow }, GoCleaner: cleaner}}
			items, err := e.Scan("")
			if err != nil {
				t.Fatal(err)
			}
			p, err := e.Plan(items, []string{"all-safe"})
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(cleaner)
			result, err := e.Apply(p.ID)
			if err == nil {
				t.Fatal("apply unexpectedly succeeded")
			}
			if len(result.Rows) != 1 || result.Rows[0].Result != tc.wantResult || result.Rows[0].RemovedBytes != 0 || result.Rows[0].ReclaimedBytes != 0 || result.Rows[0].MovedBytes != 0 {
				t.Fatalf("result=%#v", result)
			}
			history, historyErr := e.History()
			if historyErr != nil || len(history) != 1 || history[0].Result != tc.wantResult {
				t.Fatalf("history=%#v err=%v", history, historyErr)
			}
		})
	}
}

func TestNativeGoCleanerUsesResolvedExecutableAndExactSanitizedCommands(t *testing.T) {
	home, _ := cacheFixture(t)
	target := filepath.Join(home, "Library", "Caches", "go-build")
	tools, err := os.MkdirTemp(".", ".go-tool-test-")
	if err != nil {
		t.Fatal(err)
	}
	tools, err = filepath.Abs(tools)
	if err != nil {
		t.Fatal(err)
	}
	realGo := filepath.Join(tools, "go-real")
	if err := os.WriteFile(realGo, []byte("synthetic executable fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	goLink := filepath.Join(tools, "go")
	if err := os.Symlink("go-real", goLink); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(goLink)
		_ = os.Remove(realGo)
		_ = os.Remove(tools)
	})
	var calls []commandCall
	runner := func(path string, args, env []string) (GoCommandResult, error) {
		calls = append(calls, commandCall{path: path, args: append([]string(nil), args...), env: append([]string(nil), env...)})
		switch strings.Join(args, " ") {
		case "version":
			return GoCommandResult{Output: []byte("go version go1.23.0 fixture/arm64\n")}, nil
		case "env GOCACHE":
			return GoCommandResult{Output: []byte(target + "\n")}, nil
		case "clean -cache":
			return GoCommandResult{}, nil
		default:
			return GoCommandResult{}, errors.New("unexpected command")
		}
	}
	cleaner := &nativeGoCleaner{runner: runner, candidates: []string{goLink}}
	binding, err := cleaner.Bind(home, target)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Path != realGo || binding.GoCache != target || binding.Fingerprint == "" || binding.Device == 0 || binding.Inode == 0 {
		t.Fatalf("binding=%#v", binding)
	}
	if err := cleaner.Clean(binding, home, target); err != nil {
		t.Fatal(err)
	}
	wantEnv := []string{"HOME=" + home, "GOCACHE=" + target, "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "PATH=/usr/bin:/bin"}
	wantArgs := [][]string{{"version"}, {"env", "GOCACHE"}, {"version"}, {"env", "GOCACHE"}, {"clean", "-cache"}}
	if len(calls) != len(wantArgs) {
		t.Fatalf("calls=%#v", calls)
	}
	for i, call := range calls {
		if call.path != realGo || !reflect.DeepEqual(call.args, wantArgs[i]) || !reflect.DeepEqual(call.env, wantEnv) {
			t.Fatalf("call[%d]=%#v", i, call)
		}
	}
}

func TestNativeGoCleanerRejectsDifferentCacheAndExecutableFingerprintChange(t *testing.T) {
	home, _ := cacheFixture(t)
	target := filepath.Join(home, "Library", "Caches", "go-build")
	tools, err := os.MkdirTemp(".", ".go-tool-test-")
	if err != nil {
		t.Fatal(err)
	}
	tools, err = filepath.Abs(tools)
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(tools, "go")
	if err := os.WriteFile(tool, []byte("first fixture executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tool)
		_ = os.Remove(tools)
	})
	wrongCache := true
	cleanCalls := 0
	runner := func(_ string, args, _ []string) (GoCommandResult, error) {
		switch strings.Join(args, " ") {
		case "version":
			return GoCommandResult{Output: []byte("go version go1.23.0 fixture/arm64")}, nil
		case "env GOCACHE":
			if wrongCache {
				return GoCommandResult{Output: []byte(filepath.Join(home, "wrong"))}, nil
			}
			return GoCommandResult{Output: []byte(target)}, nil
		case "clean -cache":
			cleanCalls++
			return GoCommandResult{}, nil
		}
		return GoCommandResult{}, errors.New("unexpected command")
	}
	cleaner := &nativeGoCleaner{runner: runner, candidates: []string{tool}}
	if err := os.Chmod(tools, 0o707); err != nil {
		t.Fatal(err)
	}
	if _, err := cleaner.Bind(home, target); err == nil {
		t.Fatal("world-writable executable path accepted")
	}
	if err := os.Chmod(tools, 0o770); err != nil {
		t.Fatal(err)
	}
	if _, err := cleaner.Bind(home, target); err == nil {
		t.Fatal("different GOCACHE accepted")
	}
	wrongCache = false
	binding, err := cleaner.Bind(home, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("changed fixture executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cleaner.Clean(binding, home, target); err == nil {
		t.Fatal("changed executable accepted")
	}
	if cleanCalls != 0 {
		t.Fatalf("owner action invoked after fingerprint change: %d", cleanCalls)
	}
}

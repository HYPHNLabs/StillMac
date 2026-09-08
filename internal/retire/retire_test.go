package retire

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRegistrationNeedsExplicitOwnerReleaseBeforeApproval(t *testing.T) {
	mainRoot, linked := linkedWorktreeFixture(t)
	dataDir := filepath.Join(t.TempDir(), "state")
	fixed := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	e := New(Config{
		Home:        t.TempDir(),
		DataDir:     dataDir,
		CurrentPath: mainRoot,
		Now:         func() time.Time { return fixed },
	})

	registration, err := e.Register(RegisterRequest{Path: linked, OwnershipAttestation: OwnerManagedAttestation})
	if err != nil {
		t.Fatal(err)
	}
	if registration.Decision != DecisionReview {
		t.Fatalf("registration=%#v, want decision %q", registration, DecisionReview)
	}
	if len(registration.Reasons) == 0 || registration.Reasons[0] != ReasonOwnerReleaseRequired {
		t.Fatalf("reasons=%#v", registration.Reasons)
	}

	plan, err := e.Plan(PlanRequest{RegistrationID: registration.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Approve(plan.PlanID); CodeOf(err) != CodeBlocked {
		t.Fatalf("approve error=%v, code=%q, want %q", err, CodeOf(err), CodeBlocked)
	}
}

func TestRetireAndRecoverPreservesCommitButNotIgnoredOrUntrackedFiles(t *testing.T) {
	fixture := newRetireFixture(t)
	registration, plan := readyPlan(t, fixture)
	if registration.Decision != DecisionReady || plan.Registration.Decision != DecisionReady {
		t.Fatalf("registration=%#v plan=%#v", registration, plan)
	}
	publicPlan, _ := json.Marshal(plan)
	for _, private := range []string{fixture.main, fixture.linked, fixture.dataDir} {
		if strings.Contains(string(publicPlan), private) {
			t.Fatalf("public plan leaked private path %q: %s", private, publicPlan)
		}
	}

	result, err := fixture.engine.Apply(plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Result != ResultRemoved || result.Receipt.Recovery != RecoveryTrackedCommit {
		t.Fatalf("receipt=%#v", result.Receipt)
	}
	if _, err := os.Lstat(fixture.linked); !os.IsNotExist(err) {
		t.Fatalf("removed root stat error=%v", err)
	}
	public, _ := json.Marshal(result)
	for _, private := range []string{fixture.main, fixture.linked, fixture.dataDir} {
		if strings.Contains(string(public), private) {
			t.Fatalf("public result leaked private path %q: %s", private, public)
		}
	}

	recovery, err := fixture.engine.Recover(result.Receipt.ReceiptID)
	if err != nil {
		history, historyErr := fixture.engine.History()
		list, listErr := fixture.engine.worktreeList(fixture.main)
		_, statErr := os.Stat(fixture.linked)
		raw := gitFixtureOutput(t, fixture.main, "worktree", "list", "--porcelain", "-z")
		t.Fatalf("recovery error=%v result=%#v history=%#v historyErr=%v list=%#v listErr=%v statErr=%v raw=%q", err, recovery, history, historyErr, list, listErr, statErr, raw)
	}
	if recovery.Result != ResultRecovered || recovery.RestoredIgnoredUntracked || recovery.Message != ReasonRecoveryLimit {
		t.Fatalf("recovery=%#v", recovery)
	}
	if info, err := os.Stat(fixture.linked); err != nil || !info.IsDir() {
		t.Fatalf("recreated root info=%v err=%v", info, err)
	}
	history, err := fixture.engine.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].Result != ResultRemoved || history[1].Result != ResultRecovered {
		t.Fatalf("history=%#v", history)
	}
	listed, err := fixture.engine.List()
	if err != nil || len(listed) != 1 || listed[0].Decision != DecisionReview || !containsReason(listed[0].Reasons, ReasonOwnerReleaseRequired) {
		t.Fatalf("post-recovery registration=%#v err=%v", listed, err)
	}
}

func TestChangedIndexRefLockAndIgnoredStateBlockApply(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, retireFixture)
		reason string
	}{
		{
			name: "changed-index",
			mutate: func(t *testing.T, fixture retireFixture) {
				if err := os.WriteFile(filepath.Join(fixture.linked, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				gitFixtureCommand(t, fixture.linked, "add", "staged.txt")
			},
			reason: ReasonTrackedChanges,
		},
		{
			name: "changed-ref",
			mutate: func(t *testing.T, fixture retireFixture) {
				gitFixtureCommand(t, fixture.linked, "commit", "--allow-empty", "-qm", "ref changed")
			},
			reason: ReasonPlanChanged,
		},
		{
			name: "lock",
			mutate: func(t *testing.T, fixture retireFixture) {
				gitFixtureCommand(t, fixture.main, "worktree", "lock", fixture.linked)
			},
			reason: ReasonLocked,
		},
		{
			name: "ignored",
			mutate: func(t *testing.T, fixture retireFixture) {
				if err := os.WriteFile(filepath.Join(fixture.linked, "ignored.txt"), []byte("ignored\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			reason: ReasonIgnoredChanges,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetireFixture(t)
			if test.name == "ignored" {
				if err := os.WriteFile(filepath.Join(fixture.linked, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				gitFixtureCommand(t, fixture.linked, "add", ".gitignore")
				gitFixtureCommand(t, fixture.linked, "commit", "-qm", "ignore rule")
			}
			_, plan := readyPlan(t, fixture)
			test.mutate(t, fixture)
			result, err := fixture.engine.Apply(plan.PlanID)
			if err == nil || (CodeOf(err) != CodeChanged && CodeOf(err) != CodeBlocked) {
				t.Fatalf("apply result=%#v err=%v code=%q", result, err, CodeOf(err))
			}
			if result.Receipt.Result != ResultBlockedChanged {
				t.Fatalf("receipt=%#v", result.Receipt)
			}
			if test.reason == ReasonPlanChanged && !containsReason(result.Receipt.Reasons, ReasonPlanChanged) {
				t.Fatalf("ref change reasons=%#v", result.Receipt.Reasons)
			}
			if test.reason != ReasonPlanChanged && !containsReason(result.Receipt.Reasons, test.reason) {
				t.Fatalf("reasons=%#v want %q", result.Receipt.Reasons, test.reason)
			}
			if _, statErr := os.Stat(fixture.linked); statErr != nil {
				t.Fatalf("blocked target missing: %v", statErr)
			}
		})
	}
}

func TestTamperedPlanRegistryExpiryAndProtectionFailClosed(t *testing.T) {
	t.Run("tampered-plan", func(t *testing.T) {
		fixture := newRetireFixture(t)
		_, plan := readyPlan(t, fixture)
		var stored Plan
		readJSONForTest(t, fixture.engine.statePath("plans", plan.PlanID), &stored)
		stored.RegistrationID = "retire-00000000000000000000000000000000"
		writeJSONForTest(t, fixture.engine.statePath("plans", plan.PlanID), stored)
		if _, err := fixture.engine.Apply(plan.PlanID); CodeOf(err) != CodeTampered {
			t.Fatalf("tampered plan error=%v code=%q", err, CodeOf(err))
		}
	})

	t.Run("tampered-registry", func(t *testing.T) {
		fixture := newRetireFixture(t)
		_, plan := readyPlan(t, fixture)
		var target privateTarget
		readJSONForTest(t, fixture.engine.statePath("targets", plan.PlanID), &target)
		target.Snapshot.HeadCommit = strings.Repeat("0", 40)
		writeJSONForTest(t, fixture.engine.statePath("targets", plan.PlanID), target)
		if _, err := fixture.engine.Apply(plan.PlanID); CodeOf(err) != CodeTampered {
			t.Fatalf("tampered registry error=%v code=%q", err, CodeOf(err))
		}
	})

	t.Run("expired", func(t *testing.T) {
		fixture := newRetireFixture(t)
		_, plan := readyPlanWithoutApproval(t, fixture)
		*fixture.now = fixture.now.Add(PlanTTL)
		if _, err := fixture.engine.Approve(plan.PlanID); CodeOf(err) != CodeExpired {
			t.Fatalf("expired approval error=%v code=%q", err, CodeOf(err))
		}
	})

	t.Run("release-invalidates-earlier-plan", func(t *testing.T) {
		fixture := newRetireFixture(t)
		registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil {
			t.Fatal(err)
		}
		plan, err := fixture.engine.Plan(PlanRequest{RegistrationID: registration.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.engine.Release(ReleaseRequest{RegistrationID: registration.ID, Attestation: OwnerReleaseAttestation}); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.engine.Approve(plan.PlanID); CodeOf(err) != CodeBlocked {
			t.Fatalf("old plan approval error=%v code=%q", err, CodeOf(err))
		}
	})

	t.Run("release-expiry-requires-new-attestation", func(t *testing.T) {
		fixture := newRetireFixture(t)
		registerAndRelease(t, fixture)
		*fixture.now = fixture.now.Add(20 * time.Minute)
		plan, err := fixture.engine.Plan(PlanRequest{RegistrationID: registrationIDForFixture(t, fixture)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.engine.Approve(plan.PlanID); err != nil {
			t.Fatal(err)
		}
		*fixture.now = fixture.now.Add(11 * time.Minute)
		result, err := fixture.engine.Apply(plan.PlanID)
		if CodeOf(err) != CodeChanged || result.Receipt.Result != ResultBlockedChanged || !containsReason(result.Receipt.Reasons, ReasonOwnerReleaseExpired) {
			t.Fatalf("expired release result=%#v err=%v code=%q", result, err, CodeOf(err))
		}
	})

	t.Run("protected-after-approval", func(t *testing.T) {
		fixture := newRetireFixture(t)
		registration, plan := readyPlan(t, fixture)
		if err := fixture.engine.Protect(registration.ID); err != nil {
			t.Fatal(err)
		}
		result, err := fixture.engine.Apply(plan.PlanID)
		if CodeOf(err) != CodeBlocked || result.Receipt.Result != ResultBlockedChanged || !containsReason(result.Receipt.Reasons, ReasonProtected) {
			t.Fatalf("protected result=%#v err=%v code=%q", result, err, CodeOf(err))
		}
		if err := fixture.engine.Unprotect(registration.ID); err != nil {
			t.Fatal(err)
		}
		listed, err := fixture.engine.List()
		if err != nil || len(listed) != 1 || listed[0].Protected {
			t.Fatalf("listed=%#v err=%v", listed, err)
		}
	})
}

func TestFailedNativeGitCommandWritesReceiptAndNeverUsesForce(t *testing.T) {
	fixture := newRetireFixture(t)
	_, plan := readyPlan(t, fixture)
	var removeArgs []string
	fixture.engine.Config.GitRunner = func(dir string, args []string, env []string) (GitResult, error) {
		if containsArg(args, "remove") {
			removeArgs = append([]string(nil), args...)
			return GitResult{ExitCode: 128}, nil
		}
		return NativeGitRunner(dir, args, env)
	}
	result, err := fixture.engine.Apply(plan.PlanID)
	if CodeOf(err) != CodeActionFailed || result.Receipt.Result != ResultNativeActionFailed {
		t.Fatalf("result=%#v err=%v code=%q", result, err, CodeOf(err))
	}
	if containsArg(removeArgs, "--force") {
		t.Fatalf("remove args contain force: %#v", removeArgs)
	}
	if _, err := os.Stat(fixture.linked); err != nil {
		t.Fatalf("failed action removed target: %v", err)
	}
	history, err := fixture.engine.History()
	if err != nil || len(history) != 1 || history[0].Result != ResultNativeActionFailed {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestOwnershipAgentRootAndUnsafeEntryGates(t *testing.T) {
	t.Run("ownership-attestation", func(t *testing.T) {
		fixture := newRetireFixture(t)
		if _, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked}); CodeOf(err) != CodeInvalidRequest {
			t.Fatalf("missing ownership attestation error=%v code=%q", err, CodeOf(err))
		}
	})

	t.Run("current-path-unavailable", func(t *testing.T) {
		fixture := newRetireFixture(t)
		fixture.engine.Config.CurrentPath = "not-an-absolute-path"
		registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonGitUnavailable) {
			t.Fatalf("registration=%#v err=%v", registration, err)
		}
	})

	t.Run("main-and-current-worktrees", func(t *testing.T) {
		fixture := newRetireFixture(t)
		mainRegistration, err := fixture.engine.Register(RegisterRequest{Path: fixture.main, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || !containsReason(mainRegistration.Reasons, ReasonMainWorktree) {
			t.Fatalf("main registration=%#v err=%v", mainRegistration, err)
		}
		fixture.engine.Config.CurrentPath = fixture.linked
		currentRegistration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || !containsReason(currentRegistration.Reasons, ReasonCurrentWorktree) {
			t.Fatalf("current registration=%#v err=%v", currentRegistration, err)
		}
	})

	t.Run("known-agent-root", func(t *testing.T) {
		mainRoot, _ := linkedWorktreeFixture(t)
		home := physicalTempDir(t)
		agentRoot := filepath.Join(home, ".codex")
		if err := os.Mkdir(agentRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		linked := filepath.Join(agentRoot, "linked")
		gitFixtureCommand(t, mainRoot, "worktree", "add", "-q", "-b", "agent-feature", linked)
		dataDir := filepath.Join(physicalTempDir(t), "state")
		e := New(Config{Home: home, DataDir: dataDir, CurrentPath: mainRoot})
		registration, err := e.Register(RegisterRequest{Path: linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonAgentRoot) {
			t.Fatalf("registration=%#v err=%v", registration, err)
		}
	})

	t.Run("symlink-entry", func(t *testing.T) {
		fixture := newRetireFixture(t)
		if err := os.Symlink("tracked.txt", filepath.Join(fixture.linked, "synthetic-link")); err != nil {
			t.Fatal(err)
		}
		registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonSymlinkOrSpecial) {
			t.Fatalf("registration=%#v err=%v", registration, err)
		}
	})

	t.Run("special-entry", func(t *testing.T) {
		fixture := newRetireFixture(t)
		if err := syscall.Mkfifo(filepath.Join(fixture.linked, "synthetic-fifo"), 0o600); err != nil {
			t.Fatal(err)
		}
		registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonSymlinkOrSpecial) {
			t.Fatalf("registration=%#v err=%v", registration, err)
		}
	})

	t.Run("submodule", func(t *testing.T) {
		fixture := newRetireFixture(t)
		submoduleRoot := filepath.Join(physicalTempDir(t), "submodule")
		if err := os.Mkdir(submoduleRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		gitFixtureCommand(t, submoduleRoot, "init", "-q", "-b", "main")
		gitFixtureCommand(t, submoduleRoot, "config", "user.name", "StillMac Fixture")
		gitFixtureCommand(t, submoduleRoot, "config", "user.email", "stillmac-fixture@example.invalid")
		if err := os.WriteFile(filepath.Join(submoduleRoot, "module.txt"), []byte("module\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitFixtureCommand(t, submoduleRoot, "add", "module.txt")
		gitFixtureCommand(t, submoduleRoot, "commit", "-qm", "module")
		gitFixtureCommand(t, fixture.linked, "-c", "protocol.file.allow=always", "submodule", "add", "-q", submoduleRoot, "module")
		gitFixtureCommand(t, fixture.linked, "commit", "-qm", "submodule")
		registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonSubmodule) {
			t.Fatalf("registration=%#v err=%v", registration, err)
		}
	})

	t.Run("unpreserved-detached-commit", func(t *testing.T) {
		fixture := newRetireFixture(t)
		detached := filepath.Join(filepath.Dir(fixture.linked), "detached")
		gitFixtureCommand(t, fixture.main, "worktree", "add", "-q", "--detach", detached)
		gitFixtureCommand(t, detached, "commit", "--allow-empty", "-qm", "unpreserved")
		registration, err := fixture.engine.Register(RegisterRequest{Path: detached, OwnershipAttestation: OwnerManagedAttestation})
		if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonCommitNotPreserved) {
			t.Fatalf("registration=%#v err=%v", registration, err)
		}
	})
}

func TestGitFilterIncludeAndWorktreeConfigAreBlockedBeforeHelpersRun(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, retireFixture, string)
	}{
		{
			name: "filter",
			setup: func(t *testing.T, fixture retireFixture, marker string) {
				gitFixtureCommand(t, fixture.main, "config", "filter.synthetic.process", "touch "+marker)
			},
		},
		{
			name: "include-if",
			setup: func(t *testing.T, fixture retireFixture, marker string) {
				included := filepath.Join(filepath.Dir(marker), "included.gitconfig")
				if err := os.WriteFile(included, []byte("[filter \"synthetic\"]\nprocess = touch "+marker+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				gitFixtureCommand(t, fixture.main, "config", "includeIf.gitdir:synthetic.path", included)
			},
		},
		{
			name: "worktree-config",
			setup: func(t *testing.T, fixture retireFixture, marker string) {
				gitFixtureCommand(t, fixture.main, "config", "extensions.worktreeConfig", "true")
				gitFixtureCommand(t, fixture.main, "config", "--worktree", "filter.synthetic.process", "touch "+marker)
			},
		},
		{
			name: "partial-clone",
			setup: func(t *testing.T, fixture retireFixture, marker string) {
				gitFixtureCommand(t, fixture.main, "config", "extensions.partialClone", "origin")
			},
		},
		{
			name: "promisor",
			setup: func(t *testing.T, fixture retireFixture, marker string) {
				gitFixtureCommand(t, fixture.main, "config", "remote.origin.promisor", "true")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetireFixture(t)
			marker := filepath.Join(physicalTempDir(t), "helper-ran")
			test.setup(t, fixture, marker)
			registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
			if err != nil || registration.Decision != DecisionBlocked || !containsReason(registration.Reasons, ReasonUnsupportedGitConfig) {
				t.Fatalf("registration=%#v err=%v", registration, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("hostile helper marker exists, stat err=%v", err)
			}
			public, _ := json.Marshal(registration)
			if strings.Contains(string(public), marker) {
				t.Fatalf("public registration leaked hostile path: %s", public)
			}
		})
	}
}

type retireFixture struct {
	engine  *Engine
	main    string
	linked  string
	dataDir string
	now     *time.Time
}

func newRetireFixture(t *testing.T) retireFixture {
	t.Helper()
	mainRoot, linked := linkedWorktreeFixture(t)
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	dataDir := filepath.Join(physicalTempDir(t), "state")
	home := physicalTempDir(t)
	return retireFixture{
		engine: New(Config{
			Home:        home,
			DataDir:     dataDir,
			CurrentPath: mainRoot,
			Now:         func() time.Time { return *(&now) },
		}),
		main:    mainRoot,
		linked:  linked,
		dataDir: dataDir,
		now:     &now,
	}
}

func readyPlan(t *testing.T, fixture retireFixture) (Registration, Plan) {
	t.Helper()
	registration := registerAndRelease(t, fixture)
	plan, err := fixture.engine.Plan(PlanRequest{RegistrationID: registration.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Approve(plan.PlanID); err != nil {
		t.Fatal(err)
	}
	return registration, plan
}

func readyPlanWithoutApproval(t *testing.T, fixture retireFixture) (Registration, Plan) {
	t.Helper()
	registration := registerAndRelease(t, fixture)
	plan, err := fixture.engine.Plan(PlanRequest{RegistrationID: registration.ID})
	if err != nil {
		t.Fatal(err)
	}
	return registration, plan
}

func registerAndRelease(t *testing.T, fixture retireFixture) Registration {
	t.Helper()
	registration, err := fixture.engine.Register(RegisterRequest{Path: fixture.linked, OwnershipAttestation: OwnerManagedAttestation})
	if err != nil {
		t.Fatal(err)
	}
	registration, err = fixture.engine.Release(ReleaseRequest{RegistrationID: registration.ID, Attestation: OwnerReleaseAttestation})
	if err != nil {
		t.Fatalf("release registration=%#v err=%v", registration, err)
	}
	if registration.Decision != DecisionReady {
		t.Fatalf("released registration=%#v", registration)
	}
	return registration
}

func registrationIDForFixture(t *testing.T, fixture retireFixture) string {
	t.Helper()
	listed, err := fixture.engine.List()
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	return listed[0].ID
}

func physicalTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func readJSONForTest(t *testing.T, path string, value any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, value); err != nil {
		t.Fatal(err)
	}
}

func writeJSONForTest(t *testing.T, path string, value any) {
	t.Helper()
	if err := atomicJSON(path, value); err != nil {
		t.Fatal(err)
	}
}

func linkedWorktreeFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	mainRoot := filepath.Join(root, "main")
	linked := filepath.Join(root, "linked")
	if err := os.Mkdir(mainRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	gitFixtureCommand(t, mainRoot, "init", "-q", "-b", "main")
	gitFixtureCommand(t, mainRoot, "config", "user.name", "StillMac Fixture")
	gitFixtureCommand(t, mainRoot, "config", "user.email", "stillmac-fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(mainRoot, "tracked.txt"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixtureCommand(t, mainRoot, "add", "tracked.txt")
	gitFixtureCommand(t, mainRoot, "commit", "-qm", "fixture")
	gitFixtureCommand(t, mainRoot, "worktree", "add", "-q", "-b", "feature", linked)
	return mainRoot, linked
}

func gitFixtureCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func gitFixtureOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return output
}

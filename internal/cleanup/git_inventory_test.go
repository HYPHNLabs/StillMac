package cleanup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInventoryIgnoresPATHAndInheritedGitConfiguration(t *testing.T) {
	root := inventoryFixture(t)
	fake := t.TempDir()
	marker := filepath.Join(fake, "executed")
	if err := os.WriteFile(filepath.Join(fake, "git"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	result, err := NativeInventoryGitRunner("-C", root, "worktree", "list", "--porcelain")
	if err != nil || result.ExitCode != 0 || !strings.Contains(string(result.Output), "worktree ") {
		t.Fatalf("trusted probe failed: %v %+v", err, result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("executed inherited PATH binary")
	}
}

func TestNativeInventoryBlocksEffectiveHelperAndPartialCloneConfiguration(t *testing.T) {
	for _, kind := range []string{"filter", "include", "worktree", "promisor"} {
		t.Run(kind, func(t *testing.T) {
			root := inventoryFixture(t)
			marker := filepath.Join(root, "helper-executed")
			switch kind {
			case "filter":
				inventoryGit(t, root, "config", "filter.hostile.clean", "touch '"+marker+"'")
			case "include":
				config := filepath.Join(t.TempDir(), "config")
				if err := os.WriteFile(config, []byte("[filter \"hostile\"]\n clean = touch '"+marker+"'\n"), 0600); err != nil {
					t.Fatal(err)
				}
				inventoryGit(t, root, "config", "include.path", config)
			case "worktree":
				inventoryGit(t, root, "config", "extensions.worktreeConfig", "true")
				inventoryGit(t, root, "config", "--worktree", "core.fsmonitor", "touch '"+marker+"'")
			case "promisor":
				inventoryGit(t, root, "config", "remote.origin.promisor", "true")
			}
			if _, err := NativeInventoryGitRunner("-C", root, "status", "--porcelain"); err == nil {
				t.Fatal("unsafe effective configuration accepted")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("configured helper ran")
			}
		})
	}
}

func TestNativeInventoryRejectsMutationCommands(t *testing.T) {
	root := inventoryFixture(t)
	if _, err := NativeInventoryGitRunner("-C", root, "worktree", "remove", root); err == nil {
		t.Fatal("mutation accepted")
	}
}

func inventoryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	inventoryGit(t, root, "init", "-q")
	return root
}
func inventoryGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("/usr/bin/git", append([]string{"-C", root}, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
}

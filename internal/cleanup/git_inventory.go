package cleanup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const inventoryOutputLimit = 1 << 20

var errInventoryGit = errors.New("bounded Git inventory unavailable")

// NativeInventoryGitRunner runs only read-only inventory probes, without
// inherited executable resolution, Git config injection or configured helpers.
func NativeInventoryGitRunner(args ...string) (GitResult, error) {
	if len(args) < 3 || args[0] != "-C" || !filepath.IsAbs(args[1]) {
		return GitResult{}, errInventoryGit
	}
	switch args[2] {
	case "worktree":
		if len(args) != 5 || args[3] != "list" || args[4] != "--porcelain" {
			return GitResult{}, errInventoryGit
		}
	case "status":
		if len(args) != 4 || args[3] != "--porcelain" {
			return GitResult{}, errInventoryGit
		}
	case "merge-base":
		if len(args) != 6 || args[3] != "--is-ancestor" || args[4] != "HEAD" || strings.HasPrefix(args[5], "-") {
			return GitResult{}, errInventoryGit
		}
	case "for-each-ref":
		if len(args) != 6 || args[3] != "--format=%(refname)" || args[4] != "refs/heads/main" || args[5] != "refs/heads/master" {
			return GitResult{}, errInventoryGit
		}
	case "symbolic-ref":
		if len(args) != 5 || args[3] != "--quiet" || args[4] != "refs/remotes/origin/HEAD" {
			return GitResult{}, errInventoryGit
		}
	case "rev-parse":
		if !(len(args) == 4 && args[3] == "--show-toplevel") && !(len(args) == 6 && args[3] == "--verify" && args[4] == "--end-of-options") {
			return GitResult{}, errInventoryGit
		}
	default:
		return GitResult{}, errInventoryGit
	}
	// Effective config includes per-worktree settings. Configuration reads cannot
	// execute filters; reject includes too rather than assume what they contain.
	if args[2] != "worktree" {
		config, err := boundedInventoryGit("-C", args[1], "config", "--includes", "--name-only", "--get-regexp", `^(filter\.|include\.|includeif\.|core\.(fsmonitor|hookspath|attributesfile)|remote\..*\.promisor|extensions\.partialclone)`)
		if err != nil || (config.ExitCode != 0 && config.ExitCode != 1) || len(config.Output) != 0 {
			return GitResult{}, errInventoryGit
		}
	}
	options := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false"}
	return boundedInventoryGit(append(options, args...)...)
}

func boundedInventoryGit(args ...string) (GitResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/git", args...)
	cmd.WaitDelay = time.Second
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat", "GIT_ATTR_NOSYSTEM=1"}
	out := &inventoryOutput{}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if ctx.Err() != nil || out.overflow {
		return GitResult{}, errInventoryGit
	}
	if err == nil {
		return GitResult{Output: out.Bytes()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return GitResult{Output: out.Bytes(), ExitCode: exitErr.ExitCode()}, nil
	}
	return GitResult{}, errInventoryGit
}

type inventoryOutput struct {
	bytes.Buffer
	overflow bool
}

func (w *inventoryOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > inventoryOutputLimit {
		w.overflow = true
		return 0, errInventoryGit
	}
	return w.Buffer.Write(p)
}

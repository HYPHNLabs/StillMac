package retire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var (
	errUnsafeTree = errors.New("unsafe worktree entry")
	errMissingRef = errors.New("preserved reference unavailable")
)

func NativeGitRunner(dir string, args []string, env []string) (GitResult, error) {
	gitPath, err := resolveGitExecutable()
	if err != nil {
		return GitResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, gitPath, args...)
	command.Dir = dir
	command.Env = append([]string(nil), env...)
	var output limitedOutput
	command.Stdout = &output
	command.Stderr = &output
	runErr := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return GitResult{}, errors.New("native Git command timed out")
	}
	if errors.Is(runErr, errGitOutputLimit) {
		return GitResult{}, errGitOutputLimit
	}
	if runErr == nil {
		return GitResult{Output: output.Bytes()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return GitResult{Output: output.Bytes(), ExitCode: exitErr.ExitCode()}, nil
	}
	return GitResult{}, runErr
}

const maxGitOutput = 1 << 20

var errGitOutputLimit = errors.New("native Git output exceeded limit")

type limitedOutput struct {
	buffer bytes.Buffer
}

func (o *limitedOutput) Write(value []byte) (int, error) {
	if o.buffer.Len()+len(value) > maxGitOutput {
		return 0, errGitOutputLimit
	}
	return o.buffer.Write(value)
}

func (o *limitedOutput) Bytes() []byte { return o.buffer.Bytes() }

func resolveGitExecutable() (string, error) {
	for _, candidate := range []string{
		"/usr/bin/git",
		"/opt/homebrew/bin/git",
		"/usr/local/bin/git",
		"/opt/local/bin/git",
	} {
		info, err := os.Lstat(candidate)
		if err != nil || info.Mode()&os.ModeSymlink == 0 && !info.Mode().IsRegular() {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil || !secureExecutablePath(resolved) {
			continue
		}
		return resolved, nil
	}
	return "", errors.New("trusted native Git unavailable")
}

func secureExecutablePath(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	uid := uint32(os.Getuid())
	cur := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if i < len(parts)-1 {
			if !trustedOwnerMode(info, uid) || !info.IsDir() {
				return false
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || !trustedOwnerMode(info, uid) {
			return false
		}
	}
	return true
}

func trustedOwnerMode(info os.FileInfo, uid uint32) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	return st.Uid == uid || st.Uid == 0
}

func gitEnvironment() []string {
	return []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"GIT_EDITOR=:",
		"GIT_SEQUENCE_EDITOR=:",
		"GIT_ASKPASS=/usr/bin/false",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_LAZY_FETCH=1",
		"PATH=/usr/bin:/bin",
		"LC_ALL=C",
	}
}

func (e *Engine) runGit(dir string, args ...string) (GitResult, error) {
	env := gitEnvironment()
	if e.Config.GitRunner != nil {
		return e.Config.GitRunner(dir, append([]string(nil), args...), env)
	}
	return NativeGitRunner(dir, args, env)
}

func (e *Engine) gitOutput(dir string, args ...string) ([]byte, error) {
	result, err := e.runGit(dir, withSafeGitOptions(args...)...)
	if err != nil || result.ExitCode != 0 {
		return nil, errors.New("Git command failed")
	}
	return result.Output, nil
}

func withSafeGitOptions(args ...string) []string {
	result := []string{
		"--no-optional-locks",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
	}
	return append(result, args...)
}

func (e *Engine) worktreeList(dir string) ([]worktreeRecord, error) {
	output, err := e.gitOutput(dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(output)
}

func parseWorktrees(output []byte) ([]worktreeRecord, error) {
	parts := bytes.Split(output, []byte{0})
	result := make([]worktreeRecord, 0)
	var current *worktreeRecord
	flush := func() error {
		if current == nil {
			return nil
		}
		if current.Path == "" || !filepath.IsAbs(current.Path) || filepath.Clean(current.Path) != current.Path || !validCommit(current.Head) {
			return errors.New("malformed Git worktree list")
		}
		result = append(result, *current)
		current = nil
		return nil
	}
	for _, raw := range parts {
		if len(raw) == 0 {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		line := string(raw)
		switch {
		case strings.HasPrefix(line, "worktree "):
			if current != nil {
				return nil, errors.New("malformed Git worktree list")
			}
			current = &worktreeRecord{Path: strings.TrimPrefix(line, "worktree ")}
		case current == nil:
			return nil, errors.New("malformed Git worktree list")
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(line, "branch ")
		case line == "detached":
			current.Branch = ""
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			current.Prunable = true
		default:
			return nil, errors.New("malformed Git worktree list")
		}
	}
	if current != nil {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if len(result) == 0 {
		return nil, errors.New("empty Git worktree list")
	}
	return result, nil
}

func (e *Engine) inspectPath(home, path, listDir string) (machineSnapshot, []string, error) {
	var snapshot machineSnapshot
	list, err := e.worktreeList(listDir)
	if err != nil {
		return snapshot, []string{ReasonGitUnavailable}, nil
	}
	return e.inspectWithList(home, path, list)
}

func (e *Engine) inspectRegistration(reg privateRegistration) (machineSnapshot, []string, error) {
	home, err := e.home()
	if err != nil {
		return machineSnapshot{}, []string{ReasonGitUnavailable}, nil
	}
	listDir := reg.MainRoot
	if listDir == "" {
		listDir = reg.TargetPath
	}
	if info, statErr := os.Lstat(listDir); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return machineSnapshot{TargetPath: reg.TargetPath, MainRoot: reg.MainRoot, HeadCommit: reg.HeadCommit}, []string{ReasonUnsafePath}, nil
	}
	return e.inspectPath(home, reg.TargetPath, listDir)
}

func (e *Engine) inspectWithList(home, path string, list []worktreeRecord) (machineSnapshot, []string, error) {
	snapshot := machineSnapshot{TargetPath: path}
	reasons := []string{}
	if isAgentRoot(home, path) {
		addReason(&reasons, ReasonAgentRoot)
		return snapshot, sortReasons(reasons), nil
	}
	if len(list) == 0 {
		addReason(&reasons, ReasonGitUnavailable)
		return snapshot, sortReasons(reasons), nil
	}
	main := list[0]
	snapshot.MainRoot = main.Path
	var target *worktreeRecord
	for i := range list {
		if list[i].Path == path {
			target = &list[i]
			break
		}
	}
	if target == nil {
		addReason(&reasons, ReasonNotLinked)
		return snapshot, sortReasons(reasons), nil
	}
	snapshot.HeadCommit = target.Head
	snapshot.BranchRef = target.Branch
	snapshot.WorktreeDigest = worktreeDigest(main, *target)
	if target.Path == main.Path {
		addReason(&reasons, ReasonMainWorktree)
	}
	currentPath, currentErr := e.currentPath()
	if currentErr != nil {
		addReason(&reasons, ReasonGitUnavailable)
	} else if samePathOrDescendant(target.Path, currentPath) {
		addReason(&reasons, ReasonCurrentWorktree)
	}
	if target.Locked {
		addReason(&reasons, ReasonLocked)
	}
	if target.Prunable {
		addReason(&reasons, ReasonPrunable)
	}
	if err := validateExistingDirectoryPath(main.Path); err != nil {
		addReason(&reasons, ReasonUnsafePath)
	}
	if err := validateExistingDirectoryPath(target.Path); err != nil {
		addReason(&reasons, ReasonUnsafePath)
	}
	if len(reasons) != 0 {
		return snapshot, sortReasons(reasons), nil
	}
	if err := e.checkGitExecutionConfig(main.Path); err != nil {
		addReason(&reasons, ReasonUnsupportedGitConfig)
		return snapshot, sortReasons(reasons), nil
	}
	if err := validateWorktreeTree(target.Path); err != nil {
		if errors.Is(err, errUnsafeTree) {
			addReason(&reasons, ReasonSymlinkOrSpecial)
		} else {
			addReason(&reasons, ReasonUnsafePath)
		}
	}
	statusOutput, statusErr := e.gitOutput(target.Path, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=traditional", "-z")
	if statusErr != nil {
		addReason(&reasons, ReasonGitUnavailable)
	} else {
		for _, reason := range statusReasons(statusOutput) {
			addReason(&reasons, reason)
		}
	}
	index, indexErr := e.indexIdentity(target.Path, main.Path)
	if indexErr != nil {
		addReason(&reasons, ReasonGitUnavailable)
	} else {
		snapshot.IndexIdentity = index
	}
	if submodule, submoduleErr := e.hasSubmodule(target.Path, target.Head); submoduleErr != nil {
		addReason(&reasons, ReasonGitUnavailable)
	} else if submodule {
		addReason(&reasons, ReasonSubmodule)
	}
	ref, oid, preserveErr := e.preservedReference(main.Path, target)
	if preserveErr != nil {
		if errors.Is(preserveErr, errMissingRef) {
			addReason(&reasons, ReasonCommitNotPreserved)
		} else {
			addReason(&reasons, ReasonPreservationChanged)
		}
	} else {
		snapshot.PreserveRef = ref
		snapshot.PreserveOID = oid
	}
	targetIdentity, identityErr := identityAt(target.Path)
	if identityErr != nil {
		addReason(&reasons, ReasonUnsafePath)
	} else {
		snapshot.TargetIdentity = targetIdentity
	}
	fingerprint, fingerprintErr := treeFingerprint(target.Path)
	if fingerprintErr != nil {
		addReason(&reasons, ReasonSymlinkOrSpecial)
	} else {
		snapshot.TreeFingerprint = fingerprint
	}
	return snapshot, sortReasons(reasons), nil
}

func isAgentRoot(home, path string) bool {
	for _, rel := range []string{".codex", ".claude", ".hermes"} {
		if samePathOrDescendant(filepath.Join(home, rel), path) {
			return true
		}
	}
	return false
}

func validateExistingDirectoryPath(path string) error {
	if _, err := absoluteCleanPath(path); err != nil {
		return err
	}
	cur := path
	for {
		info, err := os.Lstat(cur)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !trustedOwnerMode(info, uint32(os.Getuid())) {
			return errUnsafeTree
		}
		if cur == filepath.Dir(cur) {
			return nil
		}
		cur = filepath.Dir(cur)
	}
}

func validateParentsForCreation(path string) error {
	if _, err := absoluteCleanPath(path); err != nil {
		return err
	}
	cur := filepath.Dir(path)
	for {
		info, err := os.Lstat(cur)
		if err != nil {
			return errUnsafeTree
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !trustedOwnerMode(info, uint32(os.Getuid())) {
			return errUnsafeTree
		}
		if cur == filepath.Dir(cur) {
			return nil
		}
		cur = filepath.Dir(cur)
	}
}

func validateWorktreeTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errUnsafeTree
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errUnsafeTree
		}
		if !trustedOwnerMode(info, uint32(os.Getuid())) {
			return errUnsafeTree
		}
		return nil
	})
}

func treeFingerprint(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return errUnsafeTree
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00%o\x00", rel, info.Size(), info.Mode())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func identityAt(path string) (fsIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !trustedOwnerMode(info, uint32(os.Getuid())) {
		return fsIdentity{}, errUnsafeTree
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fsIdentity{}, errors.New("filesystem identity unavailable")
	}
	return fsIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino), Size: info.Size(), ModTimeNS: info.ModTime().UnixNano(), Mode: uint32(info.Mode().Perm()), Owner: st.Uid}, nil
}

func (e *Engine) indexIdentity(target, main string) (fsIdentity, error) {
	output, err := e.gitOutput(target, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return fsIdentity{}, err
	}
	path := strings.TrimSpace(string(output))
	if path == "" || strings.ContainsAny(path, "\x00\r\n") {
		return fsIdentity{}, errors.New("invalid Git index path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(main, path)
	}
	if _, err := absoluteCleanPath(path); err != nil {
		return fsIdentity{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !trustedOwnerMode(info, uint32(os.Getuid())) {
		return fsIdentity{}, errUnsafeTree
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fsIdentity{}, errors.New("Git index identity unavailable")
	}
	return fsIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino), Size: info.Size(), ModTimeNS: info.ModTime().UnixNano(), Mode: uint32(info.Mode().Perm()), Owner: st.Uid}, nil
}

func statusReasons(output []byte) []string {
	var tracked, untracked, ignored bool
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) < 2 {
			continue
		}
		switch string(record[:2]) {
		case "!!":
			ignored = true
		case "??":
			untracked = true
		default:
			tracked = true
		}
	}
	result := make([]string, 0, 3)
	if tracked {
		result = append(result, ReasonTrackedChanges)
	}
	if untracked {
		result = append(result, ReasonUntrackedChanges)
	}
	if ignored {
		result = append(result, ReasonIgnoredChanges)
	}
	return result
}

func worktreeDigest(main, target worktreeRecord) string {
	value := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%t\x00%t", main.Path, target.Path, target.Head, target.Branch, target.Locked, target.Prunable)
	return opaqueHash(value)
}

func (e *Engine) hasSubmodule(target, head string) (bool, error) {
	index, err := e.gitOutput(target, "ls-files", "--stage", "-z")
	if err != nil {
		return false, err
	}
	if hasSubmoduleMode(index) {
		return true, nil
	}
	tree, err := e.gitOutput(target, "ls-tree", "-r", "-z", "--full-tree", head)
	if err != nil {
		return false, err
	}
	return hasSubmoduleMode(tree), nil
}

func hasSubmoduleMode(output []byte) bool {
	for _, record := range bytes.Split(output, []byte{0}) {
		if bytes.HasPrefix(record, []byte("160000 ")) {
			return true
		}
	}
	return false
}

func (e *Engine) preservedReference(main string, target *worktreeRecord) (string, string, error) {
	if !validCommit(target.Head) {
		return "", "", errMissingRef
	}
	if target.Branch != "" {
		if !strings.HasPrefix(target.Branch, "refs/heads/") || strings.ContainsAny(target.Branch, "\x00\r\n") {
			return "", "", errMissingRef
		}
		output, err := e.gitOutput(main, "rev-parse", "--verify", target.Branch+"^{commit}")
		if err != nil {
			return "", "", errMissingRef
		}
		oid := strings.TrimSpace(string(output))
		if oid != target.Head || !validCommit(oid) {
			return "", "", errors.New("preserved reference changed")
		}
		return target.Branch, oid, nil
	}
	output, err := e.gitOutput(main, "for-each-ref", "--format=%(refname)%00%(objectname)", "--contains", target.Head, "refs/heads", "refs/tags")
	if err != nil {
		return "", "", errMissingRef
	}
	type reference struct{ name, oid string }
	refs := make([]reference, 0)
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		parts := bytes.Split(line, []byte{0})
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(string(parts[0]))
		oid := strings.TrimSpace(string(parts[1]))
		if name != "" && validCommit(oid) && strings.HasPrefix(name, "refs/") && !strings.ContainsAny(name, "\r\n") {
			refs = append(refs, reference{name: name, oid: oid})
		}
	}
	if len(refs) == 0 {
		return "", "", errMissingRef
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].name < refs[j].name })
	return refs[0].name, refs[0].oid, nil
}

func (e *Engine) checkGitExecutionConfig(main string) error {
	worktreeConfig, err := e.gitConfigValue(main, "--local", "extensions.worktreeconfig")
	if err != nil {
		return err
	}
	if worktreeConfig != "" && isTrueConfigValue(worktreeConfig) {
		return errors.New("unsupported Git worktree configuration")
	}
	pattern := `^(filter\..+\.(clean|smudge|process)|core\.(fsmonitor|hookspath|attributesfile)|include(if)?\..+|extensions\.partialclone|remote\..+\.(promisor|partialclonefilter))$`
	output, err := e.gitOutputAllowExit(main, "config", "--local", "--no-includes", "--null", "--name-only", "--get-regexp", pattern)
	if err != nil || len(output) != 0 {
		return errors.New("unsupported Git execution configuration")
	}
	return nil
}

func (e *Engine) gitConfigValue(dir, scope, key string) (string, error) {
	result, err := e.runGit(dir, withSafeGitOptions("config", scope, "--no-includes", "--get", key)...)
	if err != nil {
		return "", errors.New("Git configuration unavailable")
	}
	if result.ExitCode == 1 {
		return "", nil
	}
	if result.ExitCode != 0 {
		return "", errors.New("Git configuration unavailable")
	}
	value := strings.TrimSpace(string(result.Output))
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("Git configuration unavailable")
	}
	return value, nil
}

func isTrueConfigValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "on", "1":
		return true
	default:
		return false
	}
}

func (e *Engine) gitOutputAllowExit(dir string, args ...string) ([]byte, error) {
	result, err := e.runGit(dir, withSafeGitOptions(args...)...)
	if err != nil || result.ExitCode != 0 && result.ExitCode != 1 {
		return nil, errors.New("Git command failed")
	}
	return result.Output, nil
}

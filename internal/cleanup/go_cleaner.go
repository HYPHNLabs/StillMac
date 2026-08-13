package cleanup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// GoToolBinding is retained only in the private target registry. Public scan and
// plan output bind it through the opaque registry hash and never expose its path.
type GoToolBinding struct {
	Path        string `json:"path"`
	Device      uint64 `json:"device"`
	Inode       uint64 `json:"inode"`
	Fingerprint string `json:"fingerprint"`
	Version     string `json:"version"`
	GoCache     string `json:"go_cache"`
}

type GoCleaner interface {
	Bind(home, target string) (GoToolBinding, error)
	Clean(binding GoToolBinding, home, target string) error
}

type GoCommandResult struct {
	Output   []byte
	ExitCode int
}

type GoCommandRunner func(path string, args, env []string) (GoCommandResult, error)

type nativeGoCleaner struct {
	runner     GoCommandRunner
	candidates []string
}

func defaultGoCleaner() GoCleaner {
	return &nativeGoCleaner{
		runner: runGoCommand,
		candidates: []string{
			"/opt/homebrew/bin/go",
			"/usr/local/go/bin/go",
			"/usr/local/bin/go",
			"/usr/bin/go",
		},
	}
}

func runGoCommand(path string, args, env []string) (GoCommandResult, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = append([]string(nil), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return GoCommandResult{Output: out}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return GoCommandResult{Output: out, ExitCode: exitErr.ExitCode()}, nil
	}
	return GoCommandResult{}, err
}

func (c *nativeGoCleaner) Bind(home, target string) (GoToolBinding, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return GoToolBinding{}, err
	}
	expected := filepath.Join(home, "Library", "Caches", "go-build")
	if target != expected {
		return GoToolBinding{}, errors.New("unexpected Go cache target")
	}
	if err := validateOwnedCacheRoot(home, target); err != nil {
		return GoToolBinding{}, err
	}
	path, err := c.resolveExecutable()
	if err != nil {
		return GoToolBinding{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return GoToolBinding{}, errors.New("unsafe Go executable")
	}
	device, inode, err := identityInfo(info)
	if err != nil {
		return GoToolBinding{}, err
	}
	fingerprint, err := fileFingerprint(path)
	if err != nil {
		return GoToolBinding{}, err
	}
	env := sanitizedGoEnvironment(home, target)
	version, err := c.runSingleLine(path, []string{"version"}, env)
	if err != nil || !strings.HasPrefix(version, "go version go") {
		return GoToolBinding{}, errors.New("Go version unavailable")
	}
	cache, err := c.runSingleLine(path, []string{"env", "GOCACHE"}, env)
	if err != nil || cache != target {
		return GoToolBinding{}, errors.New("Go cache binding unavailable")
	}
	finalInfo, err := os.Lstat(path)
	if err != nil || !finalInfo.Mode().IsRegular() || finalInfo.Mode().Perm()&0o111 == 0 {
		return GoToolBinding{}, errors.New("Go executable changed during verification")
	}
	finalDevice, finalInode, err := identityInfo(finalInfo)
	if err != nil {
		return GoToolBinding{}, err
	}
	finalFingerprint, err := fileFingerprint(path)
	if err != nil || finalDevice != device || finalInode != inode || finalFingerprint != fingerprint {
		return GoToolBinding{}, errors.New("Go executable changed during verification")
	}
	return GoToolBinding{Path: path, Device: device, Inode: inode, Fingerprint: fingerprint, Version: version, GoCache: cache}, nil
}

func (c *nativeGoCleaner) Clean(binding GoToolBinding, home, target string) error {
	current, err := c.Bind(home, target)
	if err != nil || current != binding {
		return errors.New("Go executable binding changed")
	}
	result, err := c.commandRunner()(binding.Path, []string{"clean", "-cache"}, sanitizedGoEnvironment(home, target))
	if err != nil || result.ExitCode != 0 {
		return errors.New("Go owner action failed")
	}
	return nil
}

func (c *nativeGoCleaner) resolveExecutable() (string, error) {
	for _, candidate := range c.candidates {
		if !filepath.IsAbs(candidate) || !secureCandidatePath(candidate) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil || !filepath.IsAbs(resolved) || !secureExecutablePath(resolved) {
			continue
		}
		return resolved, nil
	}
	return "", errors.New("trusted Go executable unavailable")
}

func secureCandidatePath(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	uid := uint32(os.Getuid())
	if !ok || !trustedOwnedMode(st.Uid, uid, info.Mode().Perm()) {
		return false
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	return err == nil && secureExecutablePath(parent)
}

func (c *nativeGoCleaner) runSingleLine(path string, args, env []string) (string, error) {
	result, err := c.commandRunner()(path, args, env)
	if err != nil || result.ExitCode != 0 {
		return "", errors.New("Go command failed")
	}
	line := strings.TrimSpace(string(result.Output))
	if line == "" || strings.ContainsAny(line, "\r\n\x00") {
		return "", errors.New("unexpected Go command output")
	}
	return line, nil
}

func (c *nativeGoCleaner) commandRunner() GoCommandRunner {
	if c.runner != nil {
		return c.runner
	}
	return runGoCommand
}

func sanitizedGoEnvironment(home, target string) []string {
	return []string{
		"HOME=" + home,
		"GOCACHE=" + target,
		"GOENV=off",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"PATH=/usr/bin:/bin",
	}
}

func secureExecutablePath(path string) bool {
	uid := uint32(os.Getuid())
	cur := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !trustedOwnedMode(st.Uid, uid, info.Mode().Perm()) {
			return false
		}
	}
	return true
}

func trustedOwnedMode(owner, current uint32, mode os.FileMode) bool {
	if owner == 0 {
		return mode&0o022 == 0
	}
	return owner == current && mode&0o002 == 0
}

func validateOwnedCacheRoot(home, target string) error {
	uid := uint32(os.Getuid())
	if err := ensureDescendantParents(home, target); err != nil {
		return err
	}
	for _, path := range []string{home, filepath.Join(home, "Library"), filepath.Join(home, "Library", "Caches"), target} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
			return errors.New("unsafe Go cache ownership")
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uid {
			return errors.New("unsafe Go cache ownership")
		}
	}
	return nil
}

func fileFingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

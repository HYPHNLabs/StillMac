package cleanup

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Decision string

const (
	Safe            Decision = "SAFE"
	Review          Decision = "REVIEW"
	Protected       Decision = "PROTECTED"
	BlockedActive   Decision = "BLOCKED_ACTIVE"
	BlockedDirty    Decision = "BLOCKED_DIRTY"
	BlockedUnmerged Decision = "BLOCKED_UNMERGED"
	BlockedUnknown  Decision = "BLOCKED_UNKNOWN"
	BlockedChanged  Decision = "BLOCKED_CHANGED"
	schemaVersion            = "stillmac.cleanup.v1"
	ruleSetVersion           = "cleanup-rules.v1"
)

type Candidate struct {
	ID           string   `json:"id"`
	Family       string   `json:"family"`
	RuleVersion  string   `json:"rule_version"`
	Bytes        int64    `json:"bytes"`
	Decision     Decision `json:"decision"`
	Reasons      []string `json:"reasons"`
	Action       string   `json:"action"`
	Reversible   bool     `json:"reversible"`
	CapturedAt   string   `json:"captured_at"`
	Label        string   `json:"label"`
	Fingerprint  string   `json:"fingerprint"`
	CurrentState string   `json:"current_state"`
	RootKind     string   `json:"root_kind"`
}

type Plan struct {
	SchemaVersion string      `json:"schema_version"`
	ID            string      `json:"plan_id"`
	Hash          string      `json:"plan_hash"`
	ExpiresAt     string      `json:"expires_at"`
	HostBinding   string      `json:"host_binding"`
	RuleSet       string      `json:"rule_set"`
	RegistryHash  string      `json:"target_registry_hash"`
	Candidates    []Candidate `json:"candidates"`
	Excluded      []Candidate `json:"excluded"`
}

type Receipt struct {
	SchemaVersion  string   `json:"schema_version"`
	CandidateID    string   `json:"candidate_id"`
	RuleVersion    string   `json:"rule_version"`
	Decision       Decision `json:"decision"`
	PlanHash       string   `json:"plan_hash"`
	BeforeBytes    int64    `json:"before_bytes"`
	AfterBytes     int64    `json:"after_bytes"`
	MovedBytes     int64    `json:"moved_bytes"`
	RemovedBytes   int64    `json:"removed_bytes"`
	ReclaimedBytes int64    `json:"reclaimed_bytes"`
	Method         string   `json:"method"`
	Result         string   `json:"result"`
	Timestamp      string   `json:"timestamp"`
}

type ApplyResult struct {
	SchemaVersion string    `json:"schema_version"`
	PlanID        string    `json:"plan_id"`
	PlanHash      string    `json:"plan_hash"`
	Rows          []Receipt `json:"rows"`
}

type GitResult struct {
	Output   []byte
	ExitCode int
}

type GitRunner func(args ...string) (GitResult, error)

type ScanConfig struct {
	Home          string
	Scope         string
	DataDir       string
	Now           time.Time
	Protected     map[string]string
	CodexInactive *bool
	GitRunner     GitRunner
	GoCleaner     GoCleaner
}

type Config struct {
	Home          string
	DataDir       string
	HostID        string
	Now           func() time.Time
	CodexInactive *bool
	GitRunner     GitRunner
	GoCleaner     GoCleaner
}

type Engine struct{ Config Config }

type privateTarget struct {
	CandidateID string         `json:"candidate_id"`
	Family      string         `json:"family"`
	RuleVersion string         `json:"rule_version"`
	RootKind    string         `json:"root_kind"`
	Path        string         `json:"path"`
	Fingerprint string         `json:"fingerprint"`
	Decision    Decision       `json:"decision"`
	Device      uint64         `json:"device"`
	Inode       uint64         `json:"inode"`
	GoTool      *GoToolBinding `json:"go_tool,omitempty"`
}

type targetRegistry struct {
	SchemaVersion string          `json:"schema_version"`
	PlanID        string          `json:"plan_id"`
	HostID        string          `json:"host_id"`
	Hash          string          `json:"registry_hash"`
	Targets       []privateTarget `json:"targets"`
}

type protectionRecord struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Family        string `json:"family"`
}

type hostRecord struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
}

func (e *Engine) now() time.Time {
	if e.Config.Now != nil {
		return e.Config.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) hostID() (string, error) {
	if e.Config.HostID != "" {
		return e.Config.HostID, nil
	}
	if err := e.ensureState(); err != nil {
		return "", err
	}
	path := filepath.Join(e.cleanupDir(), "host.json")
	var record hostRecord
	if err := readStrictJSON(path, &record); err == nil {
		if record.SchemaVersion != schemaVersion || len(record.ID) != 32 || !isHex(record.ID) {
			return "", errors.New("invalid host identity")
		}
		return record.ID, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	record = hostRecord{SchemaVersion: schemaVersion, ID: hex.EncodeToString(random)}
	if err := atomicJSON(path, record); err != nil {
		return "", err
	}
	return record.ID, nil
}

func (e *Engine) home() (string, error) {
	if e.Config.Home != "" {
		return filepath.Abs(e.Config.Home)
	}
	return os.UserHomeDir()
}

func (e *Engine) Scan(scope string) ([]Candidate, error) {
	home, err := e.home()
	if err != nil {
		return nil, err
	}
	protected, err := readProtected(e.cleanupDir())
	if err != nil {
		return nil, err
	}
	return ScanWithConfig(ScanConfig{Home: home, Scope: scope, DataDir: e.Config.DataDir, Now: e.now(), Protected: protected, CodexInactive: e.Config.CodexInactive, GitRunner: e.Config.GitRunner, GoCleaner: e.Config.GoCleaner})
}

func Scan(scope string, now time.Time) ([]Candidate, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return ScanWithConfig(ScanConfig{Home: home, Scope: scope, Now: now})
}

type rootRule struct{ family, rule, label, rel, kind string }

var cacheRules = []rootRule{
	{"homebrew-cache", "homebrew-cache.v1", "Homebrew download cache", "Library/Caches/Homebrew", "homebrew-cache"},
	{"go-build-cache", "go-build-cache.v1", "Go build cache", "Library/Caches/go-build", "go-build-cache"},
	{"codex-runtime-cache", "codex-runtime-cache.v1", "Codex runtime cache", ".cache/codex-runtimes", "codex-runtime-cache"},
}

func ScanWithConfig(c ScanConfig) ([]Candidate, error) {
	if c.Home == "" {
		var err error
		c.Home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if c.Now.IsZero() {
		c.Now = time.Now().UTC()
	}
	home, err := filepath.Abs(c.Home)
	if err != nil {
		return nil, err
	}
	protected := c.Protected
	if protected == nil && c.DataDir != "" {
		protected, err = readProtected(filepath.Join(c.DataDir, "cleanup"))
		if err != nil {
			return nil, err
		}
	}
	if protected == nil {
		protected = map[string]string{}
	}
	out := make([]Candidate, 0)
	for _, rule := range cacheRules {
		root := filepath.Join(home, filepath.FromSlash(rule.rel))
		item, err := inspectCache(home, root, rule, c.Now, c.CodexInactive, c.GoCleaner)
		if err != nil {
			return nil, err
		}
		if item == nil {
			continue
		}
		if family, ok := protected[item.ID]; ok {
			if family != item.Family {
				return nil, errors.New("protection family mismatch")
			}
			item.Decision, item.Action, item.Reversible = Protected, "none", false
			item.Reasons = []string{"candidate is explicitly protected"}
		}
		out = append(out, *item)
	}
	if c.Scope != "" && c.Scope != "." {
		gitItems, err := inspectWorktrees(c.Scope, c.Now, c.GitRunner)
		if err != nil {
			return nil, err
		}
		for i := range gitItems {
			if family, ok := protected[gitItems[i].ID]; ok && family == gitItems[i].Family {
				gitItems[i].Decision, gitItems[i].Reasons = Protected, []string{"candidate is explicitly protected"}
			}
		}
		out = append(out, gitItems...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func inspectCache(home, root string, rule rootRule, now time.Time, codexInactive *bool, goCleaner GoCleaner) (*Candidate, error) {
	if err := ensureDescendantParents(home, root); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := &Candidate{ID: stableID(rule.family, rule.rel), Family: rule.family, RuleVersion: rule.rule, CapturedAt: now.UTC().Format(time.RFC3339), Label: rule.label, RootKind: rule.kind, Action: "none"}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "unsafe-root", []string{"exact root is not a real directory"}
		return c, nil
	}
	if rule.family == "codex-runtime-cache" {
		c.Decision, c.CurrentState, c.Reasons = BlockedActive, "activity-unproven", []string{"Codex inactivity was not proven"}
		if codexInactive != nil && *codexInactive {
			c.Decision, c.CurrentState, c.Reasons = Review, "inactive-proven", []string{"inactivity is proven but this release has no Codex action"}
		}
		return c, nil
	}
	c.Bytes, err = treeBytes(root)
	if err != nil {
		if rule.family == "homebrew-cache" {
			c.Decision, c.CurrentState, c.Reasons = Review, "inventory-partial", []string{"this release has no bounded owner-native Homebrew action", "cache size could not be safely measured"}
			return c, nil
		}
		c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "unsafe-entry", []string{"cache tree contains an unsafe or unreadable entry"}
		return c, nil
	}
	c.Fingerprint, err = treeFingerprint(root)
	if err != nil {
		if rule.family == "homebrew-cache" {
			c.Decision, c.CurrentState, c.Reasons = Review, "inventory-partial", []string{"this release has no bounded owner-native Homebrew action", "cache fingerprint could not be safely measured"}
			return c, nil
		}
		c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "unsafe-entry", []string{"cache tree contains an unsafe or unreadable entry"}
		return c, nil
	}
	if rule.family == "homebrew-cache" {
		c.Decision, c.CurrentState, c.Action, c.Reversible = Review, "owner-action-unavailable", "none", false
		c.Reasons = []string{"exact allowlisted cache root", "this release has no bounded owner-native Homebrew action"}
		return c, nil
	}
	if goCleaner == nil {
		goCleaner = defaultGoCleaner()
	}
	binding, bindErr := goCleaner.Bind(home, root)
	if bindErr != nil || binding.GoCache != root {
		c.Decision, c.CurrentState, c.Action, c.Reversible = BlockedUnknown, "go-binding-unavailable", "none", false
		c.Reasons = []string{"Go executable or exact GOCACHE ownership could not be verified"}
		return c, nil
	}
	c.Decision, c.CurrentState, c.Action, c.Reversible = Safe, "verified-owner-action", "owner-native-go-clean-cache", false
	c.Reasons = []string{"exact allowlisted Go cache root", "owner-native Go executable and GOCACHE verified"}
	return c, nil
}

type worktree struct {
	path, branch     string
	locked, prunable bool
}

func inspectWorktrees(scope string, now time.Time, runner GitRunner) ([]Candidate, error) {
	absScope, err := filepath.Abs(scope)
	if err != nil {
		return nil, err
	}
	if runner == nil {
		runner = func(args ...string) (GitResult, error) {
			cmd := exec.Command("git", args...)
			out, err := cmd.Output()
			if err == nil {
				return GitResult{Output: out, ExitCode: 0}, nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return GitResult{Output: exitErr.Stderr, ExitCode: exitErr.ExitCode()}, nil
			}
			return GitResult{}, err
		}
	}
	result, err := runner("-C", absScope, "worktree", "list", "--porcelain")
	if err != nil {
		return []Candidate{{ID: stableID("git-worktree", absScope), Family: "git-worktree", RuleVersion: "git-worktree.v1", Decision: BlockedUnknown, Reasons: []string{"Git worktree inventory unavailable"}, Action: "none", CapturedAt: now.UTC().Format(time.RFC3339), Label: "Git worktree inventory", CurrentState: "unknown", RootKind: "git-worktree"}}, nil
	}
	if result.ExitCode != 0 {
		return []Candidate{{ID: stableID("git-worktree", absScope), Family: "git-worktree", RuleVersion: "git-worktree.v1", Decision: BlockedUnknown, Reasons: []string{"Git worktree inventory unavailable"}, Action: "none", CapturedAt: now.UTC().Format(time.RFC3339), Label: "Git worktree inventory", CurrentState: "unknown", RootKind: "git-worktree"}}, nil
	}
	parsed := parseWorktrees(string(result.Output))
	items := make([]Candidate, 0, len(parsed))
	for i, wt := range parsed {
		c := Candidate{ID: stableID("git-worktree", wt.path), Family: "git-worktree", RuleVersion: "git-worktree.v1", Decision: Review, Reasons: []string{"clean merged inactive worktree; inventory only"}, Action: "none", Reversible: false, CapturedAt: now.UTC().Format(time.RFC3339), Label: fmt.Sprintf("Git linked worktree %d", i+1), CurrentState: "clean-merged", RootKind: "git-worktree"}
		cleanPath, _ := filepath.Abs(wt.path)
		switch {
		case cleanPath == absScope || wt.branch == "refs/heads/main" || wt.branch == "refs/heads/master":
			c.Decision, c.CurrentState, c.Reasons = BlockedActive, "current", []string{"current or main worktree"}
		case wt.locked:
			c.Decision, c.CurrentState, c.Reasons = BlockedActive, "locked", []string{"worktree is locked"}
		case wt.prunable:
			c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "prunable", []string{"worktree is marked prunable"}
		default:
			statusResult, statusErr := runner("-C", cleanPath, "status", "--porcelain")
			if statusErr != nil {
				c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "unknown", []string{"worktree status unavailable"}
			} else if statusResult.ExitCode != 0 {
				c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "unknown", []string{"worktree status unavailable"}
			} else if len(strings.TrimSpace(string(statusResult.Output))) != 0 {
				c.Decision, c.CurrentState, c.Reasons = BlockedDirty, "dirty", []string{"worktree has changes"}
			} else if mergeResult, mergeErr := runner("-C", cleanPath, "merge-base", "--is-ancestor", "HEAD", "main"); mergeErr != nil || mergeResult.ExitCode != 0 {
				if mergeErr == nil && mergeResult.ExitCode == 1 {
					c.Decision, c.CurrentState, c.Reasons = BlockedUnmerged, "unmerged", []string{"HEAD is not proven merged into main"}
				} else {
					c.Decision, c.CurrentState, c.Reasons = BlockedUnknown, "unknown", []string{"Git merge status unavailable"}
				}
			}
		}
		items = append(items, c)
	}
	return items, nil
}

func parseWorktrees(s string) []worktree {
	var out []worktree
	var cur *worktree
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &worktree{path: strings.TrimPrefix(line, "worktree ")}
		case cur != nil && strings.HasPrefix(line, "branch "):
			cur.branch = strings.TrimPrefix(line, "branch ")
		case cur != nil && strings.HasPrefix(line, "locked"):
			cur.locked = true
		case cur != nil && strings.HasPrefix(line, "prunable"):
			cur.prunable = true
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func (e *Engine) Plan(items []Candidate, ids []string) (Plan, error) {
	if err := e.validateState(); err != nil {
		return Plan{}, err
	}
	if len(ids) == 0 {
		return Plan{}, errors.New("no candidates selected")
	}
	wanted, allSafe := map[string]bool{}, false
	for _, id := range ids {
		if id == "all-safe" {
			allSafe = true
		} else {
			if !validCandidateID(id) {
				return Plan{}, errors.New("invalid candidate ID")
			}
			wanted[id] = true
		}
	}
	if allSafe && len(wanted) != 0 {
		return Plan{}, errors.New("cannot combine all-safe with candidate IDs")
	}
	seen := map[string]bool{}
	var selected, excluded []Candidate
	for _, c := range items {
		if allSafe {
			if c.Decision == Safe {
				selected = append(selected, c)
			} else {
				excluded = append(excluded, c)
			}
		} else if wanted[c.ID] {
			seen[c.ID] = true
			if c.Decision != Safe {
				return Plan{}, errors.New("selected candidate is not SAFE")
			}
			selected = append(selected, c)
		}
	}
	for id := range wanted {
		if !seen[id] {
			return Plan{}, errors.New("unknown candidate ID")
		}
	}
	if len(selected) == 0 {
		return Plan{}, errors.New("no SAFE candidates selected")
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].ID < excluded[j].ID })
	home, err := e.home()
	if err != nil {
		return Plan{}, err
	}
	hostID, err := e.hostID()
	if err != nil {
		return Plan{}, err
	}
	reg := targetRegistry{SchemaVersion: schemaVersion, HostID: hostID}
	for _, c := range selected {
		path, ok := supportedPath(home, c)
		if !ok {
			return Plan{}, errors.New("candidate has no supported action")
		}
		dev, inode, err := identityOf(path)
		if err != nil {
			return Plan{}, err
		}
		cleaner := e.Config.GoCleaner
		if cleaner == nil {
			cleaner = defaultGoCleaner()
		}
		binding, err := cleaner.Bind(home, path)
		if err != nil || binding.GoCache != path {
			return Plan{}, errors.New("Go executable binding unavailable")
		}
		reg.Targets = append(reg.Targets, privateTarget{CandidateID: c.ID, Family: c.Family, RuleVersion: c.RuleVersion, RootKind: c.RootKind, Path: path, Fingerprint: c.Fingerprint, Decision: c.Decision, Device: dev, Inode: inode, GoTool: &binding})
	}
	reg.Hash = registryHash(reg)
	p := Plan{SchemaVersion: schemaVersion, ExpiresAt: e.now().Add(15 * time.Minute).Format(time.RFC3339), HostBinding: opaqueHash(hostID), RuleSet: ruleSetVersion, RegistryHash: reg.Hash, Candidates: selected, Excluded: excluded}
	p.Hash = planHash(p)
	p.ID = "plan-" + p.Hash[:16]
	reg.PlanID = p.ID
	reg.Hash = registryHash(reg)
	p.RegistryHash = reg.Hash
	p.Hash = planHash(p)
	p.ID = "plan-" + p.Hash[:16]
	reg.PlanID = p.ID
	reg.Hash = registryHash(reg)
	p.RegistryHash = reg.Hash
	p.Hash = planHash(p)
	p.ID = "plan-" + p.Hash[:16]
	// Registry PlanID is not part of its integrity hash to avoid a circular identifier dependency.
	reg.PlanID = p.ID
	if err := e.ensureState(); err != nil {
		return Plan{}, err
	}
	if err := atomicJSON(filepath.Join(e.cleanupDir(), "targets", p.ID+".json"), reg); err != nil {
		return Plan{}, err
	}
	if err := atomicJSON(filepath.Join(e.cleanupDir(), "plans", p.ID+".json"), p); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func registryHash(r targetRegistry) string {
	q := r
	q.Hash = ""
	q.PlanID = ""
	b, _ := json.Marshal(q)
	return opaqueHash(string(b))
}
func planHash(p Plan) string {
	q := p
	q.ID = ""
	q.Hash = ""
	b, _ := json.Marshal(q)
	return opaqueHash(string(b))
}
func opaqueHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (e *Engine) Apply(id string) (ApplyResult, error) {
	result := ApplyResult{SchemaVersion: schemaVersion, PlanID: id}
	if !validPlanID(id) {
		return result, errors.New("invalid plan ID")
	}
	if err := e.validateState(); err != nil {
		return result, err
	}
	p, reg, err := e.loadPlanRegistry(id)
	if err != nil {
		return result, err
	}
	result.PlanHash = p.Hash
	expiresAt, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil {
		return result, errors.New("invalid plan expiry")
	}
	if !e.now().Before(expiresAt) {
		return result, errors.New("plan expired")
	}
	hostID, hostErr := e.hostID()
	if hostErr != nil {
		return result, hostErr
	}
	if p.HostBinding != opaqueHash(hostID) || reg.HostID != hostID {
		return result, errors.New("plan host mismatch")
	}
	protected, err := readProtected(e.cleanupDir())
	if err != nil {
		return result, err
	}
	home, err := e.home()
	if err != nil {
		return result, err
	}
	var applyErr error
	for i, c := range p.Candidates {
		t := reg.Targets[i]
		r := Receipt{SchemaVersion: schemaVersion, CandidateID: c.ID, RuleVersion: c.RuleVersion, Decision: BlockedChanged, PlanHash: p.Hash, Method: "none", Result: "blocked_changed", Timestamp: e.now().Format(time.RFC3339)}
		if family, ok := protected[c.ID]; ok && family == c.Family {
			r.Decision = Protected
		}
		current, binding, validationErr := e.revalidate(home, c, t, protected)
		if validationErr != nil {
			applyErr = errors.New("one or more candidates changed")
			if current != nil {
				r.BeforeBytes = current.Bytes
			}
		} else {
			r.BeforeBytes = current.Bytes
			cleaner := e.Config.GoCleaner
			if cleaner == nil {
				cleaner = defaultGoCleaner()
			}
			if err := cleaner.Clean(binding, home, t.Path); err != nil {
				r.Result = "owner_action_failed"
				r.Method = "owner-native-go-clean-cache"
				r.Decision = Safe
				applyErr = errors.New("one or more candidates failed")
				if after, measureErr := inspectCache(home, t.Path, mustRule(c.Family), e.now(), e.Config.CodexInactive, cleaner); measureErr == nil && after != nil {
					r.AfterBytes = after.Bytes
				}
			} else {
				afterCandidate, measureErr := inspectCache(home, t.Path, mustRule(c.Family), e.now(), e.Config.CodexInactive, cleaner)
				if measureErr != nil || (afterCandidate != nil && afterCandidate.Decision != Safe) {
					r.Result = "blocked_changed"
					r.Method = "none"
					applyErr = errors.New("one or more candidates changed")
				} else {
					after := int64(0)
					if afterCandidate != nil {
						after = afterCandidate.Bytes
					}
					r.AfterBytes = after
					r.RemovedBytes = max(current.Bytes-after, 0)
					r.ReclaimedBytes = r.RemovedBytes
					r.Method = "owner-native-go-clean-cache"
					r.Result = "cleaned"
					r.Decision = Safe
				}
			}
		}
		result.Rows = append(result.Rows, r)
		if err := atomicJSON(filepath.Join(e.cleanupDir(), "receipts", fmt.Sprintf("%s-%03d-%d.json", id, i, e.now().UnixNano())), r); err != nil {
			return result, errors.New("receipt write failed")
		}
	}
	return result, applyErr
}

func (e *Engine) revalidate(home string, c Candidate, t privateTarget, protected map[string]string) (*Candidate, GoToolBinding, error) {
	if family, ok := protected[c.ID]; ok && family == c.Family {
		return nil, GoToolBinding{}, errors.New("candidate is protected")
	}
	expected, ok := supportedPath(home, c)
	if !ok || expected != t.Path || t.CandidateID != c.ID || t.Family != c.Family || t.RuleVersion != c.RuleVersion || t.RootKind != c.RootKind || t.Decision != Safe || t.GoTool == nil {
		return nil, GoToolBinding{}, errors.New("target registry changed")
	}
	if err := ensureDescendantParents(home, t.Path); err != nil {
		return nil, GoToolBinding{}, err
	}
	dev, inode, err := identityOf(t.Path)
	if err != nil || dev != t.Device || inode != t.Inode {
		return nil, GoToolBinding{}, errors.New("root identity changed")
	}
	rule, ok := ruleForFamily(c.Family)
	if !ok || rule.rule != c.RuleVersion {
		return nil, GoToolBinding{}, errors.New("rule changed")
	}
	cleaner := e.Config.GoCleaner
	if cleaner == nil {
		cleaner = defaultGoCleaner()
	}
	current, err := inspectCache(home, t.Path, rule, e.now(), e.Config.CodexInactive, cleaner)
	if err != nil || current == nil {
		return current, GoToolBinding{}, errors.New("candidate unavailable")
	}
	if current.Decision != Safe || current.Fingerprint != c.Fingerprint || current.Fingerprint != t.Fingerprint {
		return current, GoToolBinding{}, errors.New("candidate changed")
	}
	binding, err := cleaner.Bind(home, t.Path)
	if err != nil || binding != *t.GoTool {
		return current, GoToolBinding{}, errors.New("Go executable binding changed")
	}
	return current, binding, nil
}

func (e *Engine) Protect(scope, id string) error {
	if !validCandidateID(id) {
		return errors.New("invalid candidate ID")
	}
	items, err := e.Scan(scope)
	if err != nil {
		return err
	}
	var found *Candidate
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		return errors.New("unknown candidate ID")
	}
	if _, ok := ruleForFamily(found.Family); !ok || found.Family == "git-worktree" {
		return errors.New("candidate family is inventory-only")
	}
	if err := e.ensureState(); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(e.cleanupDir(), "protected", id+".json"), protectionRecord{SchemaVersion: schemaVersion, ID: id, Family: found.Family})
}

func (e *Engine) History() ([]Receipt, error) {
	dir := filepath.Join(e.cleanupDir(), "receipts")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Receipt{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Receipt
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			return nil, errors.New("unsafe receipt entry")
		}
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			return nil, errors.New("unsafe receipt permissions")
		}
		var r Receipt
		if err := readStrictJSON(filepath.Join(dir, entry.Name()), &r); err != nil || !validReceipt(r) {
			return nil, errors.New("malformed receipt")
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out, nil
}

func validReceipt(r Receipt) bool {
	if r.SchemaVersion != schemaVersion || !validCandidateID(r.CandidateID) || r.PlanHash == "" || r.RuleVersion == "" || r.Timestamp == "" || !validDecision(r.Decision) {
		return false
	}
	if r.BeforeBytes < 0 || r.AfterBytes < 0 || r.MovedBytes < 0 || r.RemovedBytes < 0 || r.ReclaimedBytes < 0 {
		return false
	}
	switch r.Result {
	case "cleaned":
		return r.Method == "owner-native-go-clean-cache" && r.Decision == Safe && r.MovedBytes == 0 && r.RemovedBytes == r.ReclaimedBytes && r.RemovedBytes == max(r.BeforeBytes-r.AfterBytes, 0)
	case "owner_action_failed":
		return r.Method == "owner-native-go-clean-cache" && r.Decision == Safe && r.MovedBytes == 0 && r.RemovedBytes == 0 && r.ReclaimedBytes == 0
	case "blocked_changed", "blocked_protected":
		return r.Method == "none" && (r.Decision == BlockedChanged || r.Decision == Protected) && r.MovedBytes == 0 && r.RemovedBytes == 0 && r.ReclaimedBytes == 0
	}
	return false
}

func (e *Engine) loadPlanRegistry(id string) (Plan, targetRegistry, error) {
	var p Plan
	var r targetRegistry
	if err := readStrictJSON(filepath.Join(e.cleanupDir(), "plans", id+".json"), &p); err != nil {
		return p, r, err
	}
	if err := readStrictJSON(filepath.Join(e.cleanupDir(), "targets", id+".json"), &r); err != nil {
		return p, r, err
	}
	if _, err := time.Parse(time.RFC3339, p.ExpiresAt); err != nil {
		return p, r, errors.New("invalid plan expiry")
	}
	if p.SchemaVersion != schemaVersion || p.ID != id || p.Hash != planHash(p) || id != "plan-"+p.Hash[:16] || p.RuleSet != ruleSetVersion {
		return p, r, errors.New("invalid plan integrity")
	}
	if r.SchemaVersion != schemaVersion || r.PlanID != id || r.Hash != registryHash(r) || p.RegistryHash != r.Hash || len(r.Targets) != len(p.Candidates) {
		return p, r, errors.New("invalid target registry")
	}
	return p, r, nil
}

func (e *Engine) cleanupDir() string { return filepath.Join(e.Config.DataDir, "cleanup") }
func (e *Engine) ensureState() error {
	if e.Config.DataDir == "" {
		return errors.New("data directory unavailable")
	}
	if err := validateCreationPath(e.Config.DataDir); err != nil {
		return err
	}
	for _, d := range []string{e.Config.DataDir, e.cleanupDir(), filepath.Join(e.cleanupDir(), "plans"), filepath.Join(e.cleanupDir(), "targets"), filepath.Join(e.cleanupDir(), "protected"), filepath.Join(e.cleanupDir(), "receipts")} {
		if err := ensurePrivateDir(d); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) validateState() error {
	if err := e.ensureState(); err != nil {
		return err
	}
	rootEntries, err := os.ReadDir(e.cleanupDir())
	if err != nil {
		return err
	}
	allowedDirs := map[string]bool{"plans": true, "targets": true, "protected": true, "receipts": true}
	for _, entry := range rootEntries {
		if allowedDirs[entry.Name()] {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				return errors.New("unknown or unsafe cleanup entry")
			}
			continue
		}
		if entry.Name() == "host.json" {
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
				return errors.New("unsafe host identity")
			}
			info, infoErr := entry.Info()
			if infoErr != nil || info.Mode().Perm() != 0o600 {
				return errors.New("unsafe host identity")
			}
			continue
		}
		return errors.New("unknown cleanup entry")
	}
	for _, name := range []string{"plans", "targets", "protected", "receipts"} {
		dir := filepath.Join(e.cleanupDir(), name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
				return errors.New("unknown or unsafe state entry")
			}
			info, err := entry.Info()
			if err != nil || info.Mode().Perm() != 0o600 {
				return errors.New("unsafe state file permissions")
			}
		}
	}
	return nil
}

func ensurePrivateDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.Mkdir(path, 0o700); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("unsafe state directory")
	}
	return nil
}

func atomicJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".stillmac-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(name)
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	closed = true
	if err != nil {
		return err
	}
	if old, statErr := os.Lstat(path); statErr == nil && (old.Mode()&os.ModeSymlink != 0 || !old.Mode().IsRegular()) {
		return errors.New("unsafe state destination")
	}
	return os.Rename(name, path)
}

func readStrictJSON(path string, v any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("unsafe state file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(v); err != nil {
		return errors.New("malformed state")
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("malformed state")
	}
	return nil
}

func readProtected(cleanupDir string) (map[string]string, error) {
	out := map[string]string{}
	dir := filepath.Join(cleanupDir, "protected")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			return nil, errors.New("unsafe protection entry")
		}
		var r protectionRecord
		if err := readStrictJSON(filepath.Join(dir, entry.Name()), &r); err != nil {
			return nil, err
		}
		if r.SchemaVersion != schemaVersion || !validCandidateID(r.ID) || r.Family == "" || entry.Name() != r.ID+".json" || !familyMatchesID(r.ID, r.Family) {
			return nil, errors.New("malformed protection")
		}
		out[r.ID] = r.Family
	}
	return out, nil
}

func identityInfo(info os.FileInfo) (uint64, uint64, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("unsupported file identity")
	}
	return uint64(st.Dev), uint64(st.Ino), nil
}

func ensureDescendantParents(base, target string) error {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("target outside safe root")
	}
	baseInfo, err := os.Lstat(baseAbs)
	if err != nil || baseInfo.Mode()&os.ModeSymlink != 0 || !baseInfo.IsDir() {
		return errors.New("unsafe safe-root identity")
	}
	cur := baseAbs
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("unsafe target parent")
		}
	}
	return nil
}

func validateCreationPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	cur := abs
	for {
		info, statErr := os.Lstat(cur)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return errors.New("unsafe state parent")
			}
			return nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return errors.New("state parent unavailable")
		}
		cur = parent
	}
}

func treeBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink inside cache root")
		}
		if !d.IsDir() {
			i, err := d.Info()
			if err != nil {
				return err
			}
			total, err = addTreeBytes(total, i.Size())
			if err != nil {
				return err
			}
		}
		return nil
	})
	return total, err
}
func addTreeBytes(total, size int64) (int64, error) {
	if size < 0 || size > math.MaxInt64-total {
		return 0, errors.New("cache size overflow")
	}
	return total + size, nil
}
func treeFingerprint(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink inside cache root")
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00", rel, info.Mode().Type(), info.Mode().Perm(), info.Size())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
func stableID(family, key string) string {
	h := sha256.Sum256([]byte(family + "\x00" + key))
	return "sm-" + hex.EncodeToString(h[:8])
}
func validCandidateID(id string) bool {
	return len(id) == 19 && strings.HasPrefix(id, "sm-") && isHex(id[3:])
}
func validPlanID(id string) bool {
	return len(id) == 21 && strings.HasPrefix(id, "plan-") && isHex(id[5:]) && filepath.Base(id) == id
}
func isHex(s string) bool { _, err := hex.DecodeString(s); return err == nil }
func familyMatchesID(id, family string) bool {
	for _, r := range cacheRules {
		if r.family == family && stableID(r.family, r.rel) == id {
			return true
		}
	}
	return false
}
func validDecision(d Decision) bool {
	switch d {
	case Safe, Review, Protected, BlockedActive, BlockedDirty, BlockedUnmerged, BlockedUnknown, BlockedChanged:
		return true
	}
	return false
}
func supportedPath(home string, c Candidate) (string, bool) {
	for _, r := range cacheRules {
		if r.family == c.Family && r.kind == c.RootKind && r.rule == c.RuleVersion {
			return filepath.Join(home, filepath.FromSlash(r.rel)), r.family == "go-build-cache"
		}
	}
	return "", false
}
func ruleForFamily(f string) (rootRule, bool) {
	for _, r := range cacheRules {
		if r.family == f {
			return r, true
		}
	}
	return rootRule{}, false
}
func identityOf(path string) (uint64, uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("root identity unavailable")
	}
	return uint64(st.Dev), uint64(st.Ino), nil
}

func mustRule(family string) rootRule {
	rule, _ := ruleForFamily(family)
	return rule
}

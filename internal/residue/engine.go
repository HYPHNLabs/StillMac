package residue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"stillmac/internal/cleanup"
)

type defaultRoot struct {
	family string
	rel    string
	rule   string
}

var defaultRoots = []defaultRoot{
	{family: "homebrew-cache", rel: "Library/Caches/Homebrew", rule: "homebrew-cache.v1"},
	{family: "go-build-cache", rel: "Library/Caches/go-build", rule: "go-build-cache.v1"},
	{family: "codex-runtime-cache", rel: ".cache/codex-runtimes", rule: "codex-runtime-cache.v1"},
}

var artifactFamilies = []string{"node_modules", ".next", "dist", "target", ".build"}

var protectedRootNames = map[string]struct{}{
	".aider":    {},
	".agents":   {},
	".amazon-q": {},
	".claude":   {},
	".cline":    {},
	".codex":    {},
	".continue": {},
	".cursor":   {},
	".gemini":   {},
	".goose":    {},
	".hermes":   {},
	".kiro":     {},
	".roo":      {},
	".windsurf": {},
}

type scanBudget struct {
	limits     Limits
	deadline   time.Time
	items      int
	bytes      int64
	visited    map[fileKey]struct{}
	duplicates int
}

type fileKey struct {
	device uint64
	inode  uint64
}

type captureState struct {
	entries       []Entry
	bindings      []ScopeBinding
	warnings      []string
	itemsVisited  int
	itemsSkipped  int
	worktrees     int
	uniqueBytes   int64
	uniquePresent bool
	uniquePartial bool
	uniqueUnknown bool
	globalFiles   map[fileKey]int64
	counted       map[string]struct{}
}

func (e *Engine) Inspect(scopes []Scope) (Report, error) {
	return e.capture("inspect", scopes)
}

func (e *Engine) Snapshot(scopes []Scope) (Report, error) {
	report, err := e.capture("snapshot", scopes)
	if err != nil {
		return Report{}, err
	}
	if err := e.appendSnapshot(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (e *Engine) capture(kind string, scopes []Scope) (Report, error) {
	if kind != "inspect" && kind != "snapshot" {
		return Report{}, ErrInvalidConfig
	}
	normalized, err := normalizeScopes(scopes)
	if err != nil {
		return Report{}, err
	}
	now := e.now()
	state := captureState{globalFiles: make(map[fileKey]int64), counted: make(map[string]struct{})}
	state.bindings = append(state.bindings, ScopeBinding{ID: "default", Kind: "default", Status: StatusComplete})

	home, homeErr := e.home()
	if homeErr != nil {
		state.warnings = append(state.warnings, "home_unavailable")
		state.bindings[0].Status = StatusUnknown
		state.entries = append(state.entries, defaultUnknownEntries("home_unavailable")...)
	} else {
		defaultEntries, defaultWarnings := e.inspectDefaultRoots(home)
		state.entries = append(state.entries, defaultEntries...)
		state.warnings = append(state.warnings, defaultWarnings...)
		for _, entry := range defaultEntries {
			e.mergeEntrySize(&state, entry)
		}
	}

	budget := newScanBudget(e.limits)
	for _, scope := range normalized {
		entries, binding, warnings, items, skipped, worktrees := e.inspectScope(scope, budget)
		state.entries = append(state.entries, entries...)
		state.bindings = append(state.bindings, binding)
		state.warnings = append(state.warnings, warnings...)
		state.itemsVisited += items
		state.itemsSkipped += skipped
		state.worktrees += worktrees
		for _, entry := range entries {
			e.mergeEntrySize(&state, entry)
		}
	}

	sort.Slice(state.entries, func(i, j int) bool { return state.entries[i].ID < state.entries[j].ID })
	sort.Slice(state.bindings, func(i, j int) bool { return state.bindings[i].ID < state.bindings[j].ID })
	state.warnings = uniqueStrings(state.warnings)
	status := captureStatus(state.entries, state.bindings)
	report := Report{
		SchemaVersion: SchemaVersion,
		ReportKind:    kind,
		Status:        status,
		CapturedAt:    now.Format(time.RFC3339Nano),
		RuleSet:       RuleSet,
		ScopeBindings: state.bindings,
		Evidence: Evidence{
			TotalSize:         state.totalSize(status),
			LimitsApplied:     limitsApplied,
			ItemsVisited:      state.itemsVisited,
			ItemsSkipped:      state.itemsSkipped,
			HardlinksSkipped:  budget.duplicates,
			WorktreesObserved: state.worktrees,
			ScopesObserved:    len(normalized),
			Warnings:          state.warnings,
		},
		Actions:  availableActions(),
		NextStep: nextStep(status),
		Entries:  state.entries,
	}
	report.ReportID = reportID(report)
	if kind == "snapshot" {
		report.SnapshotID = report.ReportID
	}
	return report, nil
}

func (e *Engine) inspectDefaultRoots(home string) ([]Entry, []string) {
	entries := make([]Entry, 0, len(defaultRoots))
	warnings := make([]string, 0)
	cleanupEngine := cleanup.Engine{Config: cleanup.Config{
		Home:      home,
		DataDir:   e.Config.DataDir,
		Now:       e.Config.Now,
		GitRunner: e.Config.GitRunner,
		GoCleaner: e.Config.GoCleaner,
	}}
	candidates, scanErr := cleanupEngine.Scan("")
	byFamily := make(map[string]cleanup.Candidate, len(candidates))
	if scanErr != nil {
		warnings = append(warnings, "cleanup_scan_unavailable")
	} else {
		for _, candidate := range candidates {
			byFamily[candidate.Family] = candidate
		}
	}
	for _, root := range defaultRoots {
		path := filepath.Join(home, filepath.FromSlash(root.rel))
		info, err := os.Lstat(path)
		entry := Entry{
			ID:                 stableID("default-cache", root.rel),
			ScopeID:            "default",
			Kind:               "default-cache",
			Family:             root.family,
			RuleVersion:        root.rule,
			ActionAvailability: ActionInventoryOnly,
			Evidence:           []string{"exact_allowlisted_root"},
		}
		if errors.Is(err, os.ErrNotExist) {
			entry.Size = Size{Status: SizeNotPresent}
			entry.State = "not-present"
			entry.Evidence = append(entry.Evidence, "root_not_present")
			entries = append(entries, entry)
			continue
		}
		if err != nil {
			entry.Size = Size{Status: SizeUnknown}
			entry.State = "unknown"
			entry.Evidence = append(entry.Evidence, "root_unavailable")
			entries = append(entries, entry)
			continue
		}
		entry.Identity = identityHash(info)
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			entry.Size = Size{Status: SizeUnknown}
			entry.State = "unsafe-root"
			entry.Evidence = append(entry.Evidence, "root_not_real_directory")
			entries = append(entries, entry)
			continue
		}
		candidate, ok := byFamily[root.family]
		if !ok {
			entry.Size = Size{Status: SizeUnknown}
			entry.State = "unknown"
			entry.Evidence = append(entry.Evidence, "cleanup_decision_unavailable")
			entries = append(entries, entry)
			continue
		}
		entry.CleanupCandidateID = candidate.ID
		entry.CleanupDecision = string(candidate.Decision)
		entry.CleanupAction = candidate.Action
		entry.Fingerprint = candidate.Fingerprint
		if candidate.Fingerprint == "" {
			entry.Size = Size{Status: SizeUnknown}
			entry.State = candidate.CurrentState
			entry.Evidence = append(entry.Evidence, "cleanup_size_unavailable")
		} else if candidate.CurrentState == "inventory-partial" {
			entry.Size = Size{Status: SizePartial}
			entry.State = "partial"
			entry.Evidence = append(entry.Evidence, "cleanup_measurement_partial")
		} else if candidate.CurrentState == "unsafe-entry" {
			entry.Size = Size{Status: SizeUnknown}
			entry.State = "unknown"
			entry.Evidence = append(entry.Evidence, "unsafe_entry")
		} else {
			bytes := candidate.Bytes
			entry.Size = Size{Status: SizeComplete, Bytes: &bytes}
			entry.State = "measured"
			entry.Evidence = append(entry.Evidence, "cleanup_measurement_complete")
		}
		entries = append(entries, entry)
	}
	return entries, warnings
}

func defaultUnknownEntries(reason string) []Entry {
	entries := make([]Entry, 0, len(defaultRoots))
	for _, root := range defaultRoots {
		entries = append(entries, Entry{
			ID:                 stableID("default-cache", root.rel),
			ScopeID:            "default",
			Kind:               "default-cache",
			Family:             root.family,
			RuleVersion:        root.rule,
			Size:               Size{Status: SizeUnknown},
			State:              "unknown",
			ActionAvailability: ActionInventoryOnly,
			Evidence:           []string{reason},
		})
	}
	return entries
}

func (e *Engine) inspectScope(scope Scope, budget *scanBudget) ([]Entry, ScopeBinding, []string, int, int, int) {
	scopeID := stableID("scope", scope.Kind+"\x00"+scope.Path)
	binding := ScopeBinding{ID: scopeID, Kind: scope.Kind, Status: StatusComplete}
	warnings := make([]string, 0)
	info, err := os.Lstat(scope.Path)
	if errors.Is(err, os.ErrNotExist) {
		binding.Status = StatusUnknown
		return []Entry{scopeUnknownEntry(scopeID, "scope_not_present")}, binding, nil, 0, 0, 0
	}
	if err != nil {
		binding.Status = StatusUnknown
		return []Entry{scopeUnknownEntry(scopeID, "scope_unavailable")}, binding, nil, 0, 0, 0
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if canonical, evalErr := filepath.EvalSymlinks(scope.Path); evalErr == nil && isProtectedPath(canonical) {
			binding.Status = StatusUnknown
			return []Entry{protectedAliasEntry(scopeID)}, binding, nil, 0, 0, 0
		}
		binding.Status = StatusUnknown
		return []Entry{scopeUnknownEntry(scopeID, "scope_symlink")}, binding, nil, 0, 0, 0
	}
	if !info.IsDir() {
		binding.Status = StatusUnknown
		return []Entry{scopeUnknownEntry(scopeID, "scope_not_directory")}, binding, nil, 0, 0, 0
	}
	binding.RootIdentity = identityHash(info)
	if protectedPathOrAlias(scope.Path) {
		binding.Status = StatusUnknown
		return []Entry{Entry{
			ID:                 stableID("protected-scope", scope.Kind+"\x00"+scope.Path),
			ScopeID:            scopeID,
			Kind:               "protected-root",
			Family:             "agent-state",
			RuleVersion:        "protected-roots.v1",
			Size:               Size{Status: SizeUnknown},
			State:              "protected",
			Identity:           binding.RootIdentity,
			ActionAvailability: ActionOwnerManaged,
			Evidence:           []string{"protected_agent_root", "no_traversal"},
		}}, binding, nil, 0, 0, 0
	}

	entries := make([]Entry, 0, len(artifactFamilies))
	items, skipped := 0, 0
	for _, family := range artifactFamilies {
		path := filepath.Join(scope.Path, family)
		measurement := measureTree(path, budget)
		entry := Entry{
			ID:                 stableID("artifact", scopeID+"\x00"+family),
			ScopeID:            scopeID,
			Kind:               "project-artifact",
			Family:             family,
			RuleVersion:        "artifacts.v1",
			Size:               measurement.Size,
			State:              measurement.State,
			Identity:           measurement.Identity,
			Fingerprint:        measurement.Fingerprint,
			ActionAvailability: ActionInventoryOnly,
			Evidence:           measurement.Evidence,
		}
		entries = append(entries, entry)
		items += measurement.ItemsVisited
		skipped += measurement.ItemsSkipped
		entry.files = measurement.Files
		entries[len(entries)-1] = entry
	}
	worktreeEntries, worktreeWarnings, worktreeCount, worktreeItems, worktreeSkipped := e.inspectWorktrees(scope, scopeID, budget)
	entries = append(entries, worktreeEntries...)
	warnings = append(warnings, worktreeWarnings...)
	for _, warning := range worktreeWarnings {
		if warning == "git_metadata_unavailable" || warning == "git_metadata_bound_reached" {
			binding.Status = StatusPartial
		}
	}
	items += worktreeItems
	skipped += worktreeSkipped
	return entries, binding, warnings, items, skipped, worktreeCount
}

func scopeUnknownEntry(scopeID, reason string) Entry {
	return Entry{
		ID:                 stableID("scope", scopeID),
		ScopeID:            scopeID,
		Kind:               "scope",
		Family:             "project-root",
		RuleVersion:        "scope.v1",
		Size:               Size{Status: SizeUnknown},
		State:              "unknown",
		ActionAvailability: ActionInventoryOnly,
		Evidence:           []string{reason, "no_traversal"},
	}
}

func protectedAliasEntry(scopeID string) Entry {
	return Entry{
		ID:                 stableID("protected-scope", scopeID),
		ScopeID:            scopeID,
		Kind:               "protected-root",
		Family:             "agent-state",
		RuleVersion:        "protected-roots.v1",
		Size:               Size{Status: SizeUnknown},
		State:              "protected",
		ActionAvailability: ActionOwnerManaged,
		Evidence:           []string{"protected_agent_root", "symlink_alias", "no_traversal"},
	}
}

func (e *Engine) inspectWorktrees(scope Scope, scopeID string, budget *scanBudget) ([]Entry, []string, int, int, int) {
	runner := e.Config.GitRunner
	if runner == nil {
		return nil, []string{"git_metadata_unavailable"}, 0, 0, 0
	}
	result, err := runner("-C", scope.Path, "worktree", "list", "--porcelain")
	if err != nil || result.ExitCode != 0 {
		return nil, []string{"git_metadata_unavailable"}, 0, 0, 0
	}
	output := result.Output
	if len(output) > 1<<20 {
		return nil, []string{"git_metadata_bound_reached"}, 0, 0, 1
	}
	worktrees := parseWorktrees(string(output))
	entries := make([]Entry, 0, len(worktrees))
	warnings := make([]string, 0)
	items, skipped := 0, 0
	for _, worktree := range worktrees {
		if len(entries) >= budget.limits.MaxItems {
			warnings = append(warnings, "worktree_item_bound_reached")
			break
		}
		if !filepath.IsAbs(worktree.Path) {
			skipped++
			warnings = append(warnings, "relative_worktree_path_skipped")
			continue
		}
		path, pathErr := filepath.Abs(worktree.Path)
		if pathErr != nil {
			skipped++
			continue
		}
		protected := protectedPathOrAlias(path)
		worktreeID := stableID("git-worktree", scopeID+"\x00"+path)
		entry := Entry{
			ID:                 worktreeID,
			ScopeID:            worktreeID,
			Kind:               "git-worktree",
			Family:             "git-linked-worktree",
			RuleVersion:        "git-worktree.v1",
			Size:               Size{Status: SizeUnknown},
			State:              "inventory-only",
			ActionAvailability: ActionInventoryOnly,
			Evidence:           []string{"metadata_only", "no_traversal"},
		}
		info, infoErr := os.Lstat(path)
		if infoErr == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
			entry.Identity = identityHash(info)
		} else if errors.Is(infoErr, os.ErrNotExist) {
			entry.Size = Size{Status: SizeNotPresent}
			entry.Evidence = append(entry.Evidence, "worktree_not_present")
		} else if infoErr != nil && !errors.Is(infoErr, os.ErrNotExist) {
			entry.Evidence = append(entry.Evidence, "worktree_identity_unavailable")
		}
		if protected {
			entry.State = "protected"
			entry.ActionAvailability = ActionOwnerManaged
			entry.Evidence = append(entry.Evidence, "protected_agent_root")
			entries = append(entries, entry)
			items++
			continue
		}
		entries = append(entries, entry)
		items++
		if hasSymlinkAncestor(path) {
			entry.State = "unknown"
			entry.Size = Size{Status: SizeUnknown}
			entry.Evidence = append(entry.Evidence, "symlink_alias", "no_traversal")
			entries[len(entries)-1] = entry
			continue
		}
		if infoErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		worktreeScope := Scope{Path: path, Kind: "worktree"}
		artifactEntries := make([]Entry, 0, len(artifactFamilies))
		for _, family := range artifactFamilies {
			measurement := measureTree(filepath.Join(path, family), budget)
			artifactEntries = append(artifactEntries, Entry{
				ID:                 stableID("artifact", worktreeID+"\x00"+family),
				ScopeID:            worktreeID,
				Kind:               worktreeScope.Kind + "-artifact",
				Family:             family,
				RuleVersion:        "artifacts.v1",
				Size:               measurement.Size,
				State:              measurement.State,
				Identity:           measurement.Identity,
				Fingerprint:        measurement.Fingerprint,
				ActionAvailability: ActionInventoryOnly,
				Evidence:           measurement.Evidence,
				files:              measurement.Files,
			})
			items += measurement.ItemsVisited
			skipped += measurement.ItemsSkipped
		}
		entries = append(entries, artifactEntries...)
	}
	return entries, warnings, len(worktrees), items, skipped
}

type linkedWorktree struct {
	Path string
}

func parseWorktrees(output string) []linkedWorktree {
	var result []linkedWorktree
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			if path != "" {
				result = append(result, linkedWorktree{Path: path})
			}
		}
	}
	return result
}

func normalizeScopes(scopes []Scope) ([]Scope, error) {
	if len(scopes) > maxScopes {
		return nil, ErrInvalidScope
	}
	result := make([]Scope, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.Path == "" {
			return nil, ErrInvalidScope
		}
		kind := scope.Kind
		if kind == "" {
			kind = "project"
		}
		switch kind {
		case "project", "repository", "worktree":
		default:
			return nil, ErrInvalidScope
		}
		path, err := filepath.Abs(scope.Path)
		if err != nil {
			return nil, ErrInvalidScope
		}
		key := kind + "\x00" + path
		if _, exists := seen[key]; exists {
			return nil, ErrInvalidScope
		}
		seen[key] = struct{}{}
		scope.Kind = kind
		scope.Path = filepath.Clean(path)
		result = append(result, scope)
	}
	return result, nil
}

func (e *Engine) home() (string, error) {
	home := e.Config.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("home unavailable")
	}
	return filepath.Clean(abs), nil
}

func (e *Engine) now() time.Time {
	if e.Config.Now != nil {
		return e.Config.Now().UTC()
	}
	return time.Now().UTC()
}

func newScanBudget(limits Limits) *scanBudget {
	return &scanBudget{
		limits:   limits,
		deadline: time.Now().Add(limits.MaxDuration),
		visited:  make(map[fileKey]struct{}),
	}
}

func (e *Engine) mergeEntrySize(state *captureState, entry Entry) {
	if entry.Size.Bytes != nil {
		if entry.Size.Status == SizeComplete || entry.Size.Status == SizePartial {
			state.uniquePresent = true
			if len(entry.files) == 0 {
				if _, counted := state.counted[entry.ID]; !counted {
					state.counted[entry.ID] = struct{}{}
					state.uniqueBytes += *entry.Size.Bytes
				}
			} else {
				for key, size := range entry.files {
					if _, exists := state.globalFiles[key]; exists {
						continue
					}
					state.globalFiles[key] = size
					if next, ok := safeAdd(state.uniqueBytes, size); ok {
						state.uniqueBytes = next
					} else {
						state.uniqueUnknown = true
					}
				}
			}
		}
	}
	if entry.Size.Status == SizePartial {
		state.uniquePartial = true
	}
	if entry.Size.Status == SizeUnknown && entry.Kind != "git-worktree" {
		state.uniqueUnknown = true
	}
}

func (state captureState) totalSize(status Status) Size {
	if status == StatusNotPresent {
		return Size{Status: SizeNotPresent}
	}
	bytes := state.uniqueBytes
	if state.uniqueUnknown && !state.uniquePresent {
		return Size{Status: SizeUnknown}
	}
	if state.uniqueUnknown || state.uniquePartial {
		return Size{Status: SizePartial, Bytes: &bytes}
	}
	return Size{Status: SizeComplete, Bytes: &bytes}
}

func captureStatus(entries []Entry, bindings []ScopeBinding) Status {
	complete, partial, unknown, present := false, false, false, false
	for _, binding := range bindings {
		if binding.Status == StatusUnknown {
			unknown = true
		} else if binding.Status == StatusPartial {
			partial = true
		}
	}
	for _, entry := range entries {
		switch entry.Size.Status {
		case SizeComplete:
			complete = true
			present = true
		case SizePartial:
			partial = true
			present = true
		case SizeUnknown:
			unknown = true
		case SizeNotPresent:
			// Absence is an observed state, not an unknown zero.
		}
	}
	if unknown && !complete && !partial {
		return StatusUnknown
	}
	if unknown || partial {
		return StatusPartial
	}
	if !present {
		return StatusNotPresent
	}
	return StatusComplete
}

func availableActions() []Action {
	return []Action{
		{Name: "residue-removal", Status: ActionUnavailable, Reason: "this release is inventory-only and has no residue deletion action"},
		{Name: "existing-cache-cleanup", Status: "candidate-dependent", Reason: "existing approval-gated cleanup rules remain the only supported cache action"},
	}
}

func nextStep(status Status) string {
	switch status {
	case StatusComplete:
		return "Review the bounded inventory; no residue removal is available from these commands."
	case StatusPartial:
		return "Review the evidence warnings and rerun with explicit roots or adjusted bounds before comparing snapshots."
	case StatusUnknown:
		return "Resolve the unavailable or protected evidence and rerun; this observation is not safe for size comparison."
	case StatusNotPresent:
		return "No selected residue was present; provide an explicit project or worktree root when needed."
	default:
		return "Review the report before taking any action."
	}
}

func stableID(kind, value string) string {
	hash := sha256.Sum256([]byte(kind + "\x00" + value))
	return "res-" + hex.EncodeToString(hash[:8])
}

func reportID(report Report) string {
	copyReport := report
	copyReport.ReportID = ""
	copyReport.SnapshotID = ""
	copyReport.Quiet = false
	copyReport.Suppressed = false
	encoded, _ := json.Marshal(copyReport)
	hash := sha256.Sum256(encoded)
	return "snapshot-" + hex.EncodeToString(hash[:8])
}

func identityHash(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return opaqueHash(fmt.Sprintf("%d:%d:%d:%d", uint64(stat.Dev), uint64(stat.Ino), info.Mode().Type(), info.Mode().Perm()))
}

func opaqueHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func isProtectedPath(path string) bool {
	clean := filepath.Clean(path)
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if _, ok := protectedRootNames[part]; ok {
			return true
		}
	}
	return false
}

func protectedPathOrAlias(path string) bool {
	if isProtectedPath(path) {
		return true
	}
	canonical, err := filepath.EvalSymlinks(path)
	return err == nil && isProtectedPath(canonical)
}

func hasSymlinkAncestor(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	clean := filepath.Clean(abs)
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return false
			}
			return true
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if allowedSystemAlias(current) {
				continue
			}
			return true
		}
	}
	return false
}

func allowedSystemAlias(path string) bool {
	return path == string(filepath.Separator)+"var" || path == string(filepath.Separator)+"tmp"
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func safeAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

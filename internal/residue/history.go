package residue

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	maxSnapshotBytes = int64(4 * 1024 * 1024)
	maxHistoryBytes  = int64(32 * 1024 * 1024)
)

func (e *Engine) appendSnapshot(report Report) error {
	if err := validateSnapshot(report); err != nil {
		return err
	}
	directory, err := e.ensureSnapshotDirectory()
	if err != nil {
		return err
	}
	existing, err := e.readSnapshots()
	if err != nil {
		return err
	}
	for _, previous := range existing {
		if previous.SnapshotID == report.SnapshotID {
			return ErrHistory
		}
	}
	contents, err := json.MarshalIndent(report, "", "  ")
	if err != nil || int64(len(contents)) > maxSnapshotBytes {
		return ErrHistory
	}
	contents = append(contents, '\n')
	temporary, err := os.CreateTemp(directory, ".stillmac-residue-")
	if err != nil {
		return ErrHistory
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return ErrHistory
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return ErrHistory
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ErrHistory
	}
	if err := temporary.Close(); err != nil {
		return ErrHistory
	}
	finalPath := filepath.Join(directory, report.SnapshotID+".json")
	if info, statErr := os.Lstat(finalPath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrHistory
		}
		return ErrHistory
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return ErrHistory
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return ErrHistory
	}
	removeTemporary = false
	if err := syncDirectory(directory); err != nil {
		return ErrHistory
	}
	if err := e.pruneSnapshots(); err != nil {
		return err
	}
	return nil
}

func (e *Engine) readSnapshots() ([]Report, error) {
	return e.readSnapshotsBounded(maxHistoryLimit, maxHistoryBytes)
}

func (e *Engine) readSnapshotsBounded(maxEntries int, maxBytes int64) ([]Report, error) {
	if e.Config.DataDir == "" {
		return nil, nil
	}
	dataDir, err := filepath.Abs(e.Config.DataDir)
	if err != nil {
		return nil, ErrHistory
	}
	if hasSymlinkAncestor(dataDir) {
		return nil, ErrHistory
	}
	info, err := os.Lstat(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !safePrivateDirectory(info) {
		return nil, ErrHistory
	}
	residueDir := filepath.Join(dataDir, "residue")
	info, err = os.Lstat(residueDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !safePrivateDirectory(info) {
		return nil, ErrHistory
	}
	residueEntries, err := os.ReadDir(residueDir)
	if err != nil {
		return nil, ErrHistory
	}
	for _, entry := range residueEntries {
		if entry.Name() != "snapshots" || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return nil, ErrHistory
		}
	}
	directory := filepath.Join(residueDir, "snapshots")
	info, err = os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !safePrivateDirectory(info) {
		return nil, ErrHistory
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, ErrHistory
	}
	if len(entries) > maxEntries {
		return nil, ErrHistory
	}
	result := make([]Report, 0, len(entries))
	var totalBytes int64
	for _, entry := range entries {
		if !validSnapshotFilename(entry.Name()) {
			return nil, ErrHistory
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil, ErrHistory
		}
		path := filepath.Join(directory, entry.Name())
		fileInfo, err := os.Lstat(path)
		if err != nil || fileInfo.Mode().Perm() != 0o600 || fileInfo.Size() < 1 || fileInfo.Size() > maxSnapshotBytes {
			return nil, ErrHistory
		}
		if next, ok := safeAdd(totalBytes, fileInfo.Size()); !ok || next > maxBytes {
			return nil, ErrHistory
		} else {
			totalBytes = next
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, ErrHistory
		}
		decoder := json.NewDecoder(io.LimitReader(file, maxSnapshotBytes+1))
		decoder.DisallowUnknownFields()
		var report Report
		decodeErr := decoder.Decode(&report)
		var trailing any
		if decodeErr == nil {
			trailingErr := decoder.Decode(&trailing)
			if trailingErr == io.EOF {
				decodeErr = nil
			} else {
				decodeErr = ErrHistory
			}
		}
		closeErr := file.Close()
		if decodeErr != nil || closeErr != nil || validateSnapshot(report) != nil || report.SnapshotID+".json" != entry.Name() {
			return nil, ErrHistory
		}
		result = append(result, report)
	}
	sort.Slice(result, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, result[i].CapturedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, result[j].CapturedAt)
		if leftErr != nil || rightErr != nil {
			return result[i].SnapshotID < result[j].SnapshotID
		}
		if left.Equal(right) {
			return result[i].SnapshotID < result[j].SnapshotID
		}
		return left.Before(right)
	})
	return result, nil
}

func (e *Engine) ensureSnapshotDirectory() (string, error) {
	if e.Config.DataDir == "" {
		return "", ErrDataDir
	}
	dataDir, err := filepath.Abs(e.Config.DataDir)
	if err != nil || dataDir == string(filepath.Separator) {
		return "", ErrDataDir
	}
	if err := ensurePrivateDirectory(dataDir); err != nil {
		return "", err
	}
	residueDir := filepath.Join(dataDir, "residue")
	if err := ensurePrivateDirectory(residueDir); err != nil {
		return "", err
	}
	directory := filepath.Join(residueDir, "snapshots")
	if err := ensurePrivateDirectory(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func ensurePrivateDirectory(path string) error {
	if hasSymlinkAncestor(path) {
		return ErrDataDir
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		parent := filepath.Dir(path)
		if parent == path {
			return ErrDataDir
		}
		if err := ensurePrivateParent(parent); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return ErrDataDir
		}
		return nil
	}
	if err != nil || !safePrivateDirectory(info) {
		return ErrDataDir
	}
	return nil
}

func ensurePrivateParent(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrDataDir
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ErrDataDir
	}
	parent := filepath.Dir(path)
	if parent == path {
		return ErrDataDir
	}
	if err := ensurePrivateParent(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return ErrDataDir
	}
	return nil
}

func (e *Engine) pruneSnapshots() error {
	snapshots, err := e.readSnapshotsBounded(maxHistoryLimit+1, maxHistoryBytes+maxSnapshotBytes)
	if err != nil {
		return err
	}
	if len(snapshots) <= e.Config.HistoryLimit {
		return nil
	}
	directory := filepath.Join(e.Config.DataDir, "residue", "snapshots")
	for _, report := range snapshots[:len(snapshots)-e.Config.HistoryLimit] {
		path := filepath.Join(directory, report.SnapshotID+".json")
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrHistory
		}
		if err := os.Remove(path); err != nil {
			return ErrHistory
		}
	}
	if err := syncDirectory(directory); err != nil {
		return ErrHistory
	}
	return nil
}

func safePrivateDirectory(info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint32(stat.Uid) == uint32(os.Getuid())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func validSnapshotFilename(name string) bool {
	return len(name) == len("snapshot-")+16+len(".json") && strings.HasPrefix(name, "snapshot-") && strings.HasSuffix(name, ".json") && isHex(name[len("snapshot-"):len(name)-len(".json")])
}

func isHex(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func validateSnapshot(report Report) error {
	if report.SchemaVersion != SchemaVersion || report.ReportKind != "snapshot" || report.RuleSet != RuleSet || report.ReportID == "" || report.ReportID != report.SnapshotID || !validSnapshotFilename(report.SnapshotID+".json") {
		return ErrHistory
	}
	at, err := time.Parse(time.RFC3339Nano, report.CapturedAt)
	if err != nil || at.Location() != time.UTC || at.Format(time.RFC3339Nano) != report.CapturedAt {
		return ErrHistory
	}
	switch report.Status {
	case StatusComplete, StatusPartial, StatusUnknown, StatusNotPresent:
	default:
		return ErrHistory
	}
	if report.ReportID != reportID(report) {
		return ErrHistory
	}
	if report.Evidence.LimitsApplied != limitsApplied || report.NextStep != nextStep(report.Status) {
		return ErrHistory
	}
	expectedActions := availableActions()
	if len(report.Actions) != len(expectedActions) {
		return ErrHistory
	}
	for index := range expectedActions {
		if report.Actions[index] != expectedActions[index] {
			return ErrHistory
		}
	}
	if report.Evidence.ItemsVisited < 0 || report.Evidence.ItemsSkipped < 0 || report.Evidence.HardlinksSkipped < 0 || report.Evidence.WorktreesObserved < 0 || report.Evidence.ScopesObserved < 0 {
		return ErrHistory
	}
	for _, warning := range report.Evidence.Warnings {
		if !safeToken(warning) {
			return ErrHistory
		}
	}
	if err := validateSize(report.Evidence.TotalSize); err != nil {
		return err
	}
	seenScopes := make(map[string]struct{}, len(report.ScopeBindings))
	for _, binding := range report.ScopeBindings {
		if !safeToken(binding.ID) || !safeToken(binding.Kind) || binding.RootIdentity != "" && (len(binding.RootIdentity) != 64 || !isHex(binding.RootIdentity)) {
			return ErrHistory
		}
		if _, exists := seenScopes[binding.ID]; exists {
			return ErrHistory
		}
		seenScopes[binding.ID] = struct{}{}
		switch binding.Status {
		case StatusComplete, StatusPartial, StatusUnknown, StatusNotPresent:
		default:
			return ErrHistory
		}
	}
	seenEntries := make(map[string]struct{}, len(report.Entries))
	for _, entry := range report.Entries {
		if !validResidueID(entry.ID) || !safeToken(entry.ScopeID) || !safeToken(entry.Kind) || !safeToken(entry.Family) || !safeToken(entry.RuleVersion) {
			return ErrHistory
		}
		if _, exists := seenEntries[entry.ID]; exists {
			return ErrHistory
		}
		seenEntries[entry.ID] = struct{}{}
		if err := validateSize(entry.Size); err != nil {
			return err
		}
		if entry.Identity != "" && (len(entry.Identity) != 64 || !isHex(entry.Identity)) || entry.Fingerprint != "" && (len(entry.Fingerprint) != 64 || !isHex(entry.Fingerprint)) {
			return ErrHistory
		}
		if !safeToken(entry.State) || !safeToken(entry.ActionAvailability) || !safeTokenList(entry.Evidence) || !safeOptionalToken(entry.CleanupDecision) || !safeOptionalToken(entry.CleanupAction) {
			return ErrHistory
		}
		if entry.CleanupCandidateID != "" && !validCleanupID(entry.CleanupCandidateID) {
			return ErrHistory
		}
	}
	if len(report.Changes) != 0 || report.Quiet || report.Suppressed || report.FromSnapshot != "" || report.ToSnapshot != "" {
		return ErrHistory
	}
	return nil
}

func validateSize(size Size) error {
	switch size.Status {
	case SizeComplete:
		if size.Bytes == nil || *size.Bytes < 0 {
			return ErrHistory
		}
	case SizePartial:
		if size.Bytes != nil && *size.Bytes < 0 {
			return ErrHistory
		}
	case SizeUnknown, SizeNotPresent:
		if size.Bytes != nil {
			return ErrHistory
		}
	default:
		return ErrHistory
	}
	return nil
}

func validResidueID(value string) bool {
	return len(value) == len("res-")+16 && strings.HasPrefix(value, "res-") && isHex(value[len("res-"):])
}

func safeToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func safeTokenList(values []string) bool {
	for _, value := range values {
		if !safeToken(value) {
			return false
		}
	}
	return true
}

func safeOptionalToken(value string) bool {
	return value == "" || safeToken(value)
}

func validCleanupID(value string) bool {
	return len(value) == len("sm-")+16 && strings.HasPrefix(value, "sm-") && isHex(value[len("sm-"):])
}

func (e *Engine) snapshotsForChanges(options ChangeOptions) (Report, Report, bool, error) {
	snapshots, err := e.readSnapshots()
	if err != nil {
		return Report{}, Report{}, false, err
	}
	if options.FromID != "" || options.ToID != "" {
		if options.FromID == "" || options.ToID == "" {
			return Report{}, Report{}, false, ErrHistory
		}
		var from, to *Report
		for index := range snapshots {
			if snapshots[index].SnapshotID == options.FromID {
				from = &snapshots[index]
			}
			if snapshots[index].SnapshotID == options.ToID {
				to = &snapshots[index]
			}
		}
		if from == nil || to == nil {
			return Report{}, Report{}, false, ErrHistory
		}
		return *from, *to, true, nil
	}
	if len(snapshots) < 2 {
		return Report{}, Report{}, false, nil
	}
	return snapshots[len(snapshots)-2], snapshots[len(snapshots)-1], true, nil
}

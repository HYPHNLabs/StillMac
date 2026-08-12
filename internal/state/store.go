package state

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"stillmac/internal/observe"
)

const (
	FileName      = "current-sample.json"
	maxStateBytes = int64(8 * 1024 * 1024)
)

var (
	ErrWrite             = errors.New("state write failed")
	ErrRead              = errors.New("state read failed")
	safeCommRE           = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	canonicalTimestampRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$`)
)

type State struct {
	SchemaVersion string         `json:"schema_version"`
	Sample        observe.Sample `json:"sample"`
}

type requiredState struct {
	SchemaVersion *string         `json:"schema_version"`
	Sample        *requiredSample `json:"sample"`
}

type requiredSample struct {
	SchemaVersion *string            `json:"schema_version"`
	CapturedAt    *string            `json:"captured_at"`
	Processes     *[]requiredProcess `json:"processes"`
	Memory        *requiredMemory    `json:"memory"`
	Quality       *requiredQuality   `json:"quality"`
}

type requiredProcess struct {
	Comm           *string  `json:"comm"`
	PID            *int     `json:"pid"`
	PPID           *int     `json:"ppid"`
	CPUPercent     *float64 `json:"cpu_percent"`
	MemoryPercent  *float64 `json:"memory_percent"`
	ElapsedSeconds *int64   `json:"elapsed_seconds"`
}

type requiredMemory struct {
	Pressure      *string `json:"pressure"`
	SwapUsedBytes *uint64 `json:"swap_used_bytes"`
}

type requiredQuality struct {
	Valid                   *bool     `json:"valid"`
	Status                  *string   `json:"status"`
	ProcessRowsObserved     *int      `json:"process_rows_observed"`
	ProcessRowsAccepted     *int      `json:"process_rows_accepted"`
	ProcessRowsRejected     *int      `json:"process_rows_rejected"`
	MemoryPressureAvailable *bool     `json:"memory_pressure_available"`
	SwapUsedAvailable       *bool     `json:"swap_used_available"`
	Issues                  *[]string `json:"issues"`
}

type Store struct {
	Directory string
	ops       *storeOps
}

type storeOps struct {
	rename        func(oldPath, newPath string) error
	remove        func(path string) error
	syncDirectory func(directory *os.File) error
}

func (ops *storeOps) renamePath(oldPath, newPath string) error {
	if ops != nil && ops.rename != nil {
		return ops.rename(oldPath, newPath)
	}
	return os.Rename(oldPath, newPath)
}

func (ops *storeOps) removePath(path string) error {
	if ops != nil && ops.remove != nil {
		return ops.remove(path)
	}
	return os.Remove(path)
}

func (ops *storeOps) syncDirectoryPath(directory *os.File) error {
	if ops != nil && ops.syncDirectory != nil {
		return ops.syncDirectory(directory)
	}
	return directory.Sync()
}

func retryFilesystem(operation func() error) error {
	if err := operation(); err != nil {
		return operation()
	}
	return nil
}

func (s Store) Write(sample observe.Sample) error {
	if s.Directory == "" || !validSample(sample) {
		return ErrWrite
	}
	directory, err := openDirectory(s.Directory, true)
	if err != nil {
		return ErrWrite
	}
	defer directory.Close()
	if err := verifyDirectory(directory, s.Directory); err != nil {
		return ErrWrite
	}

	statePath := filepath.Join(s.Directory, FileName)
	originalInfo, originalExists, err := regularEntryInfo(statePath)
	if err != nil {
		return ErrWrite
	}

	contents, err := json.MarshalIndent(State{
		SchemaVersion: "stillmac.state.v1",
		Sample:        sample,
	}, "", "  ")
	if err != nil {
		return ErrWrite
	}
	contents = append(contents, '\n')
	if int64(len(contents)) > maxStateBytes {
		return ErrWrite
	}

	temporary, err := os.CreateTemp(s.Directory, ".stillmac-state-")
	if err != nil {
		return ErrWrite
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return ErrWrite
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return ErrWrite
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return ErrWrite
	}
	if err := temporary.Close(); err != nil {
		return ErrWrite
	}
	if err := verifyDirectory(directory, s.Directory); err != nil {
		return ErrWrite
	}
	currentInfo, currentExists, err := regularEntryInfo(statePath)
	if err != nil || originalExists != currentExists || (originalExists && !os.SameFile(originalInfo, currentInfo)) {
		return ErrWrite
	}
	if err := os.Rename(temporaryPath, statePath); err != nil {
		return ErrWrite
	}
	removeTemporary = false

	if err := s.ops.syncDirectoryPath(directory); err != nil {
		return ErrWrite
	}
	return nil
}

func (s Store) Read() (State, error) {
	if s.Directory == "" {
		return State{}, ErrRead
	}
	directory, err := openDirectory(s.Directory, false)
	if err != nil {
		return State{}, ErrRead
	}
	defer directory.Close()
	if err := verifyDirectory(directory, s.Directory); err != nil {
		return State{}, ErrRead
	}
	historyInfo, historyErr := os.Lstat(filepath.Join(s.Directory, historyDirectoryName))
	if historyErr == nil && !historyInfo.IsDir() {
		return State{}, ErrRead
	}
	if !errors.Is(historyErr, os.ErrNotExist) {
		if historyErr != nil {
			return State{}, ErrRead
		}
		entries, err := readHistoryAt(s.Directory)
		if err != nil || !historyWithinBounds(entries) {
			return State{}, ErrRead
		}
	}

	fileDescriptor, err := syscall.Open(
		filepath.Join(s.Directory, FileName),
		syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return State{}, ErrRead
	}
	file := os.NewFile(uintptr(fileDescriptor), FileName)
	if file == nil {
		_ = syscall.Close(fileDescriptor)
		return State{}, ErrRead
	}
	defer file.Close()
	if err := verifyDirectory(directory, s.Directory); err != nil {
		return State{}, ErrRead
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxStateBytes {
		return State{}, ErrRead
	}

	decoder := json.NewDecoder(io.LimitReader(file, maxStateBytes+1))
	decoder.DisallowUnknownFields()
	var encoded requiredState
	if err := decoder.Decode(&encoded); err != nil {
		return State{}, ErrRead
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, ErrRead
	}
	stored, ok := encoded.state()
	if !ok {
		return State{}, ErrRead
	}
	if stored.SchemaVersion != "stillmac.state.v1" || !validSample(stored.Sample) {
		return State{}, ErrRead
	}
	return stored, nil
}

func (encoded requiredState) state() (State, bool) {
	if encoded.SchemaVersion == nil || encoded.Sample == nil {
		return State{}, false
	}
	sample, ok := encoded.Sample.sample()
	if !ok {
		return State{}, false
	}
	return State{SchemaVersion: *encoded.SchemaVersion, Sample: sample}, true
}

func (encoded requiredSample) sample() (observe.Sample, bool) {
	if encoded.SchemaVersion == nil || encoded.CapturedAt == nil || encoded.Processes == nil ||
		encoded.Memory == nil || encoded.Quality == nil {
		return observe.Sample{}, false
	}
	processes := make([]observe.Process, len(*encoded.Processes))
	for index, process := range *encoded.Processes {
		decoded, ok := process.process()
		if !ok {
			return observe.Sample{}, false
		}
		processes[index] = decoded
	}
	memory, ok := encoded.Memory.memory()
	if !ok {
		return observe.Sample{}, false
	}
	quality, ok := encoded.Quality.quality()
	if !ok {
		return observe.Sample{}, false
	}
	return observe.Sample{
		SchemaVersion: *encoded.SchemaVersion,
		CapturedAt:    *encoded.CapturedAt,
		Processes:     processes,
		Memory:        memory,
		Quality:       quality,
	}, true
}

func (encoded requiredProcess) process() (observe.Process, bool) {
	if encoded.Comm == nil || encoded.PID == nil || encoded.PPID == nil || encoded.CPUPercent == nil ||
		encoded.MemoryPercent == nil || encoded.ElapsedSeconds == nil {
		return observe.Process{}, false
	}
	return observe.Process{
		Comm:           *encoded.Comm,
		PID:            *encoded.PID,
		PPID:           *encoded.PPID,
		CPUPercent:     *encoded.CPUPercent,
		MemoryPercent:  *encoded.MemoryPercent,
		ElapsedSeconds: *encoded.ElapsedSeconds,
	}, true
}

func (encoded requiredMemory) memory() (observe.Memory, bool) {
	if encoded.Pressure == nil || encoded.SwapUsedBytes == nil {
		return observe.Memory{}, false
	}
	return observe.Memory{Pressure: *encoded.Pressure, SwapUsedBytes: *encoded.SwapUsedBytes}, true
}

func (encoded requiredQuality) quality() (observe.Quality, bool) {
	if encoded.Valid == nil || encoded.Status == nil || encoded.ProcessRowsObserved == nil ||
		encoded.ProcessRowsAccepted == nil || encoded.ProcessRowsRejected == nil ||
		encoded.MemoryPressureAvailable == nil || encoded.SwapUsedAvailable == nil || encoded.Issues == nil {
		return observe.Quality{}, false
	}
	issues := make([]string, len(*encoded.Issues))
	copy(issues, *encoded.Issues)
	return observe.Quality{
		Valid:                   *encoded.Valid,
		Status:                  *encoded.Status,
		ProcessRowsObserved:     *encoded.ProcessRowsObserved,
		ProcessRowsAccepted:     *encoded.ProcessRowsAccepted,
		ProcessRowsRejected:     *encoded.ProcessRowsRejected,
		MemoryPressureAvailable: *encoded.MemoryPressureAvailable,
		SwapUsedAvailable:       *encoded.SwapUsedAvailable,
		Issues:                  issues,
	}, true
}

func openDirectory(path string, create bool) (*os.File, error) {
	if path == "" {
		return nil, errors.New("empty directory")
	}
	if create {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
	}
	fileDescriptor, err := syscall.Open(
		path,
		syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY|syscall.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fileDescriptor), path)
	if directory == nil {
		_ = syscall.Close(fileDescriptor)
		return nil, errors.New("open directory")
	}
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		_ = directory.Close()
		return nil, errors.New("invalid directory")
	}
	if create {
		if err := directory.Chmod(0o700); err != nil {
			_ = directory.Close()
			return nil, err
		}
	}
	return directory, nil
}

func regularEntryInfo(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("state path is not regular")
	}
	return info, true, nil
}

func verifyDirectory(directory *os.File, path string) error {
	openedInfo, err := directory.Stat()
	if err != nil || !openedInfo.IsDir() {
		return errors.New("opened directory unavailable")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.IsDir() || !os.SameFile(openedInfo, pathInfo) {
		return errors.New("selected directory changed")
	}
	return nil
}

func validSample(sample observe.Sample) bool {
	if sample.SchemaVersion != "stillmac.sample.v1" || len(sample.Processes) == 0 {
		return false
	}
	capturedAt, err := time.Parse(time.RFC3339Nano, sample.CapturedAt)
	if err != nil || !canonicalTimestampRE.MatchString(sample.CapturedAt) || capturedAt.UTC().Format(time.RFC3339Nano) != sample.CapturedAt {
		return false
	}
	if sample.Memory.Pressure != "normal" && sample.Memory.Pressure != "warning" && sample.Memory.Pressure != "critical" {
		return false
	}
	for _, process := range sample.Processes {
		if process.PID <= 0 || process.PPID < 0 || process.ElapsedSeconds < 0 ||
			process.CPUPercent < 0 || math.IsInf(process.CPUPercent, 0) || math.IsNaN(process.CPUPercent) ||
			process.MemoryPercent < 0 || process.MemoryPercent > 100 || math.IsInf(process.MemoryPercent, 0) || math.IsNaN(process.MemoryPercent) ||
			!safeCommRE.MatchString(process.Comm) {
			return false
		}
	}

	quality := sample.Quality
	if !quality.Valid || (quality.Status != "complete" && quality.Status != "degraded") ||
		!quality.MemoryPressureAvailable || !quality.SwapUsedAvailable ||
		quality.ProcessRowsAccepted != len(sample.Processes) ||
		quality.ProcessRowsObserved != quality.ProcessRowsAccepted+quality.ProcessRowsRejected ||
		quality.ProcessRowsRejected < 0 {
		return false
	}
	if quality.Status == "complete" {
		return quality.ProcessRowsRejected == 0 && len(quality.Issues) == 0
	}
	return quality.ProcessRowsRejected > 0 && len(quality.Issues) == 1 && quality.Issues[0] == "process_rows_rejected"
}

// ValidSample reports whether a collected sample satisfies the complete stored
// sample contract.
func ValidSample(sample observe.Sample) bool {
	return validSample(sample)
}

const (
	historyDirectoryName = "samples"
	maxHistorySamples    = 672
	maxHistoryAge        = 14 * 24 * time.Hour
	maxHistoryBytes      = int64(128 * 1024 * 1024)
	maxHistorySampleSize = int64(2 * 1024 * 1024)
)

var historyNameRE = regexp.MustCompile(`^sample-[A-Za-z0-9._+-]{1,96}\.json$`)

type historyEntry struct {
	name    string
	sample  observe.Sample
	encoded int64
}

// Append atomically adds one immutable sample to the bounded history.
func (s Store) Append(sample observe.Sample) error {
	if s.Directory == "" || !validSample(sample) {
		return ErrWrite
	}
	contents, err := marshalSample(sample)
	if err != nil || int64(len(contents)) > maxHistorySampleSize {
		return ErrWrite
	}
	root, err := openDirectory(s.Directory, true)
	if err != nil {
		return ErrWrite
	}
	defer root.Close()
	if err := verifyDirectory(root, s.Directory); err != nil {
		return ErrWrite
	}
	historyPath := filepath.Join(s.Directory, historyDirectoryName)
	if _, err := os.Lstat(historyPath); err == nil {
		existingHistory, err := openHistoryDirectory(s.Directory, true)
		if err != nil {
			return ErrWrite
		}
		_ = existingHistory.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrWrite
	}
	legacyPath := filepath.Join(s.Directory, FileName)
	_, legacyExists, legacyErr := regularEntryInfo(legacyPath)
	if legacyErr != nil {
		return ErrWrite
	}
	if legacyExists {
		stored, err := s.Read()
		if err != nil {
			return ErrWrite
		}
		if stored.Sample.CapturedAt == sample.CapturedAt {
			return ErrWrite
		}
	}
	directory, err := openHistoryDirectory(s.Directory, true)
	if err != nil {
		return ErrWrite
	}
	defer directory.Close()
	if err := verifyDirectory(directory, filepath.Join(s.Directory, historyDirectoryName)); err != nil {
		return ErrWrite
	}
	entries, err := readHistoryEntries(directory, filepath.Join(s.Directory, historyDirectoryName))
	if err != nil {
		return ErrWrite
	}
	for _, entry := range entries {
		if entry.sample.CapturedAt == sample.CapturedAt {
			return ErrWrite
		}
	}
	name := historyFileName(sample.CapturedAt)
	for _, entry := range entries {
		if entry.name == name {
			return ErrWrite
		}
	}
	candidate := append(entries, historyEntry{name: name, sample: sample, encoded: int64(len(contents))})
	prune, ok := historyPrune(candidate)
	if !ok {
		return ErrWrite
	}
	for _, entry := range prune {
		if entry.name == name {
			return ErrWrite
		}
	}

	temporary, err := os.CreateTemp(filepath.Join(s.Directory, historyDirectoryName), ".stillmac-sample-")
	if err != nil {
		return ErrWrite
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = retryFilesystem(func() error { return s.ops.removePath(temporaryPath) })
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return ErrWrite
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return ErrWrite
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ErrWrite
	}
	if err := temporary.Close(); err != nil {
		return ErrWrite
	}
	if err := verifyDirectory(directory, filepath.Join(s.Directory, historyDirectoryName)); err != nil {
		return ErrWrite
	}
	finalPath := filepath.Join(s.Directory, historyDirectoryName, name)
	if err := os.Link(temporaryPath, finalPath); err != nil {
		return ErrWrite
	}
	if err := s.ops.removePath(temporaryPath); err != nil {
		finalCleanupErr := retryFilesystem(func() error { return s.ops.removePath(finalPath) })
		temporaryCleanupErr := retryFilesystem(func() error { return s.ops.removePath(temporaryPath) })
		if finalCleanupErr != nil || temporaryCleanupErr != nil {
			return ErrWrite
		}
		return ErrWrite
	}
	keepTemporary = false
	removed := make([]historyBackup, 0, len(prune))
	for _, entry := range prune {
		backup, err := os.CreateTemp(filepath.Join(s.Directory, historyDirectoryName), ".stillmac-backup-")
		if err != nil {
			return s.rollbackHistory(finalPath, removed)
		}
		backupPath := backup.Name()
		if err := backup.Close(); err != nil {
			_ = retryFilesystem(func() error { return s.ops.removePath(backupPath) })
			return s.rollbackHistory(finalPath, removed)
		}
		if err := retryFilesystem(func() error { return s.ops.removePath(backupPath) }); err != nil {
			return s.rollbackHistory(finalPath, removed)
		}
		if err := s.ops.renamePath(filepath.Join(s.Directory, historyDirectoryName, entry.name), backupPath); err != nil {
			return s.rollbackHistory(finalPath, removed)
		}
		removed = append(removed, historyBackup{name: entry.name, path: backupPath, sample: entry.sample})
	}
	for _, backup := range removed {
		if err := s.ops.removePath(backup.path); err != nil {
			return s.rollbackHistory(finalPath, removed)
		}
	}
	if err := s.ops.syncDirectoryPath(directory); err != nil {
		return s.rollbackHistory(finalPath, removed)
	}
	return nil
}

type historyBackup struct {
	name   string
	path   string
	sample observe.Sample
}

func (s Store) rollbackHistory(finalPath string, removed []historyBackup) error {
	cleanupErr := retryFilesystem(func() error { return s.ops.removePath(finalPath) })
	for index := len(removed) - 1; index >= 0; index-- {
		backup := removed[index]
		targetPath := filepath.Join(filepath.Dir(finalPath), backup.name)
		// The backup may already have been deleted by a partially completed
		// cleanup phase. Reconstruct from the validated semantic sample rather
		// than treating a missing backup as unrecoverable.
		if _, err := os.Lstat(targetPath); errors.Is(err, os.ErrNotExist) {
			restored := retryFilesystem(func() error { return s.ops.renamePath(backup.path, targetPath) })
			if restored != nil {
				// A failed restore rename is not evidence that the validated
				// entry cannot be recovered: publish its preserved bytes using
				// the private no-replace reconstruction path.
				restored = s.restoreHistorySample(filepath.Dir(finalPath), targetPath, backup.sample)
			}
			if restored != nil && cleanupErr == nil {
				cleanupErr = restored
			}
		} else if err != nil && cleanupErr == nil {
			cleanupErr = err
		}
		if err := retryFilesystem(func() error { return s.ops.removePath(backup.path) }); err != nil && !errors.Is(err, os.ErrNotExist) && cleanupErr == nil {
			cleanupErr = err
		}
	}
	directory, err := os.Open(filepath.Dir(finalPath))
	if err != nil {
		if cleanupErr == nil {
			cleanupErr = err
		}
	} else {
		if syncErr := s.ops.syncDirectoryPath(directory); syncErr != nil && cleanupErr == nil {
			cleanupErr = syncErr
		}
		_ = directory.Close()
	}
	return ErrWrite
}

func (s Store) restoreHistorySample(directory, targetPath string, sample observe.Sample) error {
	contents, err := marshalSample(sample)
	if err != nil || int64(len(contents)) > maxHistorySampleSize {
		return ErrWrite
	}
	temporary, err := os.CreateTemp(directory, ".stillmac-restore-")
	if err != nil {
		return ErrWrite
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = retryFilesystem(func() error { return s.ops.removePath(temporaryPath) })
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return ErrWrite
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return ErrWrite
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ErrWrite
	}
	if err := temporary.Close(); err != nil {
		return ErrWrite
	}
	if err := os.Link(temporaryPath, targetPath); err != nil {
		return ErrWrite
	}
	if err := retryFilesystem(func() error { return s.ops.removePath(temporaryPath) }); err != nil {
		_ = retryFilesystem(func() error { return s.ops.removePath(targetPath) })
		return ErrWrite
	}
	keepTemporary = false
	return nil
}

// ReadAll returns valid history and, when valid, the legacy current sample in capture order.
func (s Store) ReadAll() ([]observe.Sample, error) {
	if s.Directory == "" {
		return nil, ErrRead
	}
	root, err := openDirectory(s.Directory, false)
	if err != nil {
		return nil, ErrRead
	}
	defer root.Close()
	if err := verifyDirectory(root, s.Directory); err != nil {
		return nil, ErrRead
	}
	entries, historyErr := readHistoryAt(s.Directory)
	if historyErr != nil {
		return nil, ErrRead
	}
	var samples []observe.Sample
	var legacyEntry *historyEntry
	legacyPath := filepath.Join(s.Directory, FileName)
	legacyInfo, legacyExists, legacyStatErr := regularEntryInfo(legacyPath)
	if legacyStatErr != nil {
		return nil, ErrRead
	}
	if legacyExists {
		stored, err := s.Read()
		if err != nil {
			return nil, ErrRead
		}
		samples = append(samples, stored.Sample)
		entry := historyEntry{name: FileName, sample: stored.Sample, encoded: legacyInfo.Size()}
		legacyEntry = &entry
	}
	boundsEntries := append([]historyEntry(nil), entries...)
	if legacyEntry != nil {
		boundsEntries = append(boundsEntries, *legacyEntry)
	}
	if !historyWithinBounds(boundsEntries) {
		return nil, ErrRead
	}
	for _, entry := range entries {
		if len(samples) > 0 && samples[0].CapturedAt == entry.sample.CapturedAt {
			return nil, ErrRead
		}
		samples = append(samples, entry.sample)
	}
	if len(samples) == 0 {
		return nil, ErrRead
	}
	sort.SliceStable(samples, func(i, j int) bool { return mustParse(samples[i].CapturedAt).Before(mustParse(samples[j].CapturedAt)) })
	return samples, nil
}

func marshalSample(sample observe.Sample) ([]byte, error) {
	contents, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

func historyFileName(capturedAt string) string {
	return "sample-" + strings.NewReplacer("-", "", ":", "", ".", "").Replace(capturedAt) + ".json"
}

func openHistoryDirectory(root string, create bool) (*os.File, error) {
	path := filepath.Join(root, historyDirectoryName)
	if create {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	directory, err := openDirectory(path, false)
	if err != nil {
		return nil, err
	}
	if create {
		if err := directory.Chmod(0o700); err != nil {
			_ = directory.Close()
			return nil, err
		}
	}
	return directory, nil
}

func readHistoryAt(root string) ([]historyEntry, error) {
	path := filepath.Join(root, historyDirectoryName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, ErrRead
	}
	directory, err := openHistoryDirectory(root, false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	return readHistoryEntries(directory, path)
}

func readHistoryEntries(directory *os.File, path string) ([]historyEntry, error) {
	if err := verifyDirectory(directory, path); err != nil {
		return nil, err
	}
	if info, err := directory.Stat(); err != nil || info.Mode().Perm()&0o777 != 0o700 {
		return nil, errors.New("exposed history directory")
	}
	names, err := readBoundedEntryNames(directory, maxHistorySamples)
	if err != nil {
		return nil, err
	}
	entries := make([]historyEntry, 0, len(names))
	for _, name := range names {
		if !historyNameRE.MatchString(name) {
			return nil, errors.New("unknown history entry")
		}
		filePath := filepath.Join(path, name)
		info, err := os.Lstat(filePath)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o777 != 0o600 {
			return nil, errors.New("invalid history entry")
		}
		sample, size, err := readSampleFile(filePath)
		if err != nil || historyFileName(sample.CapturedAt) != name {
			return nil, errors.New("invalid history sample")
		}
		entries = append(entries, historyEntry{name: name, sample: sample, encoded: size})
	}
	return entries, nil
}

func readBoundedEntryNames(directory *os.File, limit int) ([]string, error) {
	if directory == nil || limit < 0 {
		return nil, errors.New("invalid history entry limit")
	}
	names, err := directory.Readdirnames(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > limit {
		return nil, errors.New("too many history entries")
	}
	return names, nil
}

func readSampleFile(path string) (observe.Sample, int64, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return observe.Sample{}, 0, err
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(fd)
		return observe.Sample{}, 0, errors.New("open sample")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxHistorySampleSize {
		return observe.Sample{}, 0, errors.New("invalid sample size")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxHistorySampleSize+1))
	decoder.DisallowUnknownFields()
	var encoded requiredSample
	if err := decoder.Decode(&encoded); err != nil {
		return observe.Sample{}, 0, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return observe.Sample{}, 0, errors.New("trailing sample")
	}
	sample, ok := encoded.sample()
	if !ok || !validSample(sample) {
		return observe.Sample{}, 0, errors.New("invalid sample")
	}
	return sample, info.Size(), nil
}

func historyWithinBounds(entries []historyEntry) bool {
	if len(entries) == 0 {
		return true
	}
	if len(entries) > maxHistorySamples {
		return false
	}
	var total int64
	newest := mustParse(entries[0].sample.CapturedAt)
	oldest := newest
	for _, entry := range entries {
		total += entry.encoded
		capturedAt := mustParse(entry.sample.CapturedAt)
		if capturedAt.After(newest) {
			newest = capturedAt
		}
		if capturedAt.Before(oldest) {
			oldest = capturedAt
		}
	}
	return total <= maxHistoryBytes && !oldest.Before(newest.Add(-maxHistoryAge))
}

func historyPrune(entries []historyEntry) ([]historyEntry, bool) {
	ordered := append([]historyEntry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return mustParse(ordered[i].sample.CapturedAt).Before(mustParse(ordered[j].sample.CapturedAt))
	})
	for {
		total := int64(0)
		for _, entry := range ordered {
			total += entry.encoded
		}
		tooOld := len(ordered) > 0 && mustParse(ordered[0].sample.CapturedAt).Before(mustParse(ordered[len(ordered)-1].sample.CapturedAt).Add(-maxHistoryAge))
		if len(ordered) <= maxHistorySamples && total <= maxHistoryBytes && !tooOld {
			break
		}
		if len(ordered) <= 1 {
			return nil, false
		}
		ordered = ordered[1:]
	}
	kept := make(map[string]bool, len(ordered))
	for _, entry := range ordered {
		kept[entry.name] = true
	}
	pruned := make([]historyEntry, 0)
	for _, entry := range entries {
		if !kept[entry.name] {
			pruned = append(pruned, entry)
		}
	}
	return pruned, true
}

func mustParse(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

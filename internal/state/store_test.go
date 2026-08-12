package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stillmac/internal/observe"
)

func TestStoreRejectsSymlinkedSelectedDirectoryAndPreservesState(t *testing.T) {
	t.Parallel()

	targetDirectory := filepath.Join(t.TempDir(), "real-data")
	targetStore := Store{Directory: targetDirectory}
	original := validFixtureSample(11)
	if err := targetStore.Write(original); err != nil {
		t.Fatalf("seed valid state: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(targetDirectory, FileName))
	if err != nil {
		t.Fatalf("read seeded state: %v", err)
	}

	selectedDirectory := filepath.Join(t.TempDir(), "selected-data")
	if err := os.Symlink(targetDirectory, selectedDirectory); err != nil {
		t.Fatalf("create selected-directory symlink: %v", err)
	}
	symlinkStore := Store{Directory: selectedDirectory}
	if _, err := symlinkStore.Read(); !errors.Is(err, ErrRead) {
		t.Fatalf("Read error = %v, want ErrRead", err)
	}
	if err := symlinkStore.Write(validFixtureSample(22)); !errors.Is(err, ErrWrite) {
		t.Fatalf("Write error = %v, want ErrWrite", err)
	}

	after, err := os.ReadFile(filepath.Join(targetDirectory, FileName))
	if err != nil {
		t.Fatalf("read preserved state: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("failed write through selected-directory symlink changed the last valid state")
	}
	if err := os.Chmod(targetDirectory, 0o755); err != nil {
		t.Fatalf("set target mode: %v", err)
	}
	if err := symlinkStore.Write(validFixtureSample(23)); !errors.Is(err, ErrWrite) {
		t.Fatalf("second Write error = %v, want ErrWrite", err)
	}
	if got := modePerm(t, targetDirectory); got != 0o755 {
		t.Fatalf("symlink target mode = %o, want unchanged 755", got)
	}
}

func TestStoreAppendRejectsHistoryDirectorySymlinkWithoutChangingTarget(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, historyDirectoryName)); err != nil {
		t.Fatal(err)
	}
	store := Store{Directory: root}
	if err := store.Append(sampleAt(24, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))); !errors.Is(err, ErrWrite) {
		t.Fatalf("Append = %v, want ErrWrite", err)
	}
	if got := modePerm(t, target); got != 0o755 {
		t.Fatalf("history symlink target mode = %o, want unchanged 755", got)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("history symlink target changed: %v", entries)
	}
}

func TestStoreAppendAndReadAllStoresSample(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	sample := validFixtureSample(91)
	if err := store.Append(sample); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	got, err := store.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(got) != 1 || got[0].Processes[0].PID != sample.Processes[0].PID {
		t.Fatalf("ReadAll() = %#v, want one appended sample", got)
	}
}

func TestStoreReadAllOrdersHistoryAndLegacyByCaptureTime(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	legacy := sampleAt(101, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC))
	middle := sampleAt(102, time.Date(2026, 8, 7, 12, 1, 0, 0, time.UTC))
	latest := sampleAt(103, time.Date(2026, 8, 7, 12, 2, 0, 0, time.UTC))
	if err := store.Write(legacy); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := store.Append(latest); err != nil {
		t.Fatalf("Append latest: %v", err)
	}
	if err := store.Append(middle); err != nil {
		t.Fatalf("Append middle: %v", err)
	}
	got, err := store.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(got) != 3 || got[0].CapturedAt != legacy.CapturedAt || got[1].CapturedAt != middle.CapturedAt || got[2].CapturedAt != latest.CapturedAt {
		t.Fatalf("capture order = %#v", got)
	}
}

func TestHistoryPruneUsesNewestSampleAgeBoundary(t *testing.T) {
	newest := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		oldest    time.Time
		wantCount int
	}{
		{name: "before boundary", oldest: newest.Add(-maxHistoryAge - time.Nanosecond), wantCount: 1},
		{name: "at boundary", oldest: newest.Add(-maxHistoryAge), wantCount: 2},
		{name: "after boundary", oldest: newest.Add(-maxHistoryAge + time.Nanosecond), wantCount: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries := []historyEntry{{name: "old", sample: sampleAt(201, test.oldest), encoded: 1}, {name: "new", sample: sampleAt(202, newest), encoded: 1}}
			pruned, ok := historyPrune(entries)
			if !ok {
				t.Fatal("historyPrune rejected boundary fixture")
			}
			if len(pruned) != 2-test.wantCount {
				t.Fatalf("pruned %d entries, want %d", len(pruned), 2-test.wantCount)
			}
		})
	}
}

func TestStoreReadAllOrdersByParsedCaptureTime(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	earlier := sampleWithCapturedAt(203, "2026-08-20T00:00:00.1Z")
	later := sampleWithCapturedAt(204, "2026-08-20T00:00:00.11Z")
	if err := store.Append(later); err != nil {
		t.Fatalf("append later: %v", err)
	}
	if err := store.Append(earlier); err != nil {
		t.Fatalf("append earlier: %v", err)
	}
	got, err := store.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 2 || got[0].CapturedAt != earlier.CapturedAt || got[1].CapturedAt != later.CapturedAt {
		t.Fatalf("parsed capture order = %#v", got)
	}
}

func TestStoreAppendRejectsDuplicateLegacyTimestamp(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	legacy := sampleAt(205, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC))
	if err := store.Write(legacy); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := store.Append(legacy); !errors.Is(err, ErrWrite) {
		t.Fatalf("duplicate legacy append = %v, want ErrWrite", err)
	}
	if _, err := os.Stat(filepath.Join(store.Directory, "samples")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate append created history: %v", err)
	}
}

func TestStoreReadAllFailsClosedOnLegacyHistoryDuplicate(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	duplicate := sampleAt(215, time.Date(2026, 8, 20, 1, 30, 0, 0, time.UTC))
	if err := store.Write(duplicate); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := os.Mkdir(filepath.Join(store.Directory, historyDirectoryName), 0o700); err != nil {
		t.Fatal(err)
	}
	contents, err := marshalSample(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Directory, historyDirectoryName, historyFileName(duplicate.CapturedAt)), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
		t.Fatalf("ReadAll = %v, want ErrRead", err)
	}
}

func TestStoreWriteAndAppendRepairPreexistingDirectoryPermissions(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	store := Store{Directory: directory}
	if err := os.Mkdir(filepath.Join(directory, historyDirectoryName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(validFixtureSample(206)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := modePerm(t, directory); got != 0o700 {
		t.Fatalf("root mode after Write = %o, want 700", got)
	}
	history := filepath.Join(directory, historyDirectoryName)
	if err := os.Chmod(history, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(sampleAt(207, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := modePerm(t, directory); got != 0o700 {
		t.Fatalf("root mode after Append = %o, want 700", got)
	}
	if got := modePerm(t, history); got != 0o700 {
		t.Fatalf("history mode after Append = %o, want 700", got)
	}
}

func TestStoreReadAllRejectsExposedHistoryDirectory(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	if err := store.Append(sampleAt(208, time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(store.Directory, historyDirectoryName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
		t.Fatalf("ReadAll = %v, want ErrRead", err)
	}
}

func TestStoreReadRejectsExposedHistoryDirectory(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	legacy := sampleAt(216, time.Date(2026, 8, 20, 2, 30, 0, 0, time.UTC))
	if err := store.Write(legacy); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(sampleAt(217, time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(store.Directory, historyDirectoryName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); !errors.Is(err, ErrRead) {
		t.Fatalf("Read = %v, want ErrRead", err)
	}
}

func TestStoreMalformedLegacyBlocksReadAllAndAppendWithoutMutation(t *testing.T) {
	directory := t.TempDir()
	legacyPath := filepath.Join(directory, FileName)
	malformed := []byte("{")
	if err := os.WriteFile(legacyPath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Directory: directory}
	if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
		t.Fatalf("ReadAll = %v, want ErrRead", err)
	}
	if err := store.Append(sampleAt(218, time.Date(2026, 8, 20, 4, 0, 0, 0, time.UTC))); !errors.Is(err, ErrWrite) {
		t.Fatalf("Append = %v, want ErrWrite", err)
	}
	got, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(malformed) {
		t.Fatal("malformed legacy state was changed")
	}
	if _, err := os.Stat(filepath.Join(directory, historyDirectoryName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed append created history: %v", err)
	}
}

func TestStoreAppendRejectsBackdatedCandidateSelectedForPruning(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	latestTime := time.Date(2026, 8, 20, 5, 0, 0, 0, time.UTC)
	latest := sampleAt(219, latestTime)
	backdated := sampleAt(220, latestTime.Add(-maxHistoryAge-time.Nanosecond))
	if err := store.Append(latest); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(backdated); !errors.Is(err, ErrWrite) {
		t.Fatalf("backdated Append = %v, want ErrWrite", err)
	}
	got, err := store.ReadAll()
	if err != nil || len(got) != 1 || got[0].CapturedAt != latest.CapturedAt {
		t.Fatalf("history after rejected backdated sample = %#v, %v", got, err)
	}
}

func TestStoreHistoryRejectsSymlinkAndNonRegularEntries(t *testing.T) {
	tests := []struct {
		name string
		make func(string, string) error
	}{
		{name: "symlink", make: func(directory, name string) error { return os.Symlink("missing", filepath.Join(directory, name)) }},
		{name: "directory", make: func(directory, name string) error { return os.Mkdir(filepath.Join(directory, name), 0o700) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := Store{Directory: t.TempDir()}
			if err := store.Append(sampleAt(209, time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC))); err != nil {
				t.Fatal(err)
			}
			history := filepath.Join(store.Directory, historyDirectoryName)
			if err := test.make(history, "sample-invalid.json"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
				t.Fatalf("ReadAll = %v, want ErrRead", err)
			}
		})
	}
}

func TestStoreReadAllRejectsMalformedHistoryJSONSyntax(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	if err := store.Append(sampleAt(210, time.Date(2026, 8, 20, 4, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Directory, historyDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Directory, historyDirectoryName, entries[0].Name())
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
		t.Fatalf("ReadAll = %v, want ErrRead", err)
	}
}

func TestHistoryPruneSizeBoundaryIsOldestFirst(t *testing.T) {
	entry := func(name string, size int64) historyEntry {
		return historyEntry{name: name, sample: sampleWithCapturedAt(211+len(name), "2026-08-20T00:00:00Z"), encoded: size}
	}
	pruned, ok := historyPrune([]historyEntry{entry("a", maxHistoryBytes), entry("b", 1)})
	if !ok {
		t.Fatal("historyPrune rejected candidate that fits after pruning oldest")
	}
	if len(pruned) != 1 || pruned[0].name != "a" {
		t.Fatalf("pruned entries = %#v, want oldest entry a", pruned)
	}
	if _, ok := historyPrune([]historyEntry{entry("a", maxHistoryBytes-1), entry("b", 1)}); !ok {
		t.Fatal("historyPrune rejected a candidate exactly at total limit")
	}
	if _, ok := historyPrune([]historyEntry{entry("a", maxHistoryBytes+1)}); ok {
		t.Fatal("historyPrune accepted oversized candidate")
	}
}

func TestStoreAppendRollbackAfterStagedPruneRestoresHistory(t *testing.T) {
	directory := t.TempDir()
	first := sampleAt(212, time.Date(2026, 8, 1, 5, 0, 0, 0, time.UTC))
	second := sampleAt(213, time.Date(2026, 8, 2, 5, 1, 0, 0, time.UTC))
	store := Store{Directory: directory}
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(second); err != nil {
		t.Fatal(err)
	}
	backupRenames := 0
	store.ops = &storeOps{rename: func(old, new string) error {
		if strings.Contains(old, "sample-") && strings.Contains(new, ".stillmac-backup-") {
			backupRenames++
			if backupRenames == 2 {
				return errors.New("injected prune failure")
			}
		}
		return os.Rename(old, new)
	}}
	if err := store.Append(sampleAt(214, time.Date(2026, 8, 20, 5, 2, 0, 0, time.UTC))); !errors.Is(err, ErrWrite) {
		t.Fatalf("failed append = %v, want ErrWrite", err)
	}
	got, err := store.ReadAll()
	if err != nil || len(got) != 2 {
		t.Fatalf("ReadAll after rollback = %#v, %v", got, err)
	}
	for _, sample := range []observe.Sample{first, second} {
		if _, err := os.Stat(filepath.Join(directory, historyDirectoryName, historyFileName(sample.CapturedAt))); err != nil {
			t.Fatalf("sample %s was not restored: %v", sample.CapturedAt, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(directory, historyDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stillmac-") {
			t.Fatalf("temporary residue: %s", entry.Name())
		}
	}
}

func TestStoreAppendDuplicateTimestampDoesNotReplace(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	original := sampleAt(111, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))
	replacement := original
	replacement.Processes[0].PID = 112
	if err := store.Append(original); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := store.Append(replacement); !errors.Is(err, ErrWrite) {
		t.Fatalf("duplicate Append error = %v, want ErrWrite", err)
	}
	got, err := store.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 1 || got[0].Processes[0].PID != original.Processes[0].PID {
		t.Fatalf("duplicate replaced sample: %#v", got)
	}
}

func TestStoreHistoryPermissionsAndNoTemporaryResidue(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	if err := store.Append(sampleAt(121, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rootInfo, err := os.Stat(filepath.Join(store.Directory, "samples"))
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm()&0o777 != 0o700 {
		t.Fatalf("samples mode = %o", rootInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Join(store.Directory, "samples"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name()[0] == '.' {
		t.Fatalf("unexpected history entries: %v", entries)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o777 != 0o600 {
		t.Fatalf("sample mode = %o", info.Mode().Perm())
	}
}

func TestStoreHistoryRejectsUnknownEntryAndSymlinkWithoutMutation(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	if err := store.Append(sampleAt(131, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("seed: %v", err)
	}
	directory := filepath.Join(store.Directory, "samples")
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(sampleAt(132, time.Date(2026, 8, 8, 0, 1, 0, 0, time.UTC))); !errors.Is(err, ErrWrite) {
		t.Fatalf("unknown entry Append = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "notes.txt")); err != nil {
		t.Fatalf("unknown entry was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, historyDirectoryName, historyFileName(sampleAt(132, time.Date(2026, 8, 8, 0, 1, 0, 0, time.UTC)).CapturedAt))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed append published a new sample: %v", err)
	}
	if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
		t.Fatalf("unknown entry ReadAll = %v", err)
	}
}

func TestStoreHistoryRetentionRemovesOldestByCount(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i <= maxHistorySamples; i++ {
		if err := store.Append(sampleAt(1000+i, base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("count Append %d: %v", i, err)
		}
	}
	got, err := store.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll after count: %v", err)
	}
	if len(got) != maxHistorySamples {
		t.Fatalf("history count = %d, want %d", len(got), maxHistorySamples)
	}
	if got[0].Processes[0].PID != 1001 {
		t.Fatalf("oldest retained PID = %d, want 1001", got[0].Processes[0].PID)
	}
}

func TestStoreAppendRejectsOversizedAndStrictMalformedHistory(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	oversized := sampleAt(151, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))
	process := oversized.Processes[0]
	oversized.Processes = make([]observe.Process, 65536)
	for i := range oversized.Processes {
		oversized.Processes[i] = process
		oversized.Processes[i].PID = i + 1
	}
	oversized.Quality.ProcessRowsObserved = len(oversized.Processes)
	oversized.Quality.ProcessRowsAccepted = len(oversized.Processes)
	if err := store.Append(oversized); !errors.Is(err, ErrWrite) {
		t.Fatalf("oversized Append = %v", err)
	}
	if err := store.Append(sampleAt(152, time.Date(2026, 8, 8, 0, 1, 0, 0, time.UTC))); err != nil {
		t.Fatalf("seed Append: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Directory, "samples"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Directory, "samples", entries[0].Name())
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(contents[:len(contents)-1], []byte(`,"unknown":true}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAll(); !errors.Is(err, ErrRead) {
		t.Fatalf("malformed history ReadAll = %v", err)
	}
}

func TestStoreAppendStagingRemovalFailureRollsBack(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	first := sampleAt(901, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	second := sampleAt(902, time.Date(2026, 8, 20, 0, 1, 0, 0, time.UTC))
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	failed := false
	store.ops = &storeOps{remove: func(path string) error {
		if strings.Contains(path, ".stillmac-sample-") && !failed {
			failed = true
			return errors.New("injected staging cleanup failure")
		}
		return os.Remove(path)
	}}
	if err := store.Append(second); !errors.Is(err, ErrWrite) {
		t.Fatalf("Append = %v, want ErrWrite", err)
	}
	got, err := store.ReadAll()
	if err != nil || len(got) != 1 || got[0].CapturedAt != first.CapturedAt {
		t.Fatalf("history after rollback = %#v, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Directory, historyDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stillmac-") || entry.Name() == historyFileName(second.CapturedAt) {
			t.Fatalf("residue or published failed sample: %s", entry.Name())
		}
	}
}

func TestStoreAppendBackupCleanupFailureRestoresHistory(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	first := sampleAt(903, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	second := sampleAt(904, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	latest := sampleAt(905, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(second); err != nil {
		t.Fatal(err)
	}
	failed := false
	completedBackupRemoves := 0
	store.ops = &storeOps{remove: func(path string) error {
		if strings.Contains(path, ".stillmac-backup-") && !failed {
			info, statErr := os.Stat(path)
			if statErr == nil && info.Size() > 0 {
				completedBackupRemoves++
				if completedBackupRemoves == 2 {
					failed = true
					return errors.New("injected backup cleanup failure")
				}
			}
		}
		return os.Remove(path)
	}}
	if err := store.Append(latest); !errors.Is(err, ErrWrite) {
		t.Fatalf("Append = %v, want ErrWrite", err)
	}
	got, err := store.ReadAll()
	if err != nil || len(got) != 2 {
		t.Fatalf("restored history = %#v, %v", got, err)
	}
	for _, sample := range []observe.Sample{first, second} {
		if _, err := os.Stat(filepath.Join(store.Directory, historyDirectoryName, historyFileName(sample.CapturedAt))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreAppendRollbackRetriesRestorationRename(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	first := sampleAt(906, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	latest := sampleAt(907, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	backupCleanupFailed := false
	store.ops = &storeOps{
		remove: func(path string) error {
			if strings.Contains(path, ".stillmac-backup-") && !backupCleanupFailed {
				info, statErr := os.Stat(path)
				if statErr == nil && info.Size() > 0 {
					backupCleanupFailed = true
					return errors.New("injected cleanup failure")
				}
			}
			return os.Remove(path)
		},
		rename: func(oldPath, newPath string) error {
			if strings.Contains(oldPath, ".stillmac-backup-") {
				return errors.New("persistent injected restore failure")
			}
			return os.Rename(oldPath, newPath)
		},
	}
	if err := store.Append(latest); !errors.Is(err, ErrWrite) {
		t.Fatalf("Append = %v, want ErrWrite", err)
	}
	if _, err := os.Stat(filepath.Join(store.Directory, historyDirectoryName, historyFileName(first.CapturedAt))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Directory, historyDirectoryName, historyFileName(latest.CapturedAt))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed sample exists: %v", err)
	}
}

func TestStoreWriteReportsDirectorySyncFailure(t *testing.T) {
	store := Store{Directory: t.TempDir(), ops: &storeOps{syncDirectory: func(*os.File) error {
		return errors.New("injected directory sync failure")
	}}}
	if err := store.Write(validFixtureSample(908)); !errors.Is(err, ErrWrite) {
		t.Fatalf("Write = %v, want ErrWrite", err)
	}
}

func TestStoreAppendDirectorySyncFailureRollsBack(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	first := sampleAt(909, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	latest := sampleAt(910, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	store.ops = &storeOps{syncDirectory: func(*os.File) error {
		return errors.New("injected directory sync failure")
	}}
	if err := store.Append(latest); !errors.Is(err, ErrWrite) {
		t.Fatalf("Append = %v, want ErrWrite", err)
	}
	got, err := store.ReadAll()
	if err != nil || len(got) != 1 || got[0].CapturedAt != first.CapturedAt {
		t.Fatalf("history after sync rollback = %#v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(store.Directory, historyDirectoryName, historyFileName(latest.CapturedAt))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed sample exists: %v", err)
	}
}

func TestHistoryReadBoundsFailClosed(t *testing.T) {
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	entry := func(i int, at time.Time, size int64) historyEntry {
		return historyEntry{name: string(rune('a' + i%26)), sample: sampleAt(10000+i, at), encoded: size}
	}
	if historyWithinBounds([]historyEntry{entry(0, base, 1), entry(1, base.Add(-maxHistoryAge-time.Nanosecond), 1)}) {
		t.Fatal("accepted over-age history")
	}
	tooMany := make([]historyEntry, maxHistorySamples+1)
	for i := range tooMany {
		tooMany[i] = entry(i, base, 1)
	}
	if historyWithinBounds(tooMany) {
		t.Fatal("accepted over-count history")
	}
	if historyWithinBounds([]historyEntry{entry(0, base, maxHistoryBytes+1)}) {
		t.Fatal("accepted over-size history")
	}
}

func TestReadBoundedEntryNamesRejectsMoreThanHistoryLimit(t *testing.T) {
	directoryPath := t.TempDir()
	for index := 0; index <= maxHistorySamples; index++ {
		name := filepath.Join(directoryPath, fmt.Sprintf("entry-%04d", index))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(directoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if _, err := readBoundedEntryNames(directory, maxHistorySamples); err == nil {
		t.Fatal("accepted more than the bounded history entry limit")
	}
}

func TestValidSampleCanonicalTimestampTable(t *testing.T) {
	for _, test := range []struct {
		timestamp string
		valid     bool
	}{
		{"2026-08-20T00:00:00Z", true}, {"2026-08-20T00:00:00.1Z", true}, {"2026-08-20T00:00:00.123456789Z", true},
		{"2026-08-20T00:00:00.10Z", false}, {"2026-08-20T00:00:00,1Z", false}, {"2026-08-20T00:00:00+00:00", false},
	} {
		t.Run(test.timestamp, func(t *testing.T) {
			sample := validFixtureSample(100)
			sample.CapturedAt = test.timestamp
			if validSample(sample) != test.valid {
				t.Fatalf("validSample(%q) = %v, want %v", test.timestamp, validSample(sample), test.valid)
			}
		})
	}
}
func sampleAt(pid int, capturedAt time.Time) observe.Sample {
	sample := validFixtureSample(pid)
	sample.CapturedAt = capturedAt.UTC().Format(time.RFC3339Nano)
	return sample
}

func sampleWithCapturedAt(pid int, capturedAt string) observe.Sample {
	sample := validFixtureSample(pid)
	sample.CapturedAt = capturedAt
	return sample
}

func modePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm() & 0o777
}

func TestStoreNeverFollowsOrReplacesStateFileSymlink(t *testing.T) {
	t.Parallel()

	victimDirectory := filepath.Join(t.TempDir(), "victim")
	victimStore := Store{Directory: victimDirectory}
	if err := victimStore.Write(validFixtureSample(31)); err != nil {
		t.Fatalf("seed victim state: %v", err)
	}
	victimPath := filepath.Join(victimDirectory, FileName)
	victimBefore, err := os.ReadFile(victimPath)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}

	selectedDirectory := filepath.Join(t.TempDir(), "selected")
	if err := os.Mkdir(selectedDirectory, 0o700); err != nil {
		t.Fatalf("mkdir selected directory: %v", err)
	}
	statePath := filepath.Join(selectedDirectory, FileName)
	if err := os.Symlink(victimPath, statePath); err != nil {
		t.Fatalf("create state symlink: %v", err)
	}
	store := Store{Directory: selectedDirectory}
	if _, err := store.Read(); !errors.Is(err, ErrRead) {
		t.Fatalf("Read error = %v, want ErrRead", err)
	}
	if err := store.Write(validFixtureSample(32)); !errors.Is(err, ErrWrite) {
		t.Fatalf("Write error = %v, want ErrWrite", err)
	}

	info, err := os.Lstat(statePath)
	if err != nil {
		t.Fatalf("lstat state symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("failed write replaced the state-file symlink")
	}
	victimAfter, err := os.ReadFile(victimPath)
	if err != nil {
		t.Fatalf("read victim after failed write: %v", err)
	}
	if string(victimAfter) != string(victimBefore) {
		t.Fatal("failed state-file symlink write changed the symlink target")
	}
}

func TestStoreRejectsNonRegularStateFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	statePath := filepath.Join(directory, FileName)
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatalf("create directory at state path: %v", err)
	}
	store := Store{Directory: directory}
	if _, err := store.Read(); !errors.Is(err, ErrRead) {
		t.Fatalf("Read error = %v, want ErrRead", err)
	}
	if err := store.Write(validFixtureSample(41)); !errors.Is(err, ErrWrite) {
		t.Fatalf("Write error = %v, want ErrWrite", err)
	}
	info, err := os.Lstat(statePath)
	if err != nil || !info.IsDir() {
		t.Fatalf("failed write changed non-regular state path: info=%v err=%v", info, err)
	}
}

func TestStoreRejectsStateLargerThanEightMiBBeforeDecode(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	contents, err := json.Marshal(State{SchemaVersion: "stillmac.state.v1", Sample: validFixtureSample(51)})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	contents = append(contents, []byte(strings.Repeat(" ", 8*1024*1024+1-len(contents)))...)
	if err := os.WriteFile(filepath.Join(directory, FileName), contents, 0o600); err != nil {
		t.Fatalf("write oversized state: %v", err)
	}

	if _, err := (Store{Directory: directory}).Read(); !errors.Is(err, ErrRead) {
		t.Fatalf("Read error = %v, want ErrRead", err)
	}
}

func TestStoreRejectsEncodedStateLargerThanEightMiBAndPreservesPreviousState(t *testing.T) {
	t.Parallel()

	store := Store{Directory: t.TempDir()}
	original := validFixtureSample(52)
	if err := store.Write(original); err != nil {
		t.Fatalf("write original: %v", err)
	}

	oversized := validFixtureSample(53)
	process := oversized.Processes[0]
	oversized.Processes = make([]observe.Process, 65536)
	for index := range oversized.Processes {
		oversized.Processes[index] = process
		oversized.Processes[index].PID = index + 1
	}
	oversized.Quality.ProcessRowsObserved = len(oversized.Processes)
	oversized.Quality.ProcessRowsAccepted = len(oversized.Processes)
	encoded, err := json.MarshalIndent(State{SchemaVersion: "stillmac.state.v1", Sample: oversized}, "", "  ")
	if err != nil {
		t.Fatalf("marshal oversized fixture: %v", err)
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) <= maxStateBytes {
		t.Fatalf("oversized fixture has %d bytes, want more than %d", len(encoded), maxStateBytes)
	}

	if err := store.Write(oversized); !errors.Is(err, ErrWrite) {
		t.Fatalf("oversized Write error = %v, want ErrWrite", err)
	}
	stored, err := store.Read()
	if err != nil {
		t.Fatalf("read preserved state: %v", err)
	}
	if stored.Sample.Processes[0].PID != original.Processes[0].PID {
		t.Fatalf("stored PID = %d, want %d", stored.Sample.Processes[0].PID, original.Processes[0].PID)
	}
}

func TestStoreReadValidation(t *testing.T) {
	t.Parallel()

	validState, err := json.Marshal(State{SchemaVersion: "stillmac.state.v1", Sample: validFixtureSample(61)})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	tests := []struct {
		name     string
		contents string
	}{
		{name: "unknown top-level field", contents: strings.TrimSuffix(string(validState), "}") + `,"private_path":"/Users/alice/Secret Workspace"}`},
		{name: "trailing JSON", contents: string(validState) + `{}`},
		{name: "trailing non-whitespace", contents: string(validState) + ` secret`},
		{name: "wrong state schema", contents: strings.Replace(string(validState), "stillmac.state.v1", "stillmac.state.v2", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, FileName), []byte(test.contents), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, err := (Store{Directory: directory}).Read(); !errors.Is(err, ErrRead) {
				t.Fatalf("Read error = %v, want ErrRead", err)
			}
		})
	}
}

func TestStoreReadRejectsEveryMissingRequiredMember(t *testing.T) {
	t.Parallel()

	sample := validFixtureSample(62)
	sample.Processes[0].PPID = 0
	sample.Processes[0].CPUPercent = 0
	sample.Processes[0].MemoryPercent = 0
	sample.Processes[0].ElapsedSeconds = 0
	sample.Memory.SwapUsedBytes = 0
	validState, err := json.Marshal(State{SchemaVersion: "stillmac.state.v1", Sample: sample})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	tests := []struct {
		name string
		path []any
	}{
		{name: "state schema_version", path: []any{"schema_version"}},
		{name: "state sample", path: []any{"sample"}},
		{name: "sample schema_version", path: []any{"sample", "schema_version"}},
		{name: "sample captured_at", path: []any{"sample", "captured_at"}},
		{name: "sample processes", path: []any{"sample", "processes"}},
		{name: "sample memory", path: []any{"sample", "memory"}},
		{name: "sample quality", path: []any{"sample", "quality"}},
		{name: "process comm", path: []any{"sample", "processes", 0, "comm"}},
		{name: "process pid", path: []any{"sample", "processes", 0, "pid"}},
		{name: "process ppid zero", path: []any{"sample", "processes", 0, "ppid"}},
		{name: "process cpu_percent zero", path: []any{"sample", "processes", 0, "cpu_percent"}},
		{name: "process memory_percent zero", path: []any{"sample", "processes", 0, "memory_percent"}},
		{name: "process elapsed_seconds zero", path: []any{"sample", "processes", 0, "elapsed_seconds"}},
		{name: "memory pressure", path: []any{"sample", "memory", "pressure"}},
		{name: "memory swap_used_bytes zero", path: []any{"sample", "memory", "swap_used_bytes"}},
		{name: "quality valid", path: []any{"sample", "quality", "valid"}},
		{name: "quality status", path: []any{"sample", "quality", "status"}},
		{name: "quality process_rows_observed", path: []any{"sample", "quality", "process_rows_observed"}},
		{name: "quality process_rows_accepted", path: []any{"sample", "quality", "process_rows_accepted"}},
		{name: "quality process_rows_rejected zero", path: []any{"sample", "quality", "process_rows_rejected"}},
		{name: "quality memory_pressure_available", path: []any{"sample", "quality", "memory_pressure_available"}},
		{name: "quality swap_used_available", path: []any{"sample", "quality", "swap_used_available"}},
		{name: "quality issues empty", path: []any{"sample", "quality", "issues"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var document map[string]any
			if err := json.Unmarshal(validState, &document); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			deleteJSONMemberAtPath(t, document, test.path...)
			contents, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("marshal fixture missing required member: %v", err)
			}
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, FileName), contents, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, err := (Store{Directory: directory}).Read(); !errors.Is(err, ErrRead) {
				t.Fatalf("Read error = %v, want ErrRead", err)
			}
		})
	}
}

func TestValidSampleTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*observe.Sample)
		valid  bool
	}{
		{name: "valid", mutate: func(*observe.Sample) {}, valid: true},
		{name: "wrong schema", mutate: func(sample *observe.Sample) { sample.SchemaVersion = "stillmac.sample.v2" }},
		{name: "no processes", mutate: func(sample *observe.Sample) {
			sample.Processes = nil
			sample.Quality.ProcessRowsAccepted = 0
			sample.Quality.ProcessRowsObserved = 0
		}},
		{name: "non-canonical timestamp", mutate: func(sample *observe.Sample) { sample.CapturedAt = "2026-08-07T13:00:00+01:00" }},
		{name: "invalid pressure", mutate: func(sample *observe.Sample) { sample.Memory.Pressure = "unknown" }},
		{name: "invalid comm", mutate: func(sample *observe.Sample) { sample.Processes[0].Comm = "private path" }},
		{name: "nan cpu", mutate: func(sample *observe.Sample) { sample.Processes[0].CPUPercent = math.NaN() }},
		{name: "invalid quality counts", mutate: func(sample *observe.Sample) { sample.Quality.ProcessRowsObserved = 2 }},
		{name: "unexpected issue", mutate: func(sample *observe.Sample) { sample.Quality.Issues = []string{"private_path"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sample := validFixtureSample(71)
			test.mutate(&sample)
			if got := validSample(sample); got != test.valid {
				t.Fatalf("validSample() = %t, want %t", got, test.valid)
			}
		})
	}
}

func TestFailedInvalidWritePreservesLastValidState(t *testing.T) {
	t.Parallel()

	store := Store{Directory: t.TempDir()}
	original := validFixtureSample(81)
	if err := store.Write(original); err != nil {
		t.Fatalf("write original: %v", err)
	}
	invalid := validFixtureSample(82)
	invalid.Quality.Valid = false
	if err := store.Write(invalid); !errors.Is(err, ErrWrite) {
		t.Fatalf("invalid Write error = %v, want ErrWrite", err)
	}
	stored, err := store.Read()
	if err != nil {
		t.Fatalf("read preserved state: %v", err)
	}
	if stored.Sample.Processes[0].PID != original.Processes[0].PID {
		t.Fatalf("stored PID = %d, want %d", stored.Sample.Processes[0].PID, original.Processes[0].PID)
	}
}

func validFixtureSample(pid int) observe.Sample {
	return observe.Sample{
		SchemaVersion: "stillmac.sample.v1",
		CapturedAt:    time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Processes: []observe.Process{{
			Comm:           "safeproc",
			PID:            pid,
			PPID:           1,
			CPUPercent:     1.5,
			MemoryPercent:  0.5,
			ElapsedSeconds: 60,
		}},
		Memory: observe.Memory{Pressure: "normal", SwapUsedBytes: 1024},
		Quality: observe.Quality{
			Valid:                   true,
			Status:                  "complete",
			ProcessRowsObserved:     1,
			ProcessRowsAccepted:     1,
			ProcessRowsRejected:     0,
			MemoryPressureAvailable: true,
			SwapUsedAvailable:       true,
			Issues:                  []string{},
		},
	}
}

func deleteJSONMemberAtPath(t *testing.T, document any, path ...any) {
	t.Helper()

	current := document
	for _, part := range path[:len(path)-1] {
		switch part := part.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("path member %q parent has type %T", part, current)
			}
			current, ok = object[part]
			if !ok {
				t.Fatalf("path member %q is already absent", part)
			}
		case int:
			array, ok := current.([]any)
			if !ok || part < 0 || part >= len(array) {
				t.Fatalf("path index %d is invalid in %T", part, current)
			}
			current = array[part]
		default:
			t.Fatalf("unsupported path member type %T", part)
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("required member parent has type %T", current)
	}
	member, ok := path[len(path)-1].(string)
	if !ok {
		t.Fatalf("required member name has type %T", path[len(path)-1])
	}
	if _, ok := object[member]; !ok {
		t.Fatalf("required member %q is already absent", member)
	}
	delete(object, member)
}

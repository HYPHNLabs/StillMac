package residue

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"stillmac/internal/cleanup"
)

func (e *Engine) Changes(options ChangeOptions) (Report, error) {
	snapshots, err := e.readSnapshots()
	if err != nil {
		return Report{}, err
	}
	if options.FromID != "" || options.ToID != "" {
		if options.FromID == "" || options.ToID == "" {
			return Report{}, ErrHistory
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
			return Report{}, ErrHistory
		}
		return e.compareReports(*from, *to, snapshots), nil
	}
	if len(snapshots) < 2 {
		return e.noComparisonReport(), nil
	}
	return e.compareReports(snapshots[len(snapshots)-2], snapshots[len(snapshots)-1], snapshots), nil
}

func (e *Engine) SessionReport(options SessionReportOptions) (Report, error) {
	if options.ThresholdBytes < 0 {
		return Report{}, ErrInvalidConfig
	}
	history, err := e.readSnapshots()
	if err != nil {
		return Report{}, err
	}
	current, err := e.capture("snapshot", options.Scopes)
	if err != nil {
		return Report{}, err
	}
	if err := e.appendSnapshot(current); err != nil {
		return Report{}, err
	}
	if len(history) == 0 {
		report := current
		report.ReportKind = "session-report"
		report.FromSnapshot = ""
		report.ToSnapshot = current.SnapshotID
		report.NextStep = "This first explicit snapshot is stored; run session-report again to establish a comparable change."
		report.ReportID = reportID(report)
		return report, nil
	}
	previous := history[len(history)-1]
	comparison := e.compareReports(previous, current, append(history, current))
	report := current
	report.ReportKind = "session-report"
	report.FromSnapshot = previous.SnapshotID
	report.ToSnapshot = current.SnapshotID
	report.Changes = comparison.Changes
	report.Status = comparison.Status
	report.Evidence.Warnings = uniqueStrings(append(report.Evidence.Warnings, comparison.Evidence.Warnings...))
	report.NextStep = comparison.NextStep
	if options.QuietUnchanged && canSuppress(comparison, options.ThresholdBytes) {
		report.Quiet = true
		report.Suppressed = true
		report.NextStep = "No residue change met the requested reporting threshold."
	}
	report.ReportID = reportID(report)
	return report, nil
}

func (e *Engine) noComparisonReport() Report {
	report := Report{
		SchemaVersion: SchemaVersion,
		ReportKind:    "changes",
		Status:        StatusNotPresent,
		CapturedAt:    e.now().Format(time.RFC3339Nano),
		RuleSet:       RuleSet,
		Evidence: Evidence{
			TotalSize:     Size{Status: SizeNotPresent},
			LimitsApplied: limitsApplied,
			Warnings:      []string{"no_comparable_snapshots"},
		},
		Actions:  availableActions(),
		NextStep: "No comparable snapshot pair exists; run an explicit snapshot or session-report first.",
	}
	report.ReportID = reportID(report)
	return report
}

func (e *Engine) compareReports(previous, current Report, history []Report) Report {
	report := Report{
		SchemaVersion: SchemaVersion,
		ReportKind:    "changes",
		Status:        StatusUnknown,
		CapturedAt:    e.now().Format(time.RFC3339Nano),
		RuleSet:       RuleSet,
		ScopeBindings: current.ScopeBindings,
		Evidence:      current.Evidence,
		Actions:       availableActions(),
		NextStep:      "Resolve the unavailable or incompatible evidence before comparing snapshots.",
		Entries:       current.Entries,
		FromSnapshot:  previous.SnapshotID,
		ToSnapshot:    current.SnapshotID,
	}
	if previous.RuleSet != current.RuleSet {
		report.Evidence.Warnings = uniqueStrings(append(report.Evidence.Warnings, "rule_set_mismatch"))
		report.ReportID = reportID(report)
		return report
	}
	if !compatibleScopes(previous.ScopeBindings, current.ScopeBindings) {
		report.Evidence.Warnings = uniqueStrings(append(report.Evidence.Warnings, "scope_binding_mismatch"))
		report.ReportID = reportID(report)
		return report
	}
	previousEntries := make(map[string]Entry, len(previous.Entries))
	currentEntries := make(map[string]Entry, len(current.Entries))
	for _, entry := range previous.Entries {
		previousEntries[entry.ID] = entry
	}
	for _, entry := range current.Entries {
		currentEntries[entry.ID] = entry
	}
	if len(previousEntries) != len(currentEntries) {
		report.Evidence.Warnings = uniqueStrings(append(report.Evidence.Warnings, "entry_set_mismatch"))
		report.ReportID = reportID(report)
		return report
	}
	for id := range previousEntries {
		if _, ok := currentEntries[id]; !ok {
			report.Evidence.Warnings = uniqueStrings(append(report.Evidence.Warnings, "entry_set_mismatch"))
			report.ReportID = reportID(report)
			return report
		}
	}

	ids := make([]string, 0, len(currentEntries))
	for id := range currentEntries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	changes := make([]Change, 0, len(ids))
	comparisonComplete := true
	knownComparisons := 0
	allNotPresent := true
	for _, id := range ids {
		before := previousEntries[id]
		after := currentEntries[id]
		change := compareEntry(before, after)
		if change.Kind != ChangeUnchanged || change.Regrowth {
			allNotPresent = false
		}
		if change.Kind == ChangeUnknown {
			comparisonComplete = false
		} else {
			knownComparisons++
		}
		if before.Size.Status != SizeNotPresent || after.Size.Status != SizeNotPresent {
			allNotPresent = false
		}
		if change.Kind == ChangeGrowth && !change.Regrowth {
			if e.regrowthEvidence(after, previous, current, history) {
				change.Regrowth = true
				change.Evidence = append(change.Evidence, "successful_go_cleanup_then_growth")
			}
		}
		changes = append(changes, change)
	}
	report.Changes = changes
	sourceIncomplete := !comparableReportStatus(previous.Status) || !comparableReportStatus(current.Status)
	if !comparisonComplete || sourceIncomplete {
		if knownComparisons == 0 {
			report.Status = StatusUnknown
		} else {
			report.Status = StatusPartial
		}
		warnings := report.Evidence.Warnings
		if !comparisonComplete {
			warnings = append(warnings, "entry_measurement_incomplete")
		}
		if sourceIncomplete {
			warnings = append(warnings, "snapshot_status_incomplete")
		}
		report.Evidence.Warnings = uniqueStrings(warnings)
		report.NextStep = "Review the incomplete entry evidence; no growth, shrink, or reclaim is asserted for it."
	} else if allNotPresent {
		report.Status = StatusNotPresent
		report.NextStep = "Both comparable observations found no selected residue."
	} else {
		report.Status = StatusComplete
		report.NextStep = "Review the comparable size changes; no residue removal is available from these commands."
	}
	report.ReportID = reportID(report)
	return report
}

func comparableReportStatus(status Status) bool {
	return status == StatusComplete || status == StatusNotPresent
}

func compareEntry(previous, current Entry) Change {
	change := Change{
		EntryID:       current.ID,
		ScopeID:       current.ScopeID,
		Family:        current.Family,
		Kind:          ChangeUnknown,
		PreviousBytes: cloneInt64(previous.Size.Bytes),
		CurrentBytes:  cloneInt64(current.Size.Bytes),
		Evidence:      []string{},
	}
	if previous.Family != current.Family || previous.RuleVersion != current.RuleVersion || previous.ScopeID != current.ScopeID {
		change.Evidence = append(change.Evidence, "entry_identity_mismatch")
		return change
	}
	if previous.Kind == "git-worktree" && current.Kind == "git-worktree" {
		if previous.Identity != "" && previous.Identity == current.Identity && previous.State == current.State {
			change.Kind = ChangeUnchanged
			change.Evidence = append(change.Evidence, "metadata_only_unchanged")
		} else {
			change.Kind = ChangeReplaced
			change.Evidence = append(change.Evidence, "worktree_metadata_changed")
		}
		return change
	}
	if previous.Size.Status == SizeNotPresent && current.Size.Status == SizeNotPresent {
		change.Kind = ChangeUnchanged
		change.Evidence = append(change.Evidence, "both_not_present")
		return change
	}
	if previous.Size.Status != SizeComplete || current.Size.Status != SizeComplete || previous.Size.Bytes == nil || current.Size.Bytes == nil {
		change.Evidence = append(change.Evidence, "size_not_complete")
		return change
	}
	if previous.Identity == "" || current.Identity == "" {
		change.Evidence = append(change.Evidence, "identity_unavailable")
		return change
	}
	if previous.Identity != current.Identity {
		change.Kind = ChangeReplaced
		change.Evidence = append(change.Evidence, "root_identity_changed")
		return change
	}
	delta, ok := safeAdd(*current.Size.Bytes, -*previous.Size.Bytes)
	if !ok {
		change.Evidence = append(change.Evidence, "delta_overflow")
		return change
	}
	change.DeltaBytes = &delta
	switch {
	case delta > 0:
		change.Kind = ChangeGrowth
	case delta < 0:
		change.Kind = ChangeShrink
	default:
		change.Kind = ChangeUnchanged
	}
	return change
}

func compatibleScopes(previous, current []ScopeBinding) bool {
	if len(previous) != len(current) {
		return false
	}
	for index := range previous {
		if previous[index] != current[index] {
			return false
		}
	}
	return true
}

func canSuppress(report Report, threshold int64) bool {
	if report.Status != StatusComplete && report.Status != StatusNotPresent {
		return false
	}
	for _, change := range report.Changes {
		switch change.Kind {
		case ChangeUnknown, ChangeReplaced:
			return false
		case ChangeGrowth, ChangeShrink:
			if change.DeltaBytes == nil || materialDelta(*change.DeltaBytes, threshold) {
				return false
			}
		}
	}
	return true
}

func materialDelta(delta, threshold int64) bool {
	if delta > threshold {
		return true
	}
	if delta < 0 && delta < -threshold {
		return true
	}
	return false
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func (e *Engine) regrowthEvidence(entry Entry, previous, current Report, history []Report) bool {
	if entry.CleanupCandidateID == "" || entry.Family != "go-build-cache" {
		return false
	}
	cleanupDir := filepath.Join(e.Config.DataDir, "cleanup")
	info, err := os.Lstat(cleanupDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	cleanupHistory, err := (&cleanup.Engine{Config: cleanup.Config{DataDir: e.Config.DataDir}}).History()
	if err != nil {
		return false
	}
	previousAt, err := time.Parse(time.RFC3339Nano, previous.CapturedAt)
	if err != nil {
		return false
	}
	for _, receipt := range cleanupHistory {
		if receipt.CandidateID != entry.CleanupCandidateID || receipt.Result != "cleaned" || receipt.Method != "owner-native-go-clean-cache" || receipt.RemovedBytes <= 0 {
			continue
		}
		receiptAt, parseErr := time.Parse(time.RFC3339Nano, receipt.Timestamp)
		if parseErr != nil || !receiptAt.Before(previousAt) {
			continue
		}
		for _, earlier := range history {
			if earlier.SnapshotID == previous.SnapshotID || earlier.RuleSet != previous.RuleSet || !compatibleScopes(earlier.ScopeBindings, previous.ScopeBindings) {
				continue
			}
			if !earlierAtBefore(earlier, receiptAt) {
				continue
			}
			beforeEntry, ok := findEntry(earlier.Entries, entry.ID)
			fromEntry, fromOK := findEntry(previous.Entries, entry.ID)
			if !ok || !fromOK || beforeEntry.Size.Status != SizeComplete || fromEntry.Size.Status != SizeComplete || beforeEntry.Size.Bytes == nil || fromEntry.Size.Bytes == nil || beforeEntry.Identity != fromEntry.Identity {
				continue
			}
			if *fromEntry.Size.Bytes < *beforeEntry.Size.Bytes {
				return true
			}
		}
	}
	return false
}

func earlierAtBefore(report Report, target time.Time) bool {
	at, err := time.Parse(time.RFC3339Nano, report.CapturedAt)
	return err == nil && at.Before(target)
}

func findEntry(entries []Entry, id string) (Entry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

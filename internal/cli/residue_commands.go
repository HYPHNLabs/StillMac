package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"stillmac/internal/cleanup"
	"stillmac/internal/residue"
)

type residueOptions struct {
	dataDir, format, from, to string
	scopes                    []residue.Scope
	limits                    residue.Limits
	threshold                 int64
	quiet                     bool
}

func parseResidueOptions(command string, args []string) (residueOptions, error) {
	opts := residueOptions{format: "text"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if name == "--quiet" {
			if command != "session-report" || inline || seen[name] {
				return opts, errInvalidOptions
			}
			opts.quiet, seen[name] = true, true
			continue
		}
		if !strings.HasPrefix(name, "--") || (seen[name] && name != "--scope") {
			return opts, errInvalidOptions
		}
		if !inline {
			i++
			if i >= len(args) {
				return opts, errInvalidOptions
			}
			value = args[i]
		}
		if value == "" || strings.HasPrefix(value, "--") {
			return opts, errInvalidOptions
		}
		seen[name] = true
		switch name {
		case "--data-dir":
			opts.dataDir = value
		case "--format":
			if value != "text" && value != "json" {
				return opts, errInvalidOptions
			}
			opts.format = value
		case "--scope":
			if command == "changes" {
				return opts, errInvalidOptions
			}
			opts.scopes = append(opts.scopes, residue.Scope{Path: value})
		case "--from", "--to":
			if command != "changes" {
				return opts, errInvalidOptions
			}
			if name == "--from" {
				opts.from = value
			} else {
				opts.to = value
			}
		case "--threshold-bytes":
			if command != "session-report" {
				return opts, errInvalidOptions
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return opts, errInvalidOptions
			}
			opts.threshold = n
		case "--max-items", "--max-depth":
			if command == "changes" {
				return opts, errInvalidOptions
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return opts, errInvalidOptions
			}
			if name == "--max-items" {
				opts.limits.MaxItems = n
			} else {
				opts.limits.MaxDepth = n
			}
		case "--max-bytes":
			if command == "changes" {
				return opts, errInvalidOptions
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1 {
				return opts, errInvalidOptions
			}
			opts.limits.MaxBytes = n
		case "--max-duration":
			if command == "changes" {
				return opts, errInvalidOptions
			}
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				return opts, errInvalidOptions
			}
			opts.limits.MaxDuration = d
		default:
			return opts, errInvalidOptions
		}
	}
	return opts, nil
}

func runResidueCommand(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	opts, err := parseResidueOptions(args[0], args[1:])
	if err != nil {
		io.WriteString(stderr, "stillmac: invalid residue options\n")
		return ExitUsage
	}
	if opts.dataDir == "" {
		opts.dataDir, err = commandDataDir(nil, deps.DefaultDataDir)
		if err != nil {
			return ExitState
		}
	}
	homeFn := deps.CleanupHome
	if homeFn == nil {
		homeFn = os.UserHomeDir
	}
	home, err := homeFn()
	if err != nil {
		return ExitState
	}
	gitRunner := deps.GitRunner
	if gitRunner == nil {
		gitRunner = cleanup.NativeInventoryGitRunner
	}
	engine, err := residue.New(residue.Config{Home: home, DataDir: opts.dataDir, Now: deps.Now, GitRunner: gitRunner, GoCleaner: deps.GoCleaner, Limits: opts.limits})
	if err != nil {
		io.WriteString(stderr, "stillmac: invalid residue configuration\n")
		return ExitUsage
	}
	var result residue.Report
	switch args[0] {
	case "inspect":
		result, err = engine.Inspect(opts.scopes)
	case "snapshot":
		result, err = engine.Snapshot(opts.scopes)
	case "changes":
		result, err = engine.Changes(residue.ChangeOptions{FromID: opts.from, ToID: opts.to})
	case "session-report":
		result, err = engine.SessionReport(residue.SessionReportOptions{Scopes: opts.scopes, ThresholdBytes: opts.threshold, QuietUnchanged: opts.quiet})
	}
	if err != nil {
		io.WriteString(stderr, "stillmac: residue report unavailable; check explicit scope, history and private state permissions\n")
		if errors.Is(err, residue.ErrInvalidScope) || errors.Is(err, residue.ErrInvalidConfig) {
			return ExitUsage
		}
		return ExitState
	}
	if result.Suppressed {
		return ExitOK
	}
	if opts.format == "json" {
		err = writeJSON(stdout, result)
	} else {
		err = writeResidueText(stdout, result)
	}
	if err != nil {
		return ExitReport
	}
	return ExitOK
}

func writeResidueText(w io.Writer, report residue.Report) error {
	if _, err := fmt.Fprintf(w, "StillMac %s: %s\nMeasured logical size: %s\n", report.ReportKind, report.Status, residueSizeText(report.Evidence.TotalSize)); err != nil {
		return err
	}
	if report.SnapshotID != "" {
		if _, err := fmt.Fprintf(w, "Snapshot: %s\n", report.SnapshotID); err != nil {
			return err
		}
	}
	if report.FromSnapshot != "" || report.ToSnapshot != "" {
		if _, err := fmt.Fprintf(w, "Compared snapshots: %s -> %s\n", report.FromSnapshot, report.ToSnapshot); err != nil {
			return err
		}
	}
	for _, entry := range report.Entries {
		if _, err := fmt.Fprintf(w, "%s %s: %s; %s; %s\n", entry.ID, entry.Family, residueSizeText(entry.Size), entry.State, entry.ActionAvailability); err != nil {
			return err
		}
		if entry.CleanupCandidateID != "" {
			if _, err := fmt.Fprintf(w, "  cleanup candidate %s: %s\n", entry.CleanupCandidateID, entry.CleanupDecision); err != nil {
				return err
			}
		}
		for _, reason := range entry.Evidence {
			if _, err := fmt.Fprintf(w, "  %s\n", reason); err != nil {
				return err
			}
		}
	}
	for _, change := range report.Changes {
		delta := "not comparable"
		if change.DeltaBytes != nil {
			delta = fmt.Sprintf("%+d logical bytes", *change.DeltaBytes)
		}
		if _, err := fmt.Fprintf(w, "%s %s: %s (%s)\n", change.EntryID, change.Family, change.Kind, delta); err != nil {
			return err
		}
	}
	for _, warning := range report.Evidence.Warnings {
		if _, err := fmt.Fprintf(w, "Evidence limit: %s\n", warning); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, report.NextStep)
	return err
}

func residueSizeText(size residue.Size) string {
	if size.Bytes == nil {
		return "not measured (" + string(size.Status) + ")"
	}
	value := float64(*size.Bytes)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s (%s)", value, units[unit], size.Status)
}

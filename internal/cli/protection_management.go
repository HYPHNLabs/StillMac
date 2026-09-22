package cli

import (
	"fmt"
	"io"

	"stillmac/internal/cleanup"
)

type protectionManager interface {
	Protections() ([]cleanup.Protection, error)
	Unprotect(string) error
}

func runProtectionCommand(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	opts, err := parseCleanupOptions(args[1:], false, true, args[0] == "protections")
	if err != nil || (args[0] == "protections" && len(opts.positionals) != 0) || (args[0] == "unprotect" && len(opts.positionals) != 1) {
		io.WriteString(stderr, "stillmac: invalid protection options\n")
		return ExitUsage
	}
	service, err := cleanupService(deps, opts.dataDir)
	if err != nil {
		return ExitState
	}
	manager, ok := service.(protectionManager)
	if !ok {
		io.WriteString(stderr, "stillmac: protection management unavailable\n")
		return ExitState
	}
	if args[0] == "unprotect" {
		if err := manager.Unprotect(opts.positionals[0]); err != nil {
			io.WriteString(stderr, "stillmac: protection unavailable or changed\n")
			return ExitState
		}
		if _, err := fmt.Fprintln(stdout, "protection removed; a fresh plan and approval are required before cleanup"); err != nil {
			return ExitReport
		}
		return ExitOK
	}
	rows, err := manager.Protections()
	if err != nil {
		io.WriteString(stderr, "stillmac: protection state unavailable\n")
		return ExitState
	}
	if opts.format == "json" {
		if err := writeJSON(stdout, rows); err != nil {
			return ExitReport
		}
		return ExitOK
	}
	if len(rows) == 0 {
		if _, err := fmt.Fprintln(stdout, "no protected cache candidates"); err != nil {
			return ExitReport
		}
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(stdout, "%s %s protected\n", row.ID, row.Family); err != nil {
			return ExitReport
		}
	}
	return ExitOK
}

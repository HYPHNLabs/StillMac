package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"stillmac/internal/retire"
)

type retireOptions struct {
	command, target, id, dataDir, format string
	userManaged, sessionEnded            bool
}

func parseRetireOptions(args []string) (retireOptions, error) {
	o := retireOptions{format: "text"}
	if len(args) == 0 {
		return o, errInvalidOptions
	}
	o.command = args[0]
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if !strings.HasPrefix(name, "--") {
			if o.id != "" || inline {
				return o, errInvalidOptions
			}
			o.id = args[i]
			continue
		}
		if seen[name] {
			return o, errInvalidOptions
		}
		seen[name] = true
		if name == "--user-managed" || name == "--session-ended" {
			if inline {
				return o, errInvalidOptions
			}
			if name == "--user-managed" {
				o.userManaged = true
			} else {
				o.sessionEnded = true
			}
			continue
		}
		if !inline {
			i++
			if i >= len(args) {
				return o, errInvalidOptions
			}
			value = args[i]
		}
		if value == "" || strings.HasPrefix(value, "--") {
			return o, errInvalidOptions
		}
		switch name {
		case "--target":
			o.target = value
		case "--data-dir":
			o.dataDir = value
		case "--format":
			if value != "text" && value != "json" {
				return o, errInvalidOptions
			}
			o.format = value
		default:
			return o, errInvalidOptions
		}
	}
	switch o.command {
	case "plan":
		if o.target != "" {
			if o.id != "" || !o.userManaged || !o.sessionEnded {
				return o, errInvalidOptions
			}
		} else if o.id == "" || o.userManaged || o.sessionEnded {
			return o, errInvalidOptions
		}
	case "register":
		if o.target == "" || o.id != "" || !o.userManaged || o.sessionEnded {
			return o, errInvalidOptions
		}
	case "release":
		if o.id == "" || !o.sessionEnded || o.userManaged || o.target != "" {
			return o, errInvalidOptions
		}
	case "apply", "approve", "protect", "unprotect", "recover":
		if o.id == "" || o.target != "" || o.userManaged || o.sessionEnded {
			return o, errInvalidOptions
		}
	case "list", "history":
		if o.id != "" || o.target != "" || o.userManaged || o.sessionEnded {
			return o, errInvalidOptions
		}
	default:
		return o, errInvalidOptions
	}
	return o, nil
}

func runRetireCommand(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	o, err := parseRetireOptions(args[1:])
	if err != nil {
		io.WriteString(stderr, "stillmac: invalid retirement options; planning a target requires --user-managed and --session-ended\n")
		return ExitUsage
	}
	if o.dataDir == "" {
		o.dataDir, err = commandDataDir(nil, deps.DefaultDataDir)
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
	host := ""
	if deps.CleanupHostID != nil {
		host = deps.CleanupHostID()
	}
	e := retire.New(retire.Config{Home: home, DataDir: o.dataDir, Now: deps.Now, HostID: host})
	var result any
	switch o.command {
	case "register":
		result, err = e.Register(retire.RegisterRequest{Path: o.target, OwnershipAttestation: retire.OwnerManagedAttestation})
	case "release":
		result, err = e.Release(retire.ReleaseRequest{RegistrationID: o.id, Attestation: retire.OwnerReleaseAttestation})
	case "plan":
		id := o.id
		if o.target != "" {
			var registration retire.Registration
			registration, err = e.Register(retire.RegisterRequest{Path: o.target, OwnershipAttestation: retire.OwnerManagedAttestation})
			if err == nil {
				registration, err = e.Release(retire.ReleaseRequest{RegistrationID: registration.ID, Attestation: retire.OwnerReleaseAttestation})
			}
			id = registration.ID
			if err != nil && registration.ID != "" {
				result = registration
			}
		}
		if err == nil {
			result, err = e.Plan(retire.PlanRequest{RegistrationID: id})
		}
	case "approve":
		result, err = e.Approve(o.id)
	case "apply":
		// The exact caller-supplied plan ID is the approval. Agent callers must
		// obtain human approval before invoking this command, just as for Go.
		_, err = e.Approve(o.id)
		if err == nil {
			result, err = e.Apply(o.id)
		}
	case "list":
		result, err = e.List()
	case "history":
		result, err = e.History()
	case "protect":
		err = e.Protect(o.id)
		if err == nil {
			result = "retirement registration protected"
		}
	case "unprotect":
		err = e.Unprotect(o.id)
		if err == nil {
			result = "retirement protection removed; a fresh plan and approval are required"
		}
	case "recover":
		result, err = e.Recover(o.id)
	}
	if result != nil {
		var writeErr error
		if o.format == "json" {
			writeErr = writeJSON(stdout, result)
		} else {
			writeErr = writeRetireText(stdout, result)
		}
		if writeErr != nil {
			return ExitReport
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "stillmac: retirement %s; no success is claimed\n", retire.CodeOf(err))
		if retire.CodeOf(err) == retire.CodeInvalidRequest {
			return ExitUsage
		}
		return ExitState
	}
	return ExitOK
}

func writeRetireText(w io.Writer, result any) error {
	switch value := result.(type) {
	case retire.Plan:
		if _, err := fmt.Fprintf(w, "Retirement plan %s\nExpires: %s\n", value.PlanID, value.ExpiresAt); err != nil {
			return err
		}
		if err := writeRetireText(w, value.Registration); err != nil {
			return err
		}
		if value.Registration.Decision != retire.DecisionReady {
			_, err := fmt.Fprintln(w, "No retirement action is available; resolve the reported evidence limits before replanning.")
			return err
		}
		_, err := fmt.Fprintf(w, "Review the exact plan before approving with: stillmac retire apply %s\n", value.PlanID)
		return err
	case retire.Registration:
		if _, err := fmt.Fprintf(w, "%s: %s (%s)\n", value.ID, value.Decision, value.Status); err != nil {
			return err
		}
		for _, reason := range value.Reasons {
			if _, err := fmt.Fprintf(w, "  %s\n", reason); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(w, value.Recovery)
		return err
	case []retire.Registration:
		if len(value) == 0 {
			_, err := fmt.Fprintln(w, "no registered worktrees")
			return err
		}
		for _, row := range value {
			if err := writeRetireText(w, row); err != nil {
				return err
			}
		}
		return nil
	case retire.ApplyResult:
		return writeRetireText(w, value.Receipt)
	case retire.Receipt:
		_, err := fmt.Fprintf(w, "Receipt %s: %s %s\n%s\n", value.ReceiptID, value.Operation, value.Result, value.Recovery)
		if err != nil {
			return err
		}
		for _, reason := range value.Reasons {
			if _, err := fmt.Fprintln(w, reason); err != nil {
				return err
			}
		}
		return nil
	case []retire.Receipt:
		if len(value) == 0 {
			_, err := fmt.Fprintln(w, "no retirement receipts")
			return err
		}
		for _, row := range value {
			if err := writeRetireText(w, row); err != nil {
				return err
			}
		}
		return nil
	case retire.RecoveryResult:
		_, err := fmt.Fprintf(w, "%s\n%s\n", value.Result, value.Message)
		return err
	case retire.Approval:
		_, err := fmt.Fprintf(w, "Approved exact plan %s until %s\n", value.PlanID, value.ExpiresAt)
		return err
	case string:
		_, err := fmt.Fprintln(w, value)
		return err
	default:
		return fmt.Errorf("unsupported retirement output")
	}
}

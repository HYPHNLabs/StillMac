package cli

import "io"

// Capabilities describes this source contract, not an installed release version.
func runCapabilities(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 && !(len(args) == 2 && args[0] == "--format" && args[1] == "json") && !(len(args) == 1 && args[0] == "--format=json") {
		io.WriteString(stderr, "stillmac: capabilities accepts only --format json\n")
		return ExitUsage
	}
	result := struct {
		Schema   string   `json:"schema_version"`
		Profile  string   `json:"profile"`
		Released bool     `json:"released"`
		Commands []string `json:"commands"`
		Actions  []string `json:"actions"`
		Approval string   `json:"approval"`
	}{
		Schema: "stillmac.capabilities.v1", Profile: "m1-m2-source", Released: false,
		Commands: []string{"doctor", "sample", "status", "report", "scan", "explain", "plan", "apply", "clean", "protect", "protections", "unprotect", "history", "inspect", "snapshot", "changes", "session-report", "retire"},
		Actions:  []string{"owner-native-go-clean-cache", "owner-native-git-worktree-retirement"},
		Approval: "Each mutation requires explicit approval of its exact plan; inventory and attestation are not approval.",
	}
	if err := writeJSON(stdout, result); err != nil {
		return ExitReport
	}
	return ExitOK
}

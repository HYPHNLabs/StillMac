package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCapabilitiesIdentifiesSourceCommandsWithoutReleaseClaim(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"capabilities", "--format", "json"}, &out, &errOut, Dependencies{})
	if code != ExitOK {
		t.Fatalf("capabilities exit=%d: %s", code, errOut.String())
	}
	var result struct {
		Schema   string   `json:"schema_version"`
		Profile  string   `json:"profile"`
		Released bool     `json:"released"`
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Schema != "stillmac.capabilities.v1" || result.Profile != "m1-m2-source" || result.Released {
		t.Fatalf("invalid source declaration: %s", out.String())
	}
	for _, command := range []string{"inspect", "snapshot", "changes", "session-report", "protections", "unprotect", "retire"} {
		found := false
		for _, name := range result.Commands {
			found = found || name == command
		}
		if !found {
			t.Errorf("missing %s", command)
		}
	}
}

func TestHelpMakesCandidateCommandsDiscoverable(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"help"}, &out, &errOut, Dependencies{}); code != ExitOK {
		t.Fatal(code)
	}
	for _, command := range []string{"capabilities", "inspect", "snapshot", "changes", "session-report", "protections", "unprotect", "retire plan", "retire apply"} {
		if !strings.Contains(out.String(), command) {
			t.Errorf("help omits %s", command)
		}
	}
}

func TestCapabilitiesRejectsUnknownOptions(t *testing.T) {
	for _, args := range [][]string{{"capabilities", "--force"}, {"capabilities", "--format", "yaml"}, {"capabilities", "--format", "json", "--format", "json"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, Dependencies{}); code != ExitUsage {
			t.Errorf("%v exit=%d", args, code)
		}
	}
}

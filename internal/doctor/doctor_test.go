package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckerExecutesFixedRecognisedProbes(t *testing.T) {
	t.Parallel()

	var invocations []string
	checker := Checker{
		GOOS: "darwin",
		Run: func(path string, args ...string) ([]byte, error) {
			invocation := path + " " + strings.Join(args, " ")
			invocations = append(invocations, invocation)
			switch invocation {
			case "/bin/ps -p 1 -o pid=":
				return []byte("  1\n"), nil
			case "/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level":
				return []byte("1\n"), nil
			case "/usr/sbin/sysctl -n vm.swapusage":
				return []byte("total = 4G used = 1G free = 3G (encrypted)\n"), nil
			default:
				return nil, errors.New("unexpected command")
			}
		},
		ProbeDirectory: func(string) error { return nil },
	}

	result := checker.Check("/private/data")
	if result.SchemaVersion != "stillmac.doctor.v1" || result.Status != "ready" {
		t.Fatalf("result = %#v", result)
	}
	wantInvocations := []string{
		"/bin/ps -p 1 -o pid=",
		"/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level",
		"/usr/sbin/sysctl -n vm.swapusage",
	}
	if strings.Join(invocations, "\n") != strings.Join(wantInvocations, "\n") {
		t.Fatalf("invocations = %#v, want %#v", invocations, wantInvocations)
	}
	if len(result.Checks) != 4 {
		t.Fatalf("check count = %d, want 4", len(result.Checks))
	}
}

func TestCheckerRejectsFailedOrUnrecognisedProbeShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ps         string
		pressure   string
		swap       string
		failedCall string
	}{
		{name: "ps failure", ps: "1", pressure: "1", swap: "total = 1G used = 0G free = 1G", failedCall: "ps"},
		{name: "ps contaminated", ps: "1 /Users/alice/private", pressure: "1", swap: "total = 1G used = 0G free = 1G"},
		{name: "pressure failure", ps: "1", pressure: "1", swap: "total = 1G used = 0G free = 1G", failedCall: "pressure"},
		{name: "pressure unrecognised", ps: "1", pressure: "normal", swap: "total = 1G used = 0G free = 1G"},
		{name: "swap failure", ps: "1", pressure: "1", swap: "total = 1G used = 0G free = 1G", failedCall: "swap"},
		{name: "swap unrecognised", ps: "1", pressure: "1", swap: "total = 1PB used = 0PB free = 1PB"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checker := Checker{
				GOOS: "darwin",
				Run: func(path string, args ...string) ([]byte, error) {
					var kind, output string
					switch {
					case path == processCommandPath:
						kind, output = "ps", test.ps
					case len(args) == 2 && args[1] == "kern.memorystatus_vm_pressure_level":
						kind, output = "pressure", test.pressure
					case len(args) == 2 && args[1] == "vm.swapusage":
						kind, output = "swap", test.swap
					default:
						return nil, errors.New("unexpected")
					}
					if kind == test.failedCall {
						return []byte("/Users/alice/Secret Workspace sk-test-NEVER"), errors.New("private native error")
					}
					return []byte(output), nil
				},
				ProbeDirectory: func(string) error { return nil },
			}
			result := checker.Check("/private/data")
			if result.Status != "not_ready" {
				t.Fatalf("status = %q, want not_ready", result.Status)
			}
			for _, item := range result.Checks {
				for _, prohibited := range []string{"alice", "Secret", "Workspace", "sk-test", "/Users/", "private native"} {
					if strings.Contains(item.Detail, prohibited) {
						t.Fatalf("check detail disclosed %q: %#v", prohibited, item)
					}
				}
			}
		})
	}
}

func TestProbeDirectoryRejectsSymlink(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	selected := filepath.Join(t.TempDir(), "selected")
	if err := os.Symlink(target, selected); err != nil {
		t.Fatalf("symlink selected directory: %v", err)
	}
	if err := ProbeDirectory(selected); err == nil {
		t.Fatal("ProbeDirectory accepted a symlinked selected directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink target was modified: %#v", entries)
	}
}

func TestProbeDirectoryRejectsUnsafeStatePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, path string) {
				t.Helper()
				victim := filepath.Join(t.TempDir(), "victim.json")
				if err := os.WriteFile(victim, []byte("preserve"), 0o600); err != nil {
					t.Fatalf("write victim: %v", err)
				}
				if err := os.Symlink(victim, path); err != nil {
					t.Fatalf("symlink state path: %v", err)
				}
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("mkdir state path: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			test.setup(t, filepath.Join(directory, "current-sample.json"))
			if err := ProbeDirectory(directory); err == nil {
				t.Fatal("ProbeDirectory accepted an unsafe state path")
			}
		})
	}
}

func TestRecognisedPSProbeShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		output string
		ok     bool
	}{
		{output: "1", ok: true},
		{output: "  42\n", ok: true},
		{output: "0"},
		{output: "-1"},
		{output: "1 2"},
		{output: "pid"},
		{output: ""},
	}
	for _, test := range tests {
		if got := recognisedPSProbe([]byte(test.output)); got != test.ok {
			t.Errorf("recognisedPSProbe(%q) = %t, want %t", test.output, got, test.ok)
		}
	}
}

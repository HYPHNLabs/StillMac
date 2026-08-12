package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stillmac/internal/baseline"
	"stillmac/internal/cli"
	"stillmac/internal/doctor"
	"stillmac/internal/observe"
	"stillmac/internal/state"
)

func TestDoctorReportsReadyWithoutDisclosingDataPath(t *testing.T) {
	t.Parallel()

	const privatePath = "/Users/mallory/Secret Workspace/stillmac"
	deps := cli.Dependencies{
		Doctor: doctor.Checker{
			GOOS: "darwin",
			Run: func(path string, args ...string) ([]byte, error) {
				invocation := path + " " + strings.Join(args, " ")
				switch invocation {
				case "/bin/ps -p 1 -o pid=":
					return []byte("1\n"), nil
				case "/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level":
					return []byte("1\n"), nil
				case "/usr/sbin/sysctl -n vm.swapusage":
					return []byte("total = 1G used = 0G free = 1G\n"), nil
				default:
					return nil, errors.New("unexpected command")
				}
			},
			ProbeDirectory: func(path string) error {
				if path != privatePath {
					t.Fatalf("probe path = %q, want %q", path, privatePath)
				}
				return nil
			},
		},
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"doctor", "--data-dir", privatePath}, &stdout, &stderr, deps)
	if exitCode != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr=%q", exitCode, cli.ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), privatePath) || strings.Contains(stderr.String(), privatePath) {
		t.Fatal("doctor output disclosed the data directory")
	}

	var got doctor.Result
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode doctor JSON: %v\n%s", err, stdout.String())
	}
	if got.SchemaVersion != "stillmac.doctor.v1" || got.Status != "ready" {
		t.Fatalf("doctor result = %#v", got)
	}
	if len(got.Checks) != 4 {
		t.Fatalf("check count = %d, want 4", len(got.Checks))
	}
}

func TestDoctorReturnsStableFailureWithoutLeakingSensitiveErrors(t *testing.T) {
	t.Parallel()

	const sensitive = "sk-test-DO-NOT-LEAK /Users/alice/private.txt --password=hunter2"
	deps := cli.Dependencies{
		Doctor: doctor.Checker{
			GOOS: "linux",
			Run: func(string, ...string) ([]byte, error) {
				return []byte(sensitive), errors.New(sensitive)
			},
			ProbeDirectory: func(string) error { return errors.New(sensitive) },
		},
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"doctor", "--data-dir", "/private/fixture"}, &stdout, &stderr, deps)
	if exitCode != cli.ExitDoctor {
		t.Fatalf("exit code = %d, want %d", exitCode, cli.ExitDoctor)
	}
	combined := stdout.String() + stderr.String()
	for _, prohibited := range strings.Fields(sensitive) {
		if strings.Contains(combined, prohibited) {
			t.Fatalf("doctor output disclosed prohibited value %q: %s", prohibited, combined)
		}
	}

	var got doctor.Result
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode doctor JSON: %v", err)
	}
	if got.Status != "not_ready" {
		t.Fatalf("status = %q, want not_ready", got.Status)
	}
}

func TestSampleStoresOnlyAllowlistedFieldsWithPrivatePermissions(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "StillMac")
	const processFixture = `101 1 12.5 3.25 01:02 /Users/alice/SecretWorkspace/bin/safeproc --api-key sk-test-NEVER --file /Users/alice/private/payroll.xlsx
not-a-pid 1 0.0 0.0 00:01 /Users/alice/private/badproc --password hunter2
202 101 0.5 0.25 2-03:04:05 helper`

	runner := func(_ context.Context, path string, args ...string) ([]byte, error) {
		invocation := path + " " + strings.Join(args, " ")
		switch invocation {
		case "/bin/ps -axo pid=,ppid=,%cpu=,%mem=,etime=,ucomm=":
			return []byte(processFixture), nil
		case "/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level":
			return []byte("2\n"), nil
		case "/usr/sbin/sysctl -n vm.swapusage":
			return []byte("total = 4096.00M  used = 128.50M  free = 3967.50M  (encrypted)\n"), nil
		default:
			t.Fatalf("unexpected native command: %s", invocation)
			return nil, errors.New("unreachable")
		}
	}
	deps := cli.Dependencies{
		Collector: observe.Collector{
			Run: runner,
			Now: func() time.Time {
				return time.Date(2026, 8, 7, 12, 34, 56, 0, time.FixedZone("private-zone", 3600))
			},
		},
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"sample", "--data-dir", dataDir}, &stdout, &stderr, deps)
	if exitCode != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", exitCode, cli.ExitOK, stdout.String(), stderr.String())
	}

	historyDir := filepath.Join(dataDir, "samples")
	entries, err := os.ReadDir(historyDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("history entries = %v, err=%v", entries, err)
	}
	statePath := filepath.Join(historyDir, entries[0].Name())
	stored, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read history state: %v", err)
	}
	combined := string(stored) + stdout.String() + stderr.String()
	for _, prohibited := range []string{
		"sk-test-NEVER",
		"alice",
		"SecretWorkspace",
		"payroll.xlsx",
		"--api-key",
		"--password",
		"hunter2",
		"/Users/",
		"private-zone",
	} {
		if strings.Contains(combined, prohibited) {
			t.Fatalf("sample artifacts disclosed prohibited value %q\n%s", prohibited, combined)
		}
	}

	var gotSample observe.Sample
	if err := json.Unmarshal(stored, &gotSample); err != nil {
		t.Fatalf("decode sample: %v", err)
	}
	if gotSample.CapturedAt != "2026-08-07T11:34:56Z" {
		t.Fatalf("captured_at = %q", gotSample.CapturedAt)
	}
	if gotSample.Memory.Pressure != "warning" || gotSample.Memory.SwapUsedBytes != 134742016 {
		t.Fatalf("memory observation = %#v", gotSample.Memory)
	}
	if len(gotSample.Processes) != 2 || gotSample.Processes[0].Comm != "safeproc" || gotSample.Processes[1].Comm != "helper" {
		t.Fatalf("process observations = %#v", gotSample.Processes)
	}
	if !gotSample.Quality.Valid || gotSample.Quality.Status != "degraded" || gotSample.Quality.ProcessRowsRejected != 1 {
		t.Fatalf("quality = %#v", gotSample.Quality)
	}

	dirInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("stat data directory: %v", err)
	}
	if permission := dirInfo.Mode().Perm(); permission != 0o700 {
		t.Fatalf("data directory mode = %04o, want 0700", permission)
	}
	historyInfo, err := os.Stat(historyDir)
	if err != nil || historyInfo.Mode().Perm() != 0o700 {
		t.Fatalf("history directory mode = %04o, err=%v", historyInfo.Mode().Perm(), err)
	}
	fileInfo, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if permission := fileInfo.Mode().Perm(); permission != 0o600 {
		t.Fatalf("state file mode = %04o, want 0600", permission)
	}
	matches, err := filepath.Glob(filepath.Join(dataDir, ".stillmac-*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestSampleFailureDoesNotPersistOrDiscloseNativeOutput(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "StillMac")
	const sensitive = "ps failed for /Users/bob/Private Project with ghp_FAKE_TOKEN and --secret"
	deps := cli.Dependencies{
		Collector: observe.Collector{
			Run: func(context.Context, string, ...string) ([]byte, error) {
				return []byte(sensitive), errors.New(sensitive)
			},
		},
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"sample", "--data-dir", dataDir}, &stdout, &stderr, deps)
	if exitCode != cli.ExitCollection {
		t.Fatalf("exit code = %d, want %d", exitCode, cli.ExitCollection)
	}
	combined := stdout.String() + stderr.String()
	for _, prohibited := range []string{"bob", "Private Project", "ghp_FAKE_TOKEN", "--secret", "/Users/"} {
		if strings.Contains(combined, prohibited) {
			t.Fatalf("sample error disclosed prohibited value %q: %s", prohibited, combined)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "samples")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("history exists after failed collection: %v", err)
	}
}

func TestDefaultDataDirectoryFailureUsesStateExit(t *testing.T) {
	t.Parallel()

	const sensitive = "/Users/alice/Secret Workspace unavailable"
	tests := []struct {
		name string
		args []string
	}{
		{name: "doctor", args: []string{"doctor"}},
		{name: "sample", args: []string{"sample"}},
		{name: "status", args: []string{"status"}},
		{name: "report", args: []string{"report", "--format", "json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			deps := cli.Dependencies{
				DefaultDataDir: func() (string, error) { return "", errors.New(sensitive) },
			}
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), test.args, &stdout, &stderr, deps)
			if code != cli.ExitState {
				t.Fatalf("exit code = %d, want %d; stderr=%q", code, cli.ExitState, stderr.String())
			}
			if stdout.Len() != 0 || stderr.String() != "stillmac: default data directory unavailable\n" {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), sensitive) || strings.Contains(stderr.String(), "/Users/") {
				t.Fatal("default-directory failure disclosed private input")
			}
		})
	}
}

func TestInvalidCollectedSampleReturnsCollectionExitWithoutWriting(t *testing.T) {
	t.Parallel()

	invalid := validCLISample()
	invalid.Processes = nil
	writeCalled := false
	deps := cli.Dependencies{
		Collector: collectorStub{sample: invalid},
		WriteSample: func(string, observe.Sample) error {
			writeCalled = true
			return nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"sample", "--data-dir", t.TempDir()}, &stdout, &stderr, deps)
	if code != cli.ExitCollection {
		t.Fatalf("exit code = %d, want %d; stderr=%q", code, cli.ExitCollection, stderr.String())
	}
	if writeCalled {
		t.Fatal("Store.Write dependency was called for a structurally invalid collected sample")
	}
	if stdout.Len() != 0 || stderr.String() != "stillmac: sample collection failed\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSampleAppendFailureReturnsGenericStateExit(t *testing.T) {
	t.Parallel()
	const sensitive = "/Users/alice/Private Workspace/sample.json ghp_FAKE_TOKEN"
	deps := cli.Dependencies{
		Collector: collectorStub{sample: validCLISample()},
		WriteSample: func(string, observe.Sample) error {
			return errors.New(sensitive)
		},
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"sample", "--data-dir", t.TempDir()}, &stdout, &stderr, deps)
	if code != cli.ExitState {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitState)
	}
	if stdout.Len() != 0 || stderr.String() != "stillmac: local state write failed\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), sensitive) || strings.Contains(stderr.String(), "/Users/") {
		t.Fatal("append failure disclosed sensitive error content")
	}
}

func TestCLIOutputWriterFailuresUseReportExit(t *testing.T) {
	t.Parallel()

	reportDirectory := t.TempDir()
	if err := (state.Store{Directory: reportDirectory}).Write(validCLISample()); err != nil {
		t.Fatalf("seed report state: %v", err)
	}
	tests := []struct {
		name string
		args []string
		deps cli.Dependencies
	}{
		{
			name: "doctor JSON",
			args: []string{"doctor", "--data-dir", t.TempDir()},
			deps: cli.Dependencies{Doctor: readyDoctorChecker()},
		},
		{
			name: "sample JSON",
			args: []string{"sample", "--data-dir", t.TempDir()},
			deps: cli.Dependencies{
				Collector:   collectorStub{sample: validCLISample()},
				WriteSample: func(string, observe.Sample) error { return nil },
			},
		},
		{
			name: "report JSON",
			args: []string{"report", "--format", "json", "--data-dir", reportDirectory},
		},
		{
			name: "report Markdown",
			args: []string{"report", "--format", "markdown", "--data-dir", reportDirectory},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			code := cli.Run(context.Background(), test.args, outputErrorWriter{}, &stderr, test.deps)
			if code != cli.ExitReport {
				t.Fatalf("exit code = %d, want %d; stderr=%q", code, cli.ExitReport, stderr.String())
			}
			if !strings.Contains(stderr.String(), "output") {
				t.Fatalf("stderr = %q, want generic output error", stderr.String())
			}
		})
	}
}

func TestStatusBuildsStableBaselineJSONFromHistory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := (state.Store{Directory: dataDir}).Append(validCLISample()); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"status", "--data-dir", dataDir}, &stdout, &stderr, cli.Dependencies{})
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr=%q", code, cli.ExitOK, stderr.String())
	}
	var got baseline.Status
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if strings.Contains(stdout.String()+stderr.String(), dataDir) || strings.Contains(stdout.String()+stderr.String(), "sample-") {
		t.Fatalf("status output disclosed local state details: %q", stdout.String()+stderr.String())
	}
	if got.SchemaVersion != baseline.SchemaVersion || !got.ReadOnly || got.RecommendationsEnabled || got.ValidSamples != 1 || len(got.Blockers) != 3 {
		t.Fatalf("status = %#v", got)
	}
}

func TestSampleAppendsDistinctSamplesInCaptureOrder(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	first := validCLISample()
	second := validCLISample()
	second.CapturedAt = "2026-08-07T13:00:00Z"
	for _, sample := range []observe.Sample{first, second} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"sample", "--data-dir", dataDir}, &stdout, &stderr, cli.Dependencies{Collector: collectorStub{sample: sample}})
		if code != cli.ExitOK {
			t.Fatalf("sample exit = %d; stderr=%q", code, stderr.String())
		}
	}
	samples, err := (state.Store{Directory: dataDir}).ReadAll()
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(samples) != 2 || samples[0].CapturedAt != first.CapturedAt || samples[1].CapturedAt != second.CapturedAt {
		t.Fatalf("history = %#v", samples)
	}
}

func TestStatusInvalidOptionsAndUnavailableStateAreStable(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"status", "--bogus"}, &stdout, &stderr, cli.Dependencies{})
	if code != cli.ExitUsage || stderr.String() != "stillmac: invalid status options\n" {
		t.Fatalf("invalid status = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"status", "--data-dir", t.TempDir()}, &stdout, &stderr, cli.Dependencies{})
	if code != cli.ExitState || stderr.String() != "stillmac: local state unavailable\n" || stdout.Len() != 0 {
		t.Fatalf("missing status = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestStatusOutputWriterFailureUsesGenericReportExit(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	if err := (state.Store{Directory: dataDir}).Append(validCLISample()); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	var stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"status", "--data-dir", dataDir}, outputErrorWriter{}, &stderr, cli.Dependencies{})
	if code != cli.ExitReport || stderr.String() != "stillmac: unable to write output\n" {
		t.Fatalf("status writer failure = %d, stderr=%q", code, stderr.String())
	}
}

type collectorStub struct {
	sample observe.Sample
	err    error
}

func (stub collectorStub) Collect(context.Context) (observe.Sample, error) {
	return stub.sample, stub.err
}

type outputErrorWriter struct{}

func (outputErrorWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer failed")
}

func validCLISample() observe.Sample {
	return observe.Sample{
		SchemaVersion: "stillmac.sample.v1",
		CapturedAt:    "2026-08-07T12:00:00Z",
		Processes: []observe.Process{{
			Comm: "safeproc", PID: 10, PPID: 1, CPUPercent: 1, MemoryPercent: 0.5, ElapsedSeconds: 60,
		}},
		Memory: observe.Memory{Pressure: "normal", SwapUsedBytes: 0},
		Quality: observe.Quality{
			Valid: true, Status: "complete", ProcessRowsObserved: 1, ProcessRowsAccepted: 1,
			MemoryPressureAvailable: true, SwapUsedAvailable: true, Issues: []string{},
		},
	}
}

func readyDoctorChecker() doctor.Checker {
	return doctor.Checker{
		GOOS: "darwin",
		Run: func(path string, args ...string) ([]byte, error) {
			invocation := path + " " + strings.Join(args, " ")
			switch invocation {
			case "/bin/ps -p 1 -o pid=":
				return []byte("1"), nil
			case "/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level":
				return []byte("1"), nil
			case "/usr/sbin/sysctl -n vm.swapusage":
				return []byte("total = 1G used = 0G free = 1G"), nil
			default:
				return nil, errors.New("unexpected command")
			}
		},
		ProbeDirectory: func(string) error { return nil },
	}
}

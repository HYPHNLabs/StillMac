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

	"stillmac/internal/cli"
	"stillmac/internal/observe"
	"stillmac/internal/report"
	"stillmac/internal/state"
)

func TestReportIsPreliminaryAndPrivateInJSONAndMarkdown(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "StillMac")
	const hostileProcessOutput = `77 1 2.5 1.25 10:05 /Users/charlie/ClientAlpha/bin/worker --token xoxb-FAKE-SECRET --input /Users/charlie/ClientAlpha/customer-list.csv
broken fixture /Users/charlie/notes.txt --password swordfish`
	collector := observe.Collector{
		Run: func(_ context.Context, path string, args ...string) ([]byte, error) {
			invocation := path + " " + strings.Join(args, " ")
			switch invocation {
			case "/bin/ps -axo pid=,ppid=,%cpu=,%mem=,etime=,ucomm=":
				return []byte(hostileProcessOutput), nil
			case "/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level":
				return []byte("1\n"), nil
			case "/usr/sbin/sysctl -n vm.swapusage":
				return []byte("total = 1024.00M used = 0.00M free = 1024.00M\n"), nil
			default:
				return nil, errors.New("unexpected command containing xoxb-FAKE-SECRET")
			}
		},
		Now: func() time.Time {
			return time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC)
		},
	}
	deps := cli.Dependencies{Collector: collector}

	var sampleStdout, sampleStderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"sample", "--data-dir", dataDir}, &sampleStdout, &sampleStderr, deps); code != cli.ExitOK {
		t.Fatalf("sample exit = %d; stdout=%q stderr=%q", code, sampleStdout.String(), sampleStderr.String())
	}

	var jsonOutput, jsonError bytes.Buffer
	if code := cli.Run(context.Background(), []string{"report", "--format", "json", "--data-dir", dataDir}, &jsonOutput, &jsonError, cli.Dependencies{}); code != cli.ExitOK {
		t.Fatalf("JSON report exit = %d; stderr=%q", code, jsonError.String())
	}
	var got report.Report
	if err := json.Unmarshal(jsonOutput.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON report: %v\n%s", err, jsonOutput.String())
	}
	if got.SchemaVersion != "stillmac.report.v1" || !got.Preliminary || got.Status != "preliminary" {
		t.Fatalf("report header = %#v", got)
	}
	if got.Confidence.Level != "low" || got.Coverage.ValidSamples != 1 || got.Coverage.ObservedSpanSeconds != 0 {
		t.Fatalf("report confidence/coverage = %#v / %#v", got.Confidence, got.Coverage)
	}
	if !strings.Contains(got.Interpretation, "temporally associated") || !strings.Contains(got.Interpretation, "do not establish") {
		t.Fatalf("interpretation lacks temporal-only caveat: %q", got.Interpretation)
	}

	var markdownOutput, markdownError bytes.Buffer
	if code := cli.Run(context.Background(), []string{"report", "--format=markdown", "--data-dir=" + dataDir}, &markdownOutput, &markdownError, cli.Dependencies{}); code != cli.ExitOK {
		t.Fatalf("Markdown report exit = %d; stderr=%q", code, markdownError.String())
	}
	for _, required := range []string{
		"# StillMac preliminary report",
		"**Status:** Preliminary",
		"**Confidence:** Low",
		"1 valid sample",
		"Data quality: Degraded",
		"temporally associated",
		"do not establish",
		"| worker | 77 | 1 | 2.5 | 1.25 | 605 |",
	} {
		if !strings.Contains(markdownOutput.String(), required) {
			t.Fatalf("Markdown report missing %q\n%s", required, markdownOutput.String())
		}
	}

	historyDir := filepath.Join(dataDir, "samples")
	entries, err := os.ReadDir(historyDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("history entries = %v, err=%v", entries, err)
	}
	if entries[0].Name() == state.FileName {
		t.Fatalf("history entry unexpectedly uses legacy filename: %q", entries[0].Name())
	}
	stored, err := os.ReadFile(filepath.Join(historyDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read stored sample: %v", err)
	}
	allArtifacts := string(stored) + sampleStdout.String() + sampleStderr.String() +
		jsonOutput.String() + jsonError.String() + markdownOutput.String() + markdownError.String()
	for _, prohibited := range []string{
		"xoxb-FAKE-SECRET",
		"charlie",
		"ClientAlpha",
		"customer-list.csv",
		"notes.txt",
		"--token",
		"--input",
		"--password",
		"swordfish",
		"/Users/",
	} {
		if strings.Contains(allArtifacts, prohibited) {
			t.Fatalf("artifact disclosed prohibited value %q\n%s", prohibited, allArtifacts)
		}
	}

	rootEntries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("read data directory: %v", err)
	}
	if len(rootEntries) != 1 || rootEntries[0].Name() != "samples" {
		t.Fatalf("unexpected state or log files: %#v", rootEntries)
	}
}

func TestReportSelectsLatestSampleByParsedCaptureTime(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	older := validCLISample()
	older.CapturedAt = "2026-08-07T12:00:00Z"
	newer := validCLISample()
	newer.CapturedAt = "2026-08-07T14:00:00Z"
	newer.Processes[0].Comm = "latestproc"
	for _, sample := range []observe.Sample{newer, older} {
		if err := (state.Store{Directory: dataDir}).Append(sample); err != nil {
			t.Fatalf("append sample: %v", err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"report", "--format", "json", "--data-dir", dataDir}, &stdout, &stderr, cli.Dependencies{})
	if code != cli.ExitOK {
		t.Fatalf("report exit = %d; stderr=%q", code, stderr.String())
	}
	var got report.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got.Coverage.CapturedAt != newer.CapturedAt || len(got.Processes) != 1 || got.Processes[0].Comm != "latestproc" {
		t.Fatalf("report selected %#v, want latest sample %#v", got, newer)
	}
	if got.Coverage.ValidSamples != 1 || got.Coverage.ObservedSpanSeconds != 0 || !got.Preliminary || got.Confidence.Level != "low" {
		t.Fatalf("report overstated multi-sample confidence: %#v", got)
	}
}

func TestReportReadsLegacyOnlySampleWhenHistoryIsAbsent(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	legacy := validCLISample()
	legacy.CapturedAt = "2026-08-07T13:00:00Z"
	if err := (state.Store{Directory: dataDir}).Write(legacy); err != nil {
		t.Fatalf("write legacy sample: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"report", "--format", "json", "--data-dir", dataDir}, &stdout, &stderr, cli.Dependencies{})
	if code != cli.ExitOK {
		t.Fatalf("report exit = %d; stderr=%q", code, stderr.String())
	}
	var got report.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got.Coverage.CapturedAt != legacy.CapturedAt || got.Coverage.ValidSamples != 1 || got.Coverage.ObservedSpanSeconds != 0 {
		t.Fatalf("legacy report = %#v", got)
	}
}

func TestReportMissingStateUsesStablePathFreeError(t *testing.T) {
	t.Parallel()

	const privatePath = "/Users/dana/Private Workspace/secret-report.txt"
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"report", "--format", "json", "--data-dir", privatePath}, &stdout, &stderr, cli.Dependencies{})
	if code != cli.ExitState {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitState)
	}
	combined := stdout.String() + stderr.String()
	for _, prohibited := range []string{"dana", "Private Workspace", "secret-report.txt", "/Users/"} {
		if strings.Contains(combined, prohibited) {
			t.Fatalf("report error disclosed prohibited value %q: %s", prohibited, combined)
		}
	}
}

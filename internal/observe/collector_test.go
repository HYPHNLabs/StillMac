package observe

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseProcessLineKeepsTheCompleteCommandColumnPrivate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		comm string
	}{
		{
			name: "spaces in private path",
			line: "101 1 1.5 0.5 00:01 /Users/alice/Secret Workspace/Private Build/safe worker",
			comm: "safe_worker",
		},
		{
			name: "command-like suffix after spaced path",
			line: "102 1 2.5 1.5 01:02 /Users/bob/Client Project/bin/helper tool --api-key sk-test-NEVER --input /Users/bob/payroll.xlsx",
			comm: "helper_tool",
		},
		{
			name: "short option suffix",
			line: "103 1 0 0 10:00 /private/tmp/Private Workspace/agent -p swordfish",
			comm: "agent",
		},
		{
			name: "short option-like parent component",
			line: "104 1 0 0 10:01 /Users/a/Secret -Client/bin/tool",
			comm: "tool",
		},
		{
			name: "long option-like parent and spaced executable",
			line: "105 1 0 0 10:02 /Users/a/Alpha --Private/Release Candidate/bin/worker helper --token hidden",
			comm: "worker_helper",
		},
		{
			name: "multiple spaced option-like parent components",
			line: "106 1 0 0 10:03 /private/tmp/-Customer Data/Build -Confidential/bin/agent -p secret",
			comm: "agent",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			process, ok := parseProcessLine(test.line)
			if !ok {
				t.Fatal("parseProcessLine rejected a valid fixed-column row")
			}
			if process.Comm != test.comm {
				t.Fatalf("comm = %q, want %q", process.Comm, test.comm)
			}
			for _, prohibited := range []string{
				"alice", "Secret", "Workspace", "Private", "bob", "Client", "Alpha",
				"Release", "Candidate", "Customer", "Build", "Confidential", "sk-test",
				"payroll", "swordfish", "hidden", "secret",
			} {
				if strings.Contains(process.Comm, prohibited) {
					t.Fatalf("comm disclosed %q: %q", prohibited, process.Comm)
				}
			}
		})
	}
}

func TestParseElapsedAcceptedForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  int64
	}{
		{value: "00:00", want: 0},
		{value: "03:04", want: 184},
		{value: "00:00:01", want: 1},
		{value: "23:59:59", want: 86399},
		{value: "2-03:04:05", want: 183845},
		{value: "2147483647-23:59:59", want: 185542587187199},
		{value: "106751991167300-15:30:07", want: math.MaxInt64},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			got, ok := parseElapsed(test.value)
			if !ok || got != test.want {
				t.Fatalf("parseElapsed(%q) = (%d, %t), want (%d, true)", test.value, got, ok, test.want)
			}
		})
	}
}

func TestParseElapsedRejectedForms(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"2-03:04",
		"2-3:04",
		"2-03:04:05:06",
		"24:00:00",
		"00:60:00",
		"00:00:60",
		"1:2:3",
		"1:2",
		"-01:02:03",
		"1--01:02:03",
		"106751991167300-15:30:08",
		"106751991167301-00:00:00",
		"18446744073709551615-23:59:59",
	} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if got, ok := parseElapsed(value); ok {
				t.Fatalf("parseElapsed(%q) = (%d, true), want rejection", value, got)
			}
		})
	}
}

func TestParseSwapUsedValidatesTheWholeUsedToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   uint64
		ok     bool
	}{
		{name: "bytes", output: "total = 1B used = 17B free = 0B", want: 17, ok: true},
		{name: "kilobytes", output: "total = 2K used = 1.5K free = 0.5K", want: 1536, ok: true},
		{name: "megabytes", output: "total = 2.00M used = 1.25M free = 0.75M", want: 1310720, ok: true},
		{name: "gigabytes", output: "total = 2G used = 1G free = 1G (encrypted)", want: 1073741824, ok: true},
		{name: "terabytes", output: "total = 2T used = 1T free = 1T", want: 1099511627776, ok: true},
		{name: "maximum bytes", output: "total = 18446744073709551615B used = 18446744073709551615B free = 0B", want: math.MaxUint64, ok: true},
		{name: "unsupported unit", output: "total = 2PB used = 1PB free = 1PB"},
		{name: "malformed suffix", output: "total = 2M used = 1MBogus free = 1M"},
		{name: "separated unit", output: "total = 2M used = 1 M free = 1M"},
		{name: "trailing token contamination", output: "total = 2M used = 1M secret free = 1M"},
		{name: "duplicate used", output: "total = 2M used = 1M used = 2M free = 0M"},
		{name: "overflow bytes", output: "total = 18446744073709551616B used = 18446744073709551616B free = 0B"},
		{name: "overflow after scaling", output: "total = 18014398509481984K used = 18014398509481984K free = 0K"},
		{name: "negative", output: "total = 2M used = -1M free = 3M"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseSwapUsed([]byte(test.output))
			if ok != test.ok || got != test.want {
				t.Fatalf("parseSwapUsed(%q) = (%d, %t), want (%d, %t)", test.output, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestCollectorUsesUcommAndBuildsQuality(t *testing.T) {
	t.Parallel()

	runner := func(_ context.Context, path string, args ...string) ([]byte, error) {
		invocation := path + " " + strings.Join(args, " ")
		switch invocation {
		case "/bin/ps -axo pid=,ppid=,%cpu=,%mem=,etime=,ucomm=":
			return []byte("9 1 0.25 0.5 00:05 /Users/alice/Private Workspace/bin/safe worker\nbad row\n"), nil
		case "/usr/sbin/sysctl -n kern.memorystatus_vm_pressure_level":
			return []byte("4\n"), nil
		case "/usr/sbin/sysctl -n vm.swapusage":
			return []byte("total = 4G used = 1G free = 3G\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	collector := Collector{
		Run: runner,
		Now: func() time.Time { return time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC) },
	}

	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(sample.Processes) != 1 || sample.Processes[0].Comm != "safe_worker" {
		t.Fatalf("processes = %#v", sample.Processes)
	}
	if sample.Quality.Status != "degraded" || sample.Quality.ProcessRowsObserved != 2 || sample.Quality.ProcessRowsRejected != 1 {
		t.Fatalf("quality = %#v", sample.Quality)
	}
	if sample.Memory.Pressure != "critical" || sample.Memory.SwapUsedBytes != 1073741824 {
		t.Fatalf("memory = %#v", sample.Memory)
	}
}

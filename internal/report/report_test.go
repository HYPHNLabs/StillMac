package report

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"stillmac/internal/observe"
)

func TestWriteMarkdownIncludesCompleteQualityEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quality  observe.Quality
		required []string
	}{
		{
			name: "complete",
			quality: observe.Quality{
				Valid:                   true,
				Status:                  "complete",
				ProcessRowsObserved:     2,
				ProcessRowsAccepted:     2,
				ProcessRowsRejected:     0,
				MemoryPressureAvailable: true,
				SwapUsedAvailable:       true,
				Issues:                  []string{},
			},
			required: []string{
				"Captured at: 2026-08-07T12:34:56Z",
				"Process rows observed: 2",
				"Process rows accepted: 2",
				"Process rows rejected: 0",
				"Memory pressure available: true",
				"Swap used available: true",
				"Issue codes: none",
			},
		},
		{
			name: "degraded",
			quality: observe.Quality{
				Valid:                   true,
				Status:                  "degraded",
				ProcessRowsObserved:     3,
				ProcessRowsAccepted:     2,
				ProcessRowsRejected:     1,
				MemoryPressureAvailable: true,
				SwapUsedAvailable:       true,
				Issues:                  []string{"process_rows_rejected"},
			},
			required: []string{
				"Process rows observed: 3",
				"Process rows accepted: 2",
				"Process rows rejected: 1",
				"Issue codes: process_rows_rejected",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := Build(observe.Sample{
				CapturedAt: "2026-08-07T12:34:56Z",
				Processes: []observe.Process{{
					Comm:           "safeproc",
					PID:            10,
					PPID:           1,
					CPUPercent:     1.5,
					MemoryPercent:  0.5,
					ElapsedSeconds: 60,
				}},
				Memory:  observe.Memory{Pressure: "normal", SwapUsedBytes: 1024},
				Quality: test.quality,
			})
			var output bytes.Buffer
			if err := WriteMarkdown(&output, value); err != nil {
				t.Fatalf("WriteMarkdown: %v", err)
			}
			for _, required := range test.required {
				if !strings.Contains(output.String(), required) {
					t.Fatalf("Markdown missing %q\n%s", required, output.String())
				}
			}
		})
	}
}

func TestReportWritersReturnOutputErrors(t *testing.T) {
	t.Parallel()

	writer := errorWriter{}
	value := Build(observe.Sample{})
	if err := WriteJSON(writer, value); !errors.Is(err, errOutput) {
		t.Fatalf("WriteJSON error = %v, want errOutput", err)
	}
	if err := WriteMarkdown(writer, value); !errors.Is(err, errOutput) {
		t.Fatalf("WriteMarkdown error = %v, want errOutput", err)
	}
}

var errOutput = errors.New("output failed")

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errOutput
}

package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"stillmac/internal/observe"
)

const TemporalAssociationStatement = "Observed process and memory measurements are temporally associated within one collection cycle. They do not establish that any process caused memory pressure."

type Confidence struct {
	Level string `json:"level"`
	Basis string `json:"basis"`
}

type Coverage struct {
	ValidSamples        int    `json:"valid_samples"`
	ObservedSpanSeconds int64  `json:"observed_span_seconds"`
	CapturedAt          string `json:"captured_at"`
}

type Report struct {
	SchemaVersion  string            `json:"schema_version"`
	Status         string            `json:"status"`
	Preliminary    bool              `json:"preliminary"`
	Confidence     Confidence        `json:"confidence"`
	Coverage       Coverage          `json:"coverage"`
	DataQuality    observe.Quality   `json:"data_quality"`
	Memory         observe.Memory    `json:"memory"`
	Processes      []observe.Process `json:"processes"`
	Interpretation string            `json:"interpretation"`
}

func Build(sample observe.Sample) Report {
	processes := append([]observe.Process(nil), sample.Processes...)
	return Report{
		SchemaVersion: "stillmac.report.v1",
		Status:        "preliminary",
		Preliminary:   true,
		Confidence: Confidence{
			Level: "low",
			Basis: "one valid instantaneous sample cannot establish a baseline or trend",
		},
		Coverage: Coverage{
			ValidSamples:        1,
			ObservedSpanSeconds: 0,
			CapturedAt:          sample.CapturedAt,
		},
		DataQuality:    sample.Quality,
		Memory:         sample.Memory,
		Processes:      processes,
		Interpretation: TemporalAssociationStatement,
	}
}

func WriteJSON(writer io.Writer, value Report) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func WriteMarkdown(writer io.Writer, value Report) error {
	quality := title(value.DataQuality.Status)
	pressure := title(value.Memory.Pressure)
	issues := "none"
	if len(value.DataQuality.Issues) > 0 {
		issues = strings.Join(value.DataQuality.Issues, ", ")
	}
	if _, err := fmt.Fprintln(writer, "# StillMac preliminary report"); err != nil {
		return err
	}
	lines := []string{
		"",
		"**Status:** Preliminary",
		"**Confidence:** Low. One valid instantaneous sample cannot establish a baseline or trend.",
		"",
		fmt.Sprintf("Coverage: %d valid sample; %d seconds observed span.", value.Coverage.ValidSamples, value.Coverage.ObservedSpanSeconds),
		fmt.Sprintf("Captured at: %s", value.Coverage.CapturedAt),
		fmt.Sprintf("Data quality: %s (%d of %d process rows accepted).", quality, value.DataQuality.ProcessRowsAccepted, value.DataQuality.ProcessRowsObserved),
		"",
		"## Data quality evidence",
		"",
		fmt.Sprintf("- Valid: %t", value.DataQuality.Valid),
		fmt.Sprintf("- Status: %s", quality),
		fmt.Sprintf("- Process rows observed: %d", value.DataQuality.ProcessRowsObserved),
		fmt.Sprintf("- Process rows accepted: %d", value.DataQuality.ProcessRowsAccepted),
		fmt.Sprintf("- Process rows rejected: %d", value.DataQuality.ProcessRowsRejected),
		fmt.Sprintf("- Memory pressure available: %t", value.DataQuality.MemoryPressureAvailable),
		fmt.Sprintf("- Swap used available: %t", value.DataQuality.SwapUsedAvailable),
		fmt.Sprintf("- Issue codes: %s", issues),
		"",
		"## Memory observation",
		"",
		fmt.Sprintf("- Pressure condition: %s", pressure),
		fmt.Sprintf("- Swap used: %d bytes", value.Memory.SwapUsedBytes),
		"",
		"## Process observations",
		"",
		"| Comm | PID | PPID | CPU % | Memory % | Elapsed seconds |",
		"|---|---:|---:|---:|---:|---:|",
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	for _, process := range value.Processes {
		if _, err := fmt.Fprintf(
			writer,
			"| %s | %d | %d | %g | %g | %d |\n",
			process.Comm,
			process.PID,
			process.PPID,
			process.CPUPercent,
			process.MemoryPercent,
			process.ElapsedSeconds,
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(writer, "\n## Interpretation limit\n\n%s\n", value.Interpretation)
	return err
}

func title(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

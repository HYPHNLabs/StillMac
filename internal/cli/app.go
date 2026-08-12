package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"stillmac/internal/baseline"
	"stillmac/internal/doctor"
	"stillmac/internal/observe"
	"stillmac/internal/report"
	"stillmac/internal/state"
)

const (
	ExitOK         = 0
	ExitUsage      = 2
	ExitDoctor     = 3
	ExitCollection = 4
	ExitState      = 5
	ExitReport     = 6
)

type Dependencies struct {
	Doctor         doctor.Checker
	Collector      SampleCollector
	WriteSample    func(directory string, sample observe.Sample) error
	DefaultDataDir func() (string, error)
}

type SampleCollector interface {
	Collect(ctx context.Context) (observe.Sample, error)
}

var (
	errInvalidOptions = errors.New("invalid options")
	errDefaultDataDir = errors.New("default data directory unavailable")
)

type sampleResult struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	SampleValid   bool   `json:"sample_valid"`
	ProcessCount  int    `json:"process_count"`
	DataQuality   string `json:"data_quality"`
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return ExitUsage
	}

	switch args[0] {
	case "doctor":
		dataDir, err := commandDataDir(args[1:], deps.DefaultDataDir)
		if errors.Is(err, errDefaultDataDir) {
			io.WriteString(stderr, "stillmac: default data directory unavailable\n")
			return ExitState
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: invalid doctor options\n")
			return ExitUsage
		}
		result := deps.Doctor.Check(dataDir)
		if err := writeJSON(stdout, result); err != nil {
			io.WriteString(stderr, "stillmac: unable to write output\n")
			return ExitReport
		}
		if result.Status != "ready" {
			return ExitDoctor
		}
		return ExitOK
	case "sample":
		dataDir, err := commandDataDir(args[1:], deps.DefaultDataDir)
		if errors.Is(err, errDefaultDataDir) {
			io.WriteString(stderr, "stillmac: default data directory unavailable\n")
			return ExitState
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: invalid sample options\n")
			return ExitUsage
		}
		collector := deps.Collector
		if collector == nil {
			collector = observe.Collector{}
		}
		sample, err := collector.Collect(ctx)
		if err != nil || !state.ValidSample(sample) {
			io.WriteString(stderr, "stillmac: sample collection failed\n")
			return ExitCollection
		}
		writeSample := deps.WriteSample
		if writeSample == nil {
			writeSample = func(directory string, sample observe.Sample) error {
				return (state.Store{Directory: directory}).Append(sample)
			}
		}
		if err := writeSample(dataDir, sample); err != nil {
			io.WriteString(stderr, "stillmac: local state write failed\n")
			return ExitState
		}
		result := sampleResult{
			SchemaVersion: "stillmac.sample-result.v1",
			Status:        "stored",
			SampleValid:   sample.Quality.Valid,
			ProcessCount:  len(sample.Processes),
			DataQuality:   sample.Quality.Status,
		}
		if err := writeJSON(stdout, result); err != nil {
			io.WriteString(stderr, "stillmac: unable to write output\n")
			return ExitReport
		}
		return ExitOK
	case "status":
		dataDir, err := commandDataDir(args[1:], deps.DefaultDataDir)
		if errors.Is(err, errDefaultDataDir) {
			io.WriteString(stderr, "stillmac: default data directory unavailable\n")
			return ExitState
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: invalid status options\n")
			return ExitUsage
		}
		samples, err := (state.Store{Directory: dataDir}).ReadAll()
		if err != nil {
			io.WriteString(stderr, "stillmac: local state unavailable\n")
			return ExitState
		}
		result, err := baseline.Build(samples)
		if err != nil {
			io.WriteString(stderr, "stillmac: local state unavailable\n")
			return ExitState
		}
		if err := writeJSON(stdout, result); err != nil {
			io.WriteString(stderr, "stillmac: unable to write output\n")
			return ExitReport
		}
		return ExitOK
	case "report":
		dataDir, format, err := reportOptions(args[1:], deps.DefaultDataDir)
		if errors.Is(err, errDefaultDataDir) {
			io.WriteString(stderr, "stillmac: default data directory unavailable\n")
			return ExitState
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: invalid report options\n")
			return ExitUsage
		}
		samples, err := (state.Store{Directory: dataDir}).ReadAll()
		if err != nil || len(samples) == 0 {
			io.WriteString(stderr, "stillmac: local state unavailable\n")
			return ExitState
		}
		value := report.Build(samples[len(samples)-1])
		if format == "json" {
			err = report.WriteJSON(stdout, value)
		} else {
			err = report.WriteMarkdown(stdout, value)
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: report output failed\n")
			return ExitReport
		}
		return ExitOK
	case "help", "--help", "-h":
		writeUsage(stdout)
		return ExitOK
	default:
		io.WriteString(stderr, "stillmac: invalid command\n")
		writeUsage(stderr)
		return ExitUsage
	}
}

func reportOptions(args []string, defaultDataDir func() (string, error)) (string, string, error) {
	format := "markdown"
	dataArgs := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--format":
			index++
			if index >= len(args) {
				return "", "", errInvalidOptions
			}
			format = args[index]
		case strings.HasPrefix(arg, "--format="):
			format = strings.TrimPrefix(arg, "--format=")
		default:
			dataArgs = append(dataArgs, arg)
		}
	}
	if format != "json" && format != "markdown" {
		return "", "", errInvalidOptions
	}
	dataDir, err := commandDataDir(dataArgs, defaultDataDir)
	return dataDir, format, err
}

func commandDataDir(args []string, defaultDataDir func() (string, error)) (string, error) {
	var dataDir string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--data-dir":
			index++
			if index >= len(args) || args[index] == "" {
				return "", errInvalidOptions
			}
			dataDir = args[index]
		case strings.HasPrefix(arg, "--data-dir="):
			dataDir = strings.TrimPrefix(arg, "--data-dir=")
			if dataDir == "" {
				return "", errInvalidOptions
			}
		default:
			return "", errInvalidOptions
		}
	}
	if dataDir != "" {
		return dataDir, nil
	}
	if defaultDataDir == nil {
		defaultDataDir = DefaultDataDir
	}
	path, err := defaultDataDir()
	if err != nil || path == "" {
		return "", errDefaultDataDir
	}
	return path, nil
}

func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("home directory unavailable")
	}
	return filepath.Join(home, "Library", "Application Support", "StillMac"), nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	return encoder.Encode(value)
}

func writeUsage(writer io.Writer) {
	io.WriteString(writer, "usage: stillmac <doctor|sample|status|report> [options]\n")
}

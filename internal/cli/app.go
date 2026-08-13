package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stillmac/internal/baseline"
	"stillmac/internal/cleanup"
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
	CleanupHome    func() (string, error)
	CleanupHostID  func() string
	Now            func() time.Time
	Stdin          io.Reader
	IsTTY          func() bool
	GitRunner      cleanup.GitRunner
	GoCleaner      cleanup.GoCleaner
	CleanupFactory func(cleanup.Config) CleanupService
}

type CleanupService interface {
	Scan(scope string) ([]cleanup.Candidate, error)
	Plan(items []cleanup.Candidate, ids []string) (cleanup.Plan, error)
	Apply(id string) (cleanup.ApplyResult, error)
	Protect(scope, id string) error
	History() ([]cleanup.Receipt, error)
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
	case "scan":
		opts, err := parseCleanupOptions(args[1:], true, false, true)
		if err != nil || len(opts.positionals) != 0 {
			io.WriteString(stderr, "stillmac: invalid scan options\n")
			return ExitUsage
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			io.WriteString(stderr, "stillmac: scan unavailable\n")
			return ExitState
		}
		items, err := engine.Scan(opts.scope)
		if err != nil {
			io.WriteString(stderr, "stillmac: scan failed\n")
			return ExitState
		}
		if opts.format == "json" {
			if err := writeJSON(stdout, items); err != nil {
				io.WriteString(stderr, "stillmac: unable to write output\n")
				return ExitReport
			}
		} else {
			if err := writeCandidatesText(stdout, items); err != nil {
				io.WriteString(stderr, "stillmac: unable to write output\n")
				return ExitReport
			}
		}
		return ExitOK
	case "plan":
		opts, err := parseCleanupOptions(args[1:], true, true, true)
		if err != nil || len(opts.positionals) == 0 {
			io.WriteString(stderr, "stillmac: invalid plan options\n")
			return ExitUsage
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			io.WriteString(stderr, "stillmac: plan unavailable\n")
			return ExitState
		}
		items, err := engine.Scan(opts.scope)
		if err != nil {
			io.WriteString(stderr, "stillmac: plan failed\n")
			return ExitState
		}
		ids := opts.positionals
		if len(ids) == 1 && ids[0] == "all" {
			ids = []string{"all-safe"}
		}
		p, err := engine.Plan(items, ids)
		if err != nil {
			io.WriteString(stderr, "stillmac: plan failed\n")
			return ExitState
		}
		if opts.format == "json" {
			err = writeJSON(stdout, p)
		} else {
			err = writePlanText(stdout, p)
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: unable to write output\n")
			return ExitReport
		}
		return ExitOK
	case "apply":
		opts, err := parseCleanupOptions(args[1:], false, true, true)
		if err != nil || len(opts.positionals) != 1 {
			io.WriteString(stderr, "stillmac: invalid apply options\n")
			return ExitUsage
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			io.WriteString(stderr, "stillmac: apply unavailable\n")
			return ExitState
		}
		result, applyErr := engine.Apply(opts.positionals[0])
		if opts.format == "json" {
			err = writeJSON(stdout, result)
		} else {
			err = writeApplyText(stdout, result)
		}
		if err != nil {
			io.WriteString(stderr, "stillmac: unable to write output\n")
			return ExitReport
		}
		if applyErr != nil {
			io.WriteString(stderr, "stillmac: apply failed closed\n")
			return ExitState
		}
		return ExitOK
	case "explain":
		opts, err := parseCleanupOptions(args[1:], true, false, true)
		if err != nil || len(opts.positionals) != 1 {
			io.WriteString(stderr, "stillmac: invalid explain options\n")
			return ExitUsage
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			return ExitState
		}
		items, err := engine.Scan(opts.scope)
		if err != nil {
			return ExitState
		}
		for _, c := range items {
			if c.ID == opts.positionals[0] {
				if opts.format == "json" {
					err = writeJSON(stdout, c)
				} else {
					err = writeCandidateText(stdout, c)
				}
				if err != nil {
					return ExitReport
				}
				return ExitOK
			}
		}
		io.WriteString(stderr, "stillmac: unknown candidate\n")
		return ExitState
	case "protect":
		opts, err := parseCleanupOptions(args[1:], true, true, false)
		if err != nil || len(opts.positionals) != 1 {
			io.WriteString(stderr, "stillmac: invalid protect options\n")
			return ExitUsage
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			return ExitState
		}
		if err = engine.Protect(opts.scope, opts.positionals[0]); err != nil {
			io.WriteString(stderr, "stillmac: protect failed\n")
			return ExitState
		}
		if _, err = fmt.Fprintf(stdout, "protected %s\n", opts.positionals[0]); err != nil {
			return ExitReport
		}
		return ExitOK
	case "history":
		opts, err := parseCleanupOptions(args[1:], false, true, true)
		if err != nil || len(opts.positionals) != 0 {
			return ExitUsage
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			return ExitState
		}
		receipts, err := engine.History()
		if err != nil {
			io.WriteString(stderr, "stillmac: history failed closed\n")
			return ExitState
		}
		if opts.format == "json" {
			err = writeJSON(stdout, receipts)
		} else {
			err = writeReceiptsText(stdout, receipts)
		}
		if err != nil {
			return ExitReport
		}
		return ExitOK
	case "clean":
		opts, err := parseCleanupOptions(args[1:], true, true, false)
		if err != nil {
			return ExitUsage
		}
		isTTY := deps.IsTTY
		if isTTY == nil {
			isTTY = func() bool {
				i, e := os.Stdin.Stat()
				return e == nil && i.Mode()&os.ModeCharDevice != 0
			}
		}
		if !isTTY() {
			io.WriteString(stderr, "stillmac: clean refuses non-TTY input; use plan then apply\n")
			return ExitUsage
		}
		ids := opts.positionals
		if len(ids) == 0 || len(ids) == 1 && ids[0] == "all" {
			ids = []string{"all-safe"}
		}
		engine, err := cleanupService(deps, opts.dataDir)
		if err != nil {
			return ExitState
		}
		items, err := engine.Scan(opts.scope)
		if err != nil {
			return ExitState
		}
		if len(opts.positionals) > 0 && !(len(opts.positionals) == 1 && opts.positionals[0] == "all") {
			ids, err = mapDisplayedSelections(items, opts.positionals)
			if err != nil {
				return ExitUsage
			}
		}
		if err = writeCandidatesText(stdout, items); err != nil {
			return ExitReport
		}
		p, err := engine.Plan(items, ids)
		if err != nil {
			io.WriteString(stderr, "stillmac: clean plan failed\n")
			return ExitState
		}
		if err = writePlanText(stdout, p); err != nil {
			return ExitReport
		}
		if _, err = fmt.Fprintf(stdout, "type exactly: apply %s\n", p.ID); err != nil {
			return ExitReport
		}
		stdin := deps.Stdin
		if stdin == nil {
			stdin = os.Stdin
		}
		line, readErr := bufio.NewReader(stdin).ReadString('\n')
		if readErr != nil && len(line) == 0 {
			io.WriteString(stderr, "stillmac: confirmation not received\n")
			return ExitUsage
		}
		if strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "apply "+p.ID {
			io.WriteString(stderr, "stillmac: confirmation did not match; nothing changed\n")
			return ExitUsage
		}
		result, applyErr := engine.Apply(p.ID)
		if err = writeApplyText(stdout, result); err != nil {
			return ExitReport
		}
		if applyErr != nil {
			io.WriteString(stderr, "stillmac: clean apply failed closed\n")
			return ExitState
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

type cleanupOptions struct {
	positionals []string
	scope       string
	dataDir     string
	format      string
}

func parseCleanupOptions(args []string, allowScope, allowData, allowFormat bool) (cleanupOptions, error) {
	o := cleanupOptions{format: "text"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := arg, "", false
		if at := strings.IndexByte(arg, '='); at >= 0 {
			name, value, hasValue = arg[:at], arg[at+1:], true
		}
		if !strings.HasPrefix(name, "--") {
			o.positionals = append(o.positionals, arg)
			continue
		}
		if !hasValue {
			i++
			if i >= len(args) {
				return o, errInvalidOptions
			}
			value = args[i]
		}
		if value == "" {
			return o, errInvalidOptions
		}
		switch name {
		case "--scope":
			if !allowScope {
				return o, errInvalidOptions
			}
			o.scope = value
		case "--data-dir":
			if !allowData {
				return o, errInvalidOptions
			}
			o.dataDir = value
		case "--format":
			if !allowFormat {
				return o, errInvalidOptions
			}
			o.format = value
		default:
			return o, errInvalidOptions
		}
	}
	if o.format != "text" && o.format != "json" {
		return o, errInvalidOptions
	}
	return o, nil
}

func cleanupService(deps Dependencies, dataDir string) (CleanupService, error) {
	if dataDir == "" {
		var err error
		dataDir, err = commandDataDir(nil, deps.DefaultDataDir)
		if err != nil {
			return nil, err
		}
	}
	homeFn := deps.CleanupHome
	if homeFn == nil {
		homeFn = os.UserHomeDir
	}
	home, err := homeFn()
	if err != nil || home == "" {
		return nil, errDefaultDataDir
	}
	host := ""
	if deps.CleanupHostID != nil {
		host = deps.CleanupHostID()
	}
	config := cleanup.Config{Home: home, DataDir: dataDir, HostID: host, Now: deps.Now, GitRunner: deps.GitRunner, GoCleaner: deps.GoCleaner}
	if deps.CleanupFactory != nil {
		return deps.CleanupFactory(config), nil
	}
	return &cleanup.Engine{Config: config}, nil
}

func writeCandidateText(w io.Writer, c cleanup.Candidate) error {
	_, err := fmt.Fprintf(w, "%s %s %s %d bytes: %s\n", c.ID, c.Decision, c.Label, c.Bytes, strings.Join(c.Reasons, "; "))
	return err
}
func writeCandidatesText(w io.Writer, items []cleanup.Candidate) error {
	for i, c := range items {
		if _, err := fmt.Fprintf(w, "%d. ", i+1); err != nil {
			return err
		}
		if err := writeCandidateText(w, c); err != nil {
			return err
		}
	}
	return nil
}
func mapDisplayedSelections(items []cleanup.Candidate, selections []string) ([]string, error) {
	byNumber := make(map[string]string, len(items))
	for i, c := range items {
		byNumber[fmt.Sprintf("%d", i+1)] = c.ID
	}
	out := make([]string, 0, len(selections))
	for _, s := range selections {
		if id, ok := byNumber[s]; ok {
			out = append(out, id)
		} else {
			out = append(out, s)
		}
	}
	return out, nil
}
func writePlanText(w io.Writer, p cleanup.Plan) error {
	if _, err := fmt.Fprintf(w, "plan %s expires %s\n", p.ID, p.ExpiresAt); err != nil {
		return err
	}
	for _, c := range p.Candidates {
		if _, err := fmt.Fprintf(w, "include %s SAFE %s\n", c.ID, c.Label); err != nil {
			return err
		}
	}
	for _, c := range p.Excluded {
		if _, err := fmt.Fprintf(w, "excluded %s %s %s\n", c.ID, c.Decision, c.Label); err != nil {
			return err
		}
	}
	return nil
}
func writeApplyText(w io.Writer, r cleanup.ApplyResult) error {
	if _, err := io.WriteString(w, "warning: approval authorizes go clean -cache for the exact logical GOCACHE pathname; concurrent same-account hostile replacement is outside StillMac's protection boundary\n"); err != nil {
		return err
	}
	for _, row := range r.Rows {
		if _, err := fmt.Fprintf(w, "%s %s before=%d after=%d removed=%d reclaimed=%d method=%s\n", row.CandidateID, row.Result, row.BeforeBytes, row.AfterBytes, row.RemovedBytes, row.ReclaimedBytes, row.Method); err != nil {
			return err
		}
	}
	return nil
}
func writeReceiptsText(w io.Writer, rows []cleanup.Receipt) error {
	if len(rows) == 0 {
		_, err := io.WriteString(w, "no cleanup receipts\n")
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(w, "%s %s removed=%d reclaimed=%d at=%s\n", row.CandidateID, row.Result, row.RemovedBytes, row.ReclaimedBytes, row.Timestamp); err != nil {
			return err
		}
	}
	return nil
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
	io.WriteString(writer, `usage: stillmac <doctor|sample|status|report> [options]
       stillmac scan [--scope PATH] [--format text|json]
       stillmac explain ID [--scope PATH] [--format text|json]
       stillmac plan ID... | plan all-safe [--scope PATH] [--data-dir PATH] [--format text|json]
       stillmac apply PLAN_ID [--data-dir PATH] [--format text|json]
       stillmac clean [IDs...|all] [--scope PATH] [--data-dir PATH]
       stillmac protect ID [--scope PATH] [--data-dir PATH]
       stillmac history [--data-dir PATH] [--format text|json]
`)
}

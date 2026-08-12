package observe

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"math"
	"math/big"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	processCommandPath = "/bin/ps"
	memoryCommandPath  = "/usr/sbin/sysctl"
)

var (
	ErrProcessCollection = errors.New("process collection failed")
	ErrMemoryCollection  = errors.New("memory collection failed")
	commandSuffixPattern = regexp.MustCompile(`\s+--?[A-Za-z0-9]`)
	swapUsagePattern     = regexp.MustCompile(`^\s*(?:vm\.swapusage:\s*)?total\s*=\s*(\S+)\s+used\s*=\s*(\S+)\s+free\s*=\s*(\S+)(?:\s+\(encrypted\))?\s*$`)
	byteQuantityPattern  = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)([KMGT]?)(?:B)?$`)
)

type Runner func(ctx context.Context, path string, args ...string) ([]byte, error)

type Process struct {
	Comm           string  `json:"comm"`
	PID            int     `json:"pid"`
	PPID           int     `json:"ppid"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryPercent  float64 `json:"memory_percent"`
	ElapsedSeconds int64   `json:"elapsed_seconds"`
}

type Memory struct {
	Pressure      string `json:"pressure"`
	SwapUsedBytes uint64 `json:"swap_used_bytes"`
}

type Quality struct {
	Valid                   bool     `json:"valid"`
	Status                  string   `json:"status"`
	ProcessRowsObserved     int      `json:"process_rows_observed"`
	ProcessRowsAccepted     int      `json:"process_rows_accepted"`
	ProcessRowsRejected     int      `json:"process_rows_rejected"`
	MemoryPressureAvailable bool     `json:"memory_pressure_available"`
	SwapUsedAvailable       bool     `json:"swap_used_available"`
	Issues                  []string `json:"issues"`
}

type Sample struct {
	SchemaVersion string    `json:"schema_version"`
	CapturedAt    string    `json:"captured_at"`
	Processes     []Process `json:"processes"`
	Memory        Memory    `json:"memory"`
	Quality       Quality   `json:"quality"`
}

type Collector struct {
	Run Runner
	Now func() time.Time
}

func (c Collector) Collect(ctx context.Context) (Sample, error) {
	run := c.Run
	if run == nil {
		run = runNativeCommand
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}

	processOutput, err := run(ctx, processCommandPath, "-axo", "pid=,ppid=,%cpu=,%mem=,etime=,ucomm=")
	if err != nil {
		return Sample{}, ErrProcessCollection
	}
	processes, observed, rejected, err := parseProcesses(processOutput)
	if err != nil || len(processes) == 0 {
		return Sample{}, ErrProcessCollection
	}

	pressureOutput, err := run(ctx, memoryCommandPath, "-n", "kern.memorystatus_vm_pressure_level")
	if err != nil {
		return Sample{}, ErrMemoryCollection
	}
	pressure, ok := parsePressure(pressureOutput)
	if !ok {
		return Sample{}, ErrMemoryCollection
	}

	swapOutput, err := run(ctx, memoryCommandPath, "-n", "vm.swapusage")
	if err != nil {
		return Sample{}, ErrMemoryCollection
	}
	swapUsed, ok := parseSwapUsed(swapOutput)
	if !ok {
		return Sample{}, ErrMemoryCollection
	}

	issues := make([]string, 0, 1)
	status := "complete"
	if rejected > 0 {
		status = "degraded"
		issues = append(issues, "process_rows_rejected")
	}

	return Sample{
		SchemaVersion: "stillmac.sample.v1",
		CapturedAt:    now().UTC().Format(time.RFC3339Nano),
		Processes:     processes,
		Memory: Memory{
			Pressure:      pressure,
			SwapUsedBytes: swapUsed,
		},
		Quality: Quality{
			Valid:                   true,
			Status:                  status,
			ProcessRowsObserved:     observed,
			ProcessRowsAccepted:     len(processes),
			ProcessRowsRejected:     rejected,
			MemoryPressureAvailable: true,
			SwapUsedAvailable:       true,
			Issues:                  issues,
		},
	}, nil
}

func runNativeCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, path, args...).Output()
}

func parseProcesses(output []byte) ([]Process, int, int, error) {
	processes := make([]Process, 0)
	observed := 0
	rejected := 0
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		observed++
		process, ok := parseProcessLine(line)
		if !ok {
			rejected++
			continue
		}
		processes = append(processes, process)
	}
	if err := scanner.Err(); err != nil {
		return nil, observed, rejected, ErrProcessCollection
	}

	sort.Slice(processes, func(i, j int) bool {
		if processes[i].PID != processes[j].PID {
			return processes[i].PID < processes[j].PID
		}
		if processes[i].PPID != processes[j].PPID {
			return processes[i].PPID < processes[j].PPID
		}
		return processes[i].Comm < processes[j].Comm
	})
	return processes, observed, rejected, nil
}

func parseProcessLine(line string) (Process, bool) {
	fields, commandColumn, ok := splitFixedColumns(line, 5)
	if !ok {
		return Process{}, false
	}

	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return Process{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid < 0 {
		return Process{}, false
	}
	cpuPercent, ok := parseNonNegativeFloat(fields[2])
	if !ok {
		return Process{}, false
	}
	memoryPercent, ok := parseNonNegativeFloat(fields[3])
	if !ok || memoryPercent > 100 {
		return Process{}, false
	}
	elapsedSeconds, ok := parseElapsed(fields[4])
	if !ok {
		return Process{}, false
	}
	comm := safeComm(commandColumn)
	if comm == "" {
		return Process{}, false
	}

	return Process{
		Comm:           comm,
		PID:            pid,
		PPID:           ppid,
		CPUPercent:     cpuPercent,
		MemoryPercent:  memoryPercent,
		ElapsedSeconds: elapsedSeconds,
	}, true
}

func splitFixedColumns(line string, count int) ([]string, string, bool) {
	fields := make([]string, 0, count)
	position := 0
	for len(fields) < count {
		for position < len(line) && isASCIISpace(line[position]) {
			position++
		}
		start := position
		for position < len(line) && !isASCIISpace(line[position]) {
			position++
		}
		if start == position {
			return nil, "", false
		}
		fields = append(fields, line[start:position])
	}
	remaining := strings.TrimSpace(line[position:])
	return fields, remaining, remaining != ""
}

func isASCIISpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func parseNonNegativeFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil && parsed >= 0 && !math.IsInf(parsed, 0) && !math.IsNaN(parsed)
}

func parseElapsed(value string) (int64, bool) {
	var days, hours, minutes, seconds uint64
	dayParts := strings.Split(value, "-")
	if len(dayParts) > 2 {
		return 0, false
	}
	timePart := dayParts[len(dayParts)-1]
	parts := strings.Split(timePart, ":")
	if len(dayParts) == 2 {
		if dayParts[0] == "" || len(parts) != 3 {
			return 0, false
		}
		parsedDays, err := strconv.ParseUint(dayParts[0], 10, 64)
		if err != nil {
			return 0, false
		}
		days = parsedDays
	} else if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return 0, false
		}
	}
	values := make([]uint64, len(parts))
	for index, part := range parts {
		parsed, err := strconv.ParseUint(part, 10, 8)
		if err != nil {
			return 0, false
		}
		values[index] = parsed
	}
	if len(parts) == 2 {
		minutes, seconds = values[0], values[1]
	} else {
		hours, minutes, seconds = values[0], values[1], values[2]
	}
	if hours > 23 || minutes > 59 || seconds > 59 {
		return 0, false
	}

	const secondsPerDay = uint64(24 * 60 * 60)
	maxSeconds := uint64(math.MaxInt64)
	if days > maxSeconds/secondsPerDay {
		return 0, false
	}
	total := days * secondsPerDay
	timeSeconds := hours*60*60 + minutes*60 + seconds
	if timeSeconds > maxSeconds-total {
		return 0, false
	}
	total += timeSeconds
	return int64(total), true
}

func safeComm(value string) string {
	value = strings.TrimSpace(value)
	name := accountingBasename(value)
	if suffix := commandSuffixPattern.FindStringIndex(name); suffix != nil {
		name = strings.TrimSpace(name[:suffix[0]])
	}
	if name == "." || name == string(filepath.Separator) || name == "" {
		return ""
	}
	var builder strings.Builder
	for _, char := range name {
		if builder.Len() >= 64 {
			break
		}
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("._+-", char) {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func accountingBasename(value string) string {
	pathEnd := len(value)
	for _, suffix := range commandSuffixPattern.FindAllStringIndex(value, -1) {
		tokenStart := suffix[0]
		for tokenStart < len(value) && isASCIISpace(value[tokenStart]) {
			tokenStart++
		}
		tokenEnd := tokenStart
		for tokenEnd < len(value) && !isASCIISpace(value[tokenEnd]) {
			tokenEnd++
		}
		if !strings.ContainsRune(value[tokenStart:tokenEnd], filepath.Separator) {
			pathEnd = suffix[0]
			break
		}
	}
	separator := strings.LastIndexByte(value[:pathEnd], filepath.Separator)
	return value[separator+1:]
}

func parsePressure(output []byte) (string, bool) {
	switch strings.TrimSpace(string(output)) {
	case "1":
		return "normal", true
	case "2":
		return "warning", true
	case "4":
		return "critical", true
	default:
		return "", false
	}
}

// ParsePressureOutput recognises the fixed native pressure shape without
// retaining or exposing the native output.
func ParsePressureOutput(output []byte) (string, bool) {
	return parsePressure(output)
}

func parseSwapUsed(output []byte) (uint64, bool) {
	matches := swapUsagePattern.FindSubmatch(output)
	if len(matches) != 4 {
		return 0, false
	}
	for _, token := range matches[1:] {
		if _, ok := parseByteQuantity(string(token)); !ok {
			return 0, false
		}
	}
	return parseByteQuantity(string(matches[2]))
}

// ParseSwapUsedOutput recognises the fixed native swap shape without retaining
// or exposing the native output.
func ParseSwapUsedOutput(output []byte) (uint64, bool) {
	return parseSwapUsed(output)
}

func parseByteQuantity(token string) (uint64, bool) {
	matches := byteQuantityPattern.FindStringSubmatch(token)
	if len(matches) != 3 {
		return 0, false
	}
	amount, ok := new(big.Rat).SetString(matches[1])
	if !ok || amount.Sign() < 0 {
		return 0, false
	}
	multiplier := uint64(1)
	switch string(matches[2]) {
	case "K":
		multiplier = 1024
	case "M":
		multiplier = 1024 * 1024
	case "G":
		multiplier = 1024 * 1024 * 1024
	case "T":
		multiplier = 1024 * 1024 * 1024 * 1024
	}
	amount.Mul(amount, new(big.Rat).SetInt(new(big.Int).SetUint64(multiplier)))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(amount.Num(), amount.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(amount.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return 0, false
	}
	return quotient.Uint64(), true
}

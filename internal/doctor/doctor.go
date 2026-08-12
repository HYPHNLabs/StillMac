package doctor

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"stillmac/internal/observe"
)

const (
	processCommandPath = "/bin/ps"
	memoryCommandPath  = "/usr/sbin/sysctl"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Result struct {
	SchemaVersion string  `json:"schema_version"`
	Status        string  `json:"status"`
	Checks        []Check `json:"checks"`
}

type Runner func(path string, args ...string) ([]byte, error)

type Checker struct {
	GOOS           string
	Run            Runner
	ProbeDirectory func(path string) error
}

func (c Checker) Check(dataDir string) Result {
	goos := c.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	run := c.Run
	if run == nil {
		run = runNativeCommand
	}
	probeDirectory := c.ProbeDirectory
	if probeDirectory == nil {
		probeDirectory = ProbeDirectory
	}

	psOutput, psErr := run(processCommandPath, "-p", "1", "-o", "pid=")
	pressureOutput, pressureErr := run(memoryCommandPath, "-n", "kern.memorystatus_vm_pressure_level")
	swapOutput, swapErr := run(memoryCommandPath, "-n", "vm.swapusage")
	_, pressureRecognised := observe.ParsePressureOutput(pressureOutput)
	_, swapRecognised := observe.ParseSwapUsedOutput(swapOutput)

	checks := []Check{
		check(
			"operating_system",
			goos == "darwin",
			"macOS detected",
			"StillMac v0.1 requires macOS",
		),
		check(
			"native_command:ps",
			psErr == nil && recognisedPSProbe(psOutput),
			"required native probe succeeded",
			"required native probe failed",
		),
		check(
			"native_command:sysctl",
			pressureErr == nil && pressureRecognised && swapErr == nil && swapRecognised,
			"required memory probes succeeded",
			"required memory probe failed",
		),
	}

	directoryReady := dataDir != "" && probeDirectory(dataDir) == nil
	checks = append(checks, check(
		"data_directory",
		directoryReady,
		"local data directory is writable",
		"local data directory is not writable",
	))

	status := "ready"
	for _, item := range checks {
		if item.Status != "pass" {
			status = "not_ready"
			break
		}
	}

	return Result{
		SchemaVersion: "stillmac.doctor.v1",
		Status:        status,
		Checks:        checks,
	}
}

func ProbeDirectory(path string) error {
	if path == "" {
		return errors.New("empty data directory")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("create data directory")
	}
	fileDescriptor, err := syscall.Open(
		path,
		syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY|syscall.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return errors.New("data directory unavailable")
	}
	directory := os.NewFile(uintptr(fileDescriptor), path)
	if directory == nil {
		_ = syscall.Close(fileDescriptor)
		return errors.New("data directory unavailable")
	}
	defer directory.Close()
	if !sameDirectory(directory, path) {
		return errors.New("data directory unavailable")
	}
	if !safeStatePath(path) {
		return errors.New("state path is unsafe")
	}

	probe, err := os.CreateTemp(path, ".stillmac-write-probe-")
	if err != nil {
		return errors.New("create data directory probe")
	}
	probeName := probe.Name()
	defer os.Remove(probeName)
	if err := probe.Chmod(0o600); err != nil {
		probe.Close()
		return errors.New("secure data directory probe")
	}
	if err := probe.Close(); err != nil {
		return errors.New("close data directory probe")
	}
	if !sameDirectory(directory, path) || !safeStatePath(path) {
		return errors.New("data directory changed")
	}
	return nil
}

func safeStatePath(directory string) bool {
	info, err := os.Lstat(filepath.Join(directory, "current-sample.json"))
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return err == nil && info.Mode().IsRegular()
}

func runNativeCommand(path string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	command := exec.Command(path, args...)
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func recognisedPSProbe(output []byte) bool {
	value := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(value)
	return err == nil && pid > 0 && !strings.ContainsAny(value, " \t\r\n")
}

func sameDirectory(directory *os.File, path string) bool {
	openedInfo, err := directory.Stat()
	if err != nil || !openedInfo.IsDir() {
		return false
	}
	pathInfo, err := os.Lstat(path)
	return err == nil && pathInfo.IsDir() && os.SameFile(openedInfo, pathInfo)
}

func check(name string, passed bool, passDetail, failDetail string) Check {
	if passed {
		return Check{Name: name, Status: "pass", Detail: passDetail}
	}
	return Check{Name: name, Status: "fail", Detail: failDetail}
}

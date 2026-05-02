package modulehelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

const (
	StatusActive         = "active"
	StatusPendingUpdate  = "pending_update"
	StatusPendingRemove  = "pending_remove"
	StatusDisabled       = "disabled"
	StatusMissing        = "missing"
	StatusServiceMissing = "service_missing"
	StatusDaemonctlMiss  = "daemonctl_missing"
	StatusDaemonMissing  = "daemon_missing"
	StatusConfigMissing  = "config_missing"
	StatusMissingProfile = "missing_profile"
	StatusInvalidProfile = "invalid_profile"

	RepairIdle      = "repair_idle"
	RepairStarted   = "repair_started"
	RepairRunning   = "repair_running"
	RepairSucceeded = "repair_succeeded"
	RepairFailed    = "repair_failed"
)

type State struct {
	Status        string `json:"status"`
	ModuleDir     string `json:"moduleDir"`
	ProfilePath   string `json:"profilePath,omitempty"`
	ProfileNodes  int    `json:"profileNodes,omitempty"`
	DaemonPID     int    `json:"daemonPid,omitempty"`
	DaemonAlive   bool   `json:"daemonAlive"`
	SocketPresent bool   `json:"socketPresent"`
	RepairStatus  string `json:"repairStatus,omitempty"`
	Message       string `json:"message,omitempty"`
}

type RepairResult struct {
	Status  string `json:"status"`
	PID     int    `json:"pid,omitempty"`
	Message string `json:"message,omitempty"`
}

type RepairStatus struct {
	Status     string `json:"status"`
	PID        int    `json:"pid,omitempty"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Message    string `json:"message,omitempty"`
}

func CurrentState(moduleDir string) State {
	paths := modulecontract.NewPaths(moduleDir)
	state := State{
		Status:       StatusActive,
		ModuleDir:    paths.Dir(),
		ProfilePath:  profile.Path(filepath.Join(paths.ConfigDir(), "config.json")),
		RepairStatus: CurrentRepairStatus(moduleDir).Status,
	}

	if hasPendingUpdate(paths.Dir()) {
		state.Status = StatusPendingUpdate
		state.Message = "module update is staged; reboot required"
		return state
	}
	if exists(filepath.Join(paths.Dir(), "remove")) {
		state.Status = StatusPendingRemove
		state.Message = "module is marked for removal; reboot required"
		return state
	}
	if exists(filepath.Join(paths.Dir(), "disable")) {
		state.Status = StatusDisabled
		state.Message = "module is disabled"
		return state
	}
	if !exists(paths.Dir()) {
		state.Status = StatusMissing
		state.Message = "module directory is missing"
		return state
	}
	if !isExecutable(filepath.Join(paths.Dir(), "service.sh")) {
		state.Status = StatusServiceMissing
		state.Message = "service.sh is missing or not executable"
		return state
	}
	if !isExecutable(filepath.Join(paths.BinDir(), "daemonctl")) {
		state.Status = StatusDaemonctlMiss
		state.Message = "daemonctl is missing or not executable"
		return state
	}
	if !isExecutable(filepath.Join(paths.BinDir(), "daemon")) {
		state.Status = StatusDaemonMissing
		state.Message = "daemon is missing or not executable"
		return state
	}
	if !exists(filepath.Join(paths.ConfigDir(), "config.json")) {
		state.Status = StatusConfigMissing
		state.Message = "config.json is missing"
		return state
	}

	state.SocketPresent = exists(paths.DaemonSocket())
	if pid := readPID(paths.DaemonPIDFile()); pid > 0 {
		state.DaemonPID = pid
		state.DaemonAlive = processMatchesPath(pid, filepath.Join(paths.BinDir(), "daemon"))
	}

	doc, found, err := profile.Load(state.ProfilePath)
	if err != nil {
		state.Status = StatusInvalidProfile
		state.Message = err.Error()
		return state
	}
	if !found {
		state.Status = StatusMissingProfile
		state.Message = "profile.json is missing"
		return state
	}
	state.ProfileNodes = len(doc.Nodes)
	return state
}

func StartRepair(moduleDir string) RepairResult {
	paths := modulecontract.NewPaths(moduleDir)
	status := CurrentRepairStatus(paths.Dir())
	if status.Status == RepairRunning {
		return RepairResult{Status: RepairRunning, PID: status.PID, Message: "module repair is already running"}
	}

	state := CurrentState(paths.Dir())
	switch state.Status {
	case StatusPendingUpdate, StatusPendingRemove, StatusDisabled, StatusMissing, StatusServiceMissing:
		return RepairResult{Status: state.Status, Message: state.Message}
	}

	servicePath := filepath.Join(paths.Dir(), "service.sh")
	if !isExecutable(servicePath) {
		return RepairResult{Status: StatusServiceMissing, Message: "service.sh is missing or not executable"}
	}
	if err := os.MkdirAll(paths.RunDir(), 0750); err != nil {
		return RepairResult{Status: RepairFailed, Message: fmt.Sprintf("create run dir: %v", err)}
	}

	startedAt := time.Now().UTC().Format(time.RFC3339)
	script := repairScript(servicePath, repairStatusPath(paths), startedAt)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return RepairResult{Status: RepairFailed, Message: fmt.Sprintf("open devnull: %v", err)}
	}
	defer devNull.Close()

	proc, err := os.StartProcess("/system/bin/sh", []string{"sh", "-c", script}, &os.ProcAttr{
		Files: []*os.File{devNull, devNull, devNull},
	})
	if err != nil && errors.Is(err, os.ErrNotExist) {
		proc, err = os.StartProcess("/bin/sh", []string{"sh", "-c", script}, &os.ProcAttr{
			Files: []*os.File{devNull, devNull, devNull},
		})
	}
	if err != nil {
		status := RepairStatus{
			Status:     RepairFailed,
			StartedAt:  startedAt,
			FinishedAt: time.Now().UTC().Format(time.RFC3339),
			Message:    fmt.Sprintf("start service.sh: %v", err),
		}
		_ = writeRepairStatus(paths, status)
		return RepairResult{Status: RepairFailed, Message: status.Message}
	}
	_ = proc.Release()

	status = RepairStatus{
		Status:    RepairRunning,
		PID:       proc.Pid,
		StartedAt: startedAt,
		Message:   "module repair started",
	}
	_ = writeRepairStatus(paths, status)
	return RepairResult{Status: RepairStarted, PID: proc.Pid, Message: status.Message}
}

func CurrentRepairStatus(moduleDir string) RepairStatus {
	paths := modulecontract.NewPaths(moduleDir)
	if pid := serviceLockPID(paths); pid > 0 && processMatchesService(pid, filepath.Join(paths.Dir(), "service.sh")) {
		status := readRepairStatus(paths)
		status.Status = RepairRunning
		status.PID = pid
		if status.Message == "" {
			status.Message = "service.sh repair is running"
		}
		return status
	}

	status := readRepairStatus(paths)
	if status.Status == RepairRunning {
		if status.PID > 0 && processExists(status.PID) {
			return status
		}
		status.Status = RepairFailed
		status.Message = "repair process exited without final status"
		return status
	}
	if status.Status != "" {
		return status
	}
	return RepairStatus{Status: RepairIdle}
}

func hasPendingUpdate(moduleDir string) bool {
	for _, base := range []string{"/data/adb/modules_update", "/data/adb/ksu/modules_update", "/data/adb/ap/modules_update"} {
		if exists(filepath.Join(base, filepath.Base(moduleDir))) {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func readPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processMatchesPath(pid int, wanted string) bool {
	if !processExists(pid) {
		return false
	}
	if exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe")); err == nil {
		if exe == wanted || filepath.Clean(exe) == filepath.Clean(wanted) {
			return true
		}
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), wanted)
}

func processMatchesService(pid int, servicePath string) bool {
	if !processExists(pid) {
		return false
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), servicePath)
}

func repairStatusPath(paths modulecontract.Paths) string {
	return filepath.Join(paths.RunDir(), "app_repair.status.json")
}

func serviceLockPID(paths modulecontract.Paths) int {
	return readPID(filepath.Join(paths.ConfigDir(), "service.lock", "pid"))
}

func readRepairStatus(paths modulecontract.Paths) RepairStatus {
	data, err := os.ReadFile(repairStatusPath(paths))
	if err != nil {
		return RepairStatus{}
	}
	var status RepairStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return RepairStatus{Status: RepairFailed, Message: fmt.Sprintf("parse repair status: %v", err)}
	}
	return status
}

func writeRepairStatus(paths modulecontract.Paths, status RepairStatus) error {
	if err := os.MkdirAll(paths.RunDir(), 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := repairStatusPath(paths) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, repairStatusPath(paths))
}

func repairScript(servicePath, statusPath, startedAt string) string {
	statusDir := filepath.Dir(statusPath)
	return strings.Join([]string{
		"umask 077",
		"mkdir -p " + shellQuote(statusDir),
		"printf '{\"status\":\"%s\",\"pid\":%s,\"startedAt\":\"%s\",\"message\":\"module repair is running\"}\\n' " + shellQuote(RepairRunning) + " \"$$\" " + shellQuote(startedAt) + " > " + shellQuote(statusPath),
		"RKNNOVPN_APP_REPAIR=1 /system/bin/sh " + shellQuote(servicePath) + " --app-repair >/dev/null 2>&1",
		"code=$?",
		"finished=$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo '')",
		"if [ \"$code\" = 0 ]; then st=" + shellQuote(RepairSucceeded) + "; msg='module repair completed'; else st=" + shellQuote(RepairFailed) + "; msg='module repair failed'; fi",
		"tmp=" + shellQuote(statusPath+".tmp"),
		"printf '{\"status\":\"%s\",\"exitCode\":%s,\"startedAt\":\"%s\",\"finishedAt\":\"%s\",\"message\":\"%s\"}\\n' \"$st\" \"$code\" " + shellQuote(startedAt) + " \"$finished\" \"$msg\" > \"$tmp\"",
		"mv -f \"$tmp\" " + shellQuote(statusPath),
	}, "; ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

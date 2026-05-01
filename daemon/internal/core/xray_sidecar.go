package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

const xrayCheckTimeout = 20 * time.Second

func (m *CoreManager) prepareXraySidecar(profile *config.NodeProfile, configPath string) (bool, error) {
	if !config.RequiresXraySidecar(profile) {
		_ = os.Remove(configPath)
		return false, nil
	}
	data, err := config.RenderXraySidecarConfig(profile, config.XraySidecarSocksPort)
	if err != nil {
		return true, err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return true, err
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return true, err
	}
	return true, nil
}

func (m *CoreManager) checkXrayConfig(configPath string) error {
	binPath := filepath.Join(modulecontract.NewPaths(m.dataDir).BinDir(), "xray")
	return runXrayConfigCheck(binPath, configPath, xrayCheckTimeout)
}

func runXrayConfigCheck(binPath string, configPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "run", "-test", "-c", configPath)
	outBytes, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(outBytes))
	if ctx.Err() == context.DeadlineExceeded {
		if out != "" {
			return fmt.Errorf("xray check timed out after %s; output: %s", timeout, out)
		}
		return fmt.Errorf("xray check timed out after %s", timeout)
	}
	if err != nil {
		if out != "" {
			return fmt.Errorf("xray check failed: %w; output: %s", err, out)
		}
		return fmt.Errorf("xray check failed: %w", err)
	}
	return nil
}

func (m *CoreManager) spawnXraySidecar(configPath string) (*os.Process, <-chan error, int, string, error) {
	binPath := filepath.Join(modulecontract.NewPaths(m.dataDir).BinDir(), "xray")
	cmd := exec.Command(binPath, "run", "-c", configPath)
	if err := m.prepareXrayCommand(cmd); err != nil {
		return nil, nil, 0, "", err
	}
	logFile, logPath, err := m.openXrayLog()
	if err != nil {
		return nil, nil, 0, "", err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = m.singBoxSysProcAttr()
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, nil, 0, logPath, err
	}
	logFile.Close()
	return cmd.Process, watchCommand(cmd), cmd.Process.Pid, logPath, nil
}

func (m *CoreManager) prepareXrayCommand(cmd *exec.Cmd) error {
	workDir := modulecontract.NewPaths(m.dataDir).RunDir()
	if err := os.MkdirAll(workDir, 0750); err != nil {
		return fmt.Errorf("mkdir xray workdir %s: %w", workDir, err)
	}
	cmd.Dir = workDir
	return nil
}

func (m *CoreManager) openXrayLog() (*os.File, string, error) {
	logDir := modulecontract.NewPaths(m.dataDir).LogDir()
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return nil, "", err
	}
	logPath := filepath.Join(logDir, "xray.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, "", err
	}
	_, _ = fmt.Fprintf(file, "\n--- xray start %s ---\n", time.Now().Format(time.RFC3339))
	return file, logPath, nil
}

func (m *CoreManager) stopXraySidecar() error {
	var firstErr error
	if m.xrayProcess != nil {
		if err := killProcessWithWait(m.xrayProcess, m.xrayExitCh, m.logger); err != nil {
			firstErr = err
		}
	} else if err := m.killTrackedXray(); err != nil {
		firstErr = err
	}
	m.xrayProcess = nil
	m.xrayExitCh = nil
	m.xrayPID = 0
	_ = os.Remove(modulecontract.NewPaths(m.dataDir).XrayPIDFile())
	return firstErr
}

func killProcessWithWait(proc *os.Process, waitCh <-chan error, logger *log.Logger) error {
	if proc == nil {
		return nil
	}
	pid := proc.Pid
	if waitCh == nil {
		return fmt.Errorf("pid %d has no tracked process wait channel", pid)
	}
	logger.Printf("sending SIGTERM to pid %d", pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		logger.Printf("SIGTERM failed (may be already dead): %v", err)
		select {
		case err := <-waitCh:
			if err != nil {
				logger.Printf("tracked wait after SIGTERM failure returned: %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			if proc.Signal(syscall.Signal(0)) == nil {
				return fmt.Errorf("pid %d still appears alive after SIGTERM failed: %w", pid, err)
			}
		}
		return nil
	}
	select {
	case err := <-waitCh:
		if err != nil {
			logger.Printf("wait after SIGTERM returned: %v", err)
		} else {
			logger.Printf("pid %d exited after SIGTERM", pid)
		}
		return nil
	case <-time.After(5 * time.Second):
	}
	logger.Printf("pid %d did not exit, sending SIGKILL", pid)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("SIGKILL pid %d: %w", pid, err)
	}
	select {
	case err := <-waitCh:
		if err != nil {
			logger.Printf("wait after SIGKILL returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		return fmt.Errorf("pid %d did not exit after SIGKILL", pid)
	}
	return nil
}

func (m *CoreManager) killTrackedXray() error {
	type trackedPID struct {
		pid       int
		startTime string
	}
	pids := make(map[int]trackedPID)
	if m.xrayPID > 0 {
		pids[m.xrayPID] = trackedPID{pid: m.xrayPID}
	}
	if raw, err := os.ReadFile(modulecontract.NewPaths(m.dataDir).XrayPIDFile()); err == nil {
		if pid, startTime, parseErr := parseSingBoxPIDFile(raw); parseErr == nil && pid > 0 {
			pids[pid] = trackedPID{pid: pid, startTime: startTime}
		}
	}

	var errs []string
	for pid, tracked := range pids {
		if pid == os.Getpid() {
			continue
		}
		if tracked.startTime != "" {
			currentStartTime, err := procStartTime(pid)
			if err != nil || currentStartTime != tracked.startTime {
				m.logger.Printf("skipping stale xray pid %d: starttime mismatch", pid)
				continue
			}
		}
		if !m.pidLooksLikeOwnedXray(pid) {
			m.logger.Printf("skipping stale xray pid %d: ownership does not match", pid)
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		m.logger.Printf("sending SIGTERM to tracked xray pid %d", pid)
		_ = proc.Signal(syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			m.logger.Printf("tracked xray pid %d still alive, sending SIGKILL", pid)
			if killErr := proc.Signal(syscall.SIGKILL); killErr != nil {
				errs = append(errs, fmt.Sprintf("SIGKILL pid %d: %v", pid, killErr))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *CoreManager) pidLooksLikeOwnedXray(pid int) bool {
	paths := modulecontract.NewPaths(m.dataDir)
	expectedBin := filepath.Clean(filepath.Join(paths.BinDir(), "xray"))
	expectedConfig := filepath.Clean(filepath.Join(paths.RenderedConfigDir(), "xray-xhttp.json"))

	exe := ""
	if target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe")); err == nil {
		exe = filepath.Clean(target)
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	cmdTokens := cleanedCmdlinePathTokens(string(data))
	binOK := exe == expectedBin || stringSliceContains(cmdTokens, expectedBin)
	configOK := stringSliceContains(cmdTokens, expectedConfig)
	return binOK && configOK
}

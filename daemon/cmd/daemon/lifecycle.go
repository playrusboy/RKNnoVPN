package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) startSubsystems() {
	cfg := d.currentConfig()
	if d.healthMon != nil && cfg != nil && cfg.Health.Enabled && cfg.Health.IntervalSec > 0 {
		d.healthMon.Start()
	}

	if d.netWatcher != nil {
		if err := d.netWatcher.Start(); err != nil {
			log.Printf("network watcher not started: %v", err)
		}
	}
}

func (d *daemon) stopSubsystems() {
	if d.healthMon != nil {
		d.healthMon.Stop()
	}
	if d.netWatcher != nil {
		d.netWatcher.Stop()
	}
}

func (d *daemon) shutdown() {
	d.stopSubsystems()
	if err := d.coreMgr.Stop(); err != nil {
		log.Printf("core stop error: %v", err)
	}
	if d.ipcServer != nil {
		d.ipcServer.Stop()
	}
}

func (d *daemon) reloadConfig() error {
	loadedConfig, err := profile.LoadConfigProjection(d.cfgPath)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}
	newCfg := loadedConfig.Config

	if err := d.applyConfigWithOperation(newCfg, true, runtimev2.OperationReload); err != nil {
		return fmt.Errorf("reload config apply: %w", err)
	}

	log.Printf("config reloaded")
	return nil
}

func (d *daemon) dumpState() {
	status := d.coreMgr.Status()

	rescueEnabled := false
	if cfg := d.currentConfig(); cfg != nil {
		rescueEnabled = cfg.Rescue.Enabled
	}

	state := map[string]interface{}{
		"version":         Version,
		"config_path":     d.cfgPath,
		"data_dir":        d.dataDir,
		"core_state":      status.State,
		"core_pid":        status.PID,
		"uptime":          status.Uptime,
		"active_profile":  status.ActiveProfile,
		"health_fails":    d.healthMon.Failures(),
		"rescue_attempts": d.rescueMgr.Attempts(),
		"rescue_enabled":  rescueEnabled,
	}

	data, _ := json.MarshalIndent(state, "", "  ")
	log.Printf("STATE DUMP:\n%s", string(data))
}

func writePID(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0644)
}

func removePIDIfOwned(path string, pid int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	current, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || current != pid {
		return nil
	}
	return os.Remove(path)
}

func refreshPIDIfMissingOrOwned(path string, pid int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return writePID(path, pid)
		}
		return err
	}
	current, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err == nil && current != pid {
		return nil
	}
	return writePID(path, pid)
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

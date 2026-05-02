package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

const (
	updateCheckStateFileName = "update-check-state.json"
	UpdateCheckMinInterval   = 24 * time.Hour
)

type UpdateCheckState struct {
	LastCheckedAt  time.Time `json:"lastCheckedAt,omitempty"`
	CurrentVersion string    `json:"currentVersion"`
	LatestVersion  string    `json:"latestVersion"`
	HasUpdate      bool      `json:"hasUpdate"`
	Changelog      string    `json:"changelog,omitempty"`
	ModuleSize     int64     `json:"moduleSize,omitempty"`
	ApkSize        int64     `json:"apkSize,omitempty"`
	Error          string    `json:"error,omitempty"`
}

func ReadUpdateCheckState(dataDir string) (*UpdateCheckState, error) {
	data, err := os.ReadFile(UpdateCheckStatePath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("updater: read update check state: %w", err)
	}
	var state UpdateCheckState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("updater: parse update check state: %w", err)
	}
	return &state, nil
}

func CheckForUpdateAndPersist(dataDir, currentVersion string) (*UpdateInfo, error) {
	return CheckForUpdateAndPersistWithContext(context.Background(), dataDir, currentVersion)
}

func CheckForUpdateAndPersistWithContext(ctx context.Context, dataDir, currentVersion string) (*UpdateInfo, error) {
	info, err := CheckForUpdateWithContext(ctx, currentVersion)
	state := updateCheckStateFromResult(currentVersion, info, err)
	if writeErr := WriteUpdateCheckState(dataDir, state); writeErr != nil && err == nil {
		return info, writeErr
	}
	return info, err
}

func MaybeCheckForUpdate(dataDir, currentVersion string, now time.Time) (*UpdateCheckState, bool, error) {
	return MaybeCheckForUpdateWithContext(context.Background(), dataDir, currentVersion, now)
}

func MaybeCheckForUpdateWithContext(ctx context.Context, dataDir, currentVersion string, now time.Time) (*UpdateCheckState, bool, error) {
	state, err := ReadUpdateCheckState(dataDir)
	if err != nil {
		return nil, false, err
	}
	if state != nil &&
		NormalizeVersionTag(state.CurrentVersion) == NormalizeVersionTag(currentVersion) &&
		!state.LastCheckedAt.IsZero() &&
		now.Sub(state.LastCheckedAt) < UpdateCheckMinInterval {
		return state, false, nil
	}
	info, err := CheckForUpdateWithContext(ctx, currentVersion)
	state = updateCheckStateFromResult(currentVersion, info, err)
	if writeErr := WriteUpdateCheckState(dataDir, state); writeErr != nil && err == nil {
		return state, true, writeErr
	}
	return state, true, err
}

func WriteUpdateCheckState(dataDir string, state *UpdateCheckState) error {
	if state == nil {
		return nil
	}
	path := UpdateCheckStatePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("updater: mkdir update check state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("updater: marshal update check state: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("updater: write update check state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("updater: replace update check state: %w", err)
	}
	return nil
}

func UpdateCheckStatePath(dataDir string) string {
	clean := filepath.Clean(strings.TrimSpace(dataDir))
	defaultModuleDir := filepath.Clean(modulecontract.DefaultModuleDir)
	if clean == "." || clean == "" || clean == defaultModuleDir ||
		strings.HasPrefix(clean, defaultModuleDir+string(os.PathSeparator)) {
		return filepath.Join(modulecontract.DefaultStateDir, updateCheckStateFileName)
	}
	return filepath.Join(clean, modulecontract.DataDirName, updateCheckStateFileName)
}

func updateCheckStateFromResult(currentVersion string, info *UpdateInfo, checkErr error) *UpdateCheckState {
	state := &UpdateCheckState{
		LastCheckedAt:  time.Now().UTC(),
		CurrentVersion: NormalizeVersionTag(currentVersion),
	}
	if info != nil {
		state.CurrentVersion = info.CurrentVersion
		state.LatestVersion = info.LatestVersion
		state.HasUpdate = info.HasUpdate
		state.Changelog = info.Changelog
		state.ModuleSize = info.ModuleSize
		state.ApkSize = info.ApkSize
	}
	if checkErr != nil {
		state.Error = checkErr.Error()
	}
	return state
}

package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
)

type LoadedConfig struct {
	Config       *config.Config
	ProfilePath  string
	ProfileFound bool
	ProfileReset bool
	ResetReason  string
}

func LoadConfigProjection(configPath string) (LoadedConfig, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return LoadedConfig{}, err
	}

	profilePath := Path(configPath)
	profileDoc, profileFound, err := Load(profilePath)
	if err != nil {
		resetReason, resetErr := deleteUnsupportedProfile(profilePath, err)
		if resetErr != nil {
			return LoadedConfig{}, fmt.Errorf("load profile: %w", resetErr)
		}
		return initializeFreshProfile(cfg, profilePath, resetReason)
	}
	if !profileFound {
		legacyProfilePath := LegacyPath(configPath)
		if legacyProfilePath != profilePath {
			profileDoc, profileFound, err = Load(legacyProfilePath)
			if err != nil {
				return LoadedConfig{}, fmt.Errorf("load module profile: %w", err)
			}
			if profileFound {
				if err := Save(profilePath, profileDoc); err != nil {
					return LoadedConfig{}, fmt.Errorf("persist profile state: %w", err)
				}
			}
		}
	}
	if profileFound {
		cfg, _, err = ApplyToConfig(cfg, profileDoc)
		if err != nil {
			return LoadedConfig{}, fmt.Errorf("apply profile: %w", err)
		}
		return LoadedConfig{Config: cfg, ProfilePath: profilePath, ProfileFound: true}, nil
	}

	if cfg.Node.Address != "" {
		return LoadedConfig{}, fmt.Errorf("profile.json is required for v2 config with an active node; legacy config-only nodes are unsupported")
	}
	return initializeFreshProfile(cfg, profilePath, "")
}

func initializeFreshProfile(cfg *config.Config, profilePath string, resetReason string) (LoadedConfig, error) {
	doc := FromConfig(cfg)
	if err := Save(profilePath, doc); err != nil {
		return LoadedConfig{}, fmt.Errorf("initialize profile: %w", err)
	}
	if resetReason != "" {
		applied, _, err := ApplyToConfig(cfg, doc)
		if err != nil {
			return LoadedConfig{}, fmt.Errorf("apply fresh profile: %w", err)
		}
		cfg = applied
	}
	return LoadedConfig{
		Config:       cfg,
		ProfilePath:  profilePath,
		ProfileReset: resetReason != "",
		ResetReason:  resetReason,
	}, nil
}

func deleteUnsupportedProfile(path string, loadErr error) (string, error) {
	var invalid InvalidProfileError
	if !errors.As(loadErr, &invalid) {
		return "", loadErr
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("delete unsupported profile %s: %w", path, err)
	}
	syncDirBestEffort(filepath.Dir(path))
	return loadErr.Error(), nil
}

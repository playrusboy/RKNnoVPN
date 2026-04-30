package profile

import (
	"fmt"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
)

type LoadedConfig struct {
	Config       *config.Config
	ProfilePath  string
	ProfileFound bool
}

func LoadConfigProjection(configPath string) (LoadedConfig, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return LoadedConfig{}, err
	}

	profilePath := Path(configPath)
	profileDoc, profileFound, err := Load(profilePath)
	if err != nil {
		return LoadedConfig{}, fmt.Errorf("load profile: %w", err)
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
	if err := Save(profilePath, FromConfig(cfg)); err != nil {
		return LoadedConfig{}, fmt.Errorf("initialize profile: %w", err)
	}
	return LoadedConfig{Config: cfg, ProfilePath: profilePath}, nil
}

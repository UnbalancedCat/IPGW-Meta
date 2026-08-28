package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	BaseDir                 string
	ConfigFile              string
	ProtocolCacheDir        string
	MigrationMarker         string
	MigrationJournal        string
	MigrationConfigRecovery string
	MigrationMarkerRecovery string
	LegacyMetaYAML          string
	LegacyUpstream          string
}

func DefaultPaths() (Paths, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	base := filepath.Join(configDir, "ipgw-meta")
	return Paths{
		BaseDir:                 base,
		ConfigFile:              filepath.Join(base, "config.yaml"),
		ProtocolCacheDir:        filepath.Join(base, "protocol-cache"),
		MigrationMarker:         filepath.Join(base, "migration-v1.yaml"),
		MigrationJournal:        filepath.Join(base, "migration-v1.pending.yaml"),
		MigrationConfigRecovery: filepath.Join(base, "migration-v1.config.before"),
		MigrationMarkerRecovery: filepath.Join(base, "migration-v1.marker.before"),
		LegacyMetaYAML:          filepath.Join(configDir, "ipgw", "config.yaml"),
		LegacyUpstream:          filepath.Join(home, ".ipgw"),
	}, nil
}

func WithConfigPath(paths Paths, path string) Paths {
	if path == "" {
		return paths
	}
	paths.ConfigFile = path
	paths.BaseDir = filepath.Dir(path)
	paths.ProtocolCacheDir = filepath.Join(paths.BaseDir, "protocol-cache")
	paths.MigrationMarker = filepath.Join(paths.BaseDir, "migration-v1.yaml")
	paths.MigrationJournal = filepath.Join(paths.BaseDir, "migration-v1.pending.yaml")
	paths.MigrationConfigRecovery = filepath.Join(paths.BaseDir, "migration-v1.config.before")
	paths.MigrationMarkerRecovery = filepath.Join(paths.BaseDir, "migration-v1.marker.before")
	return paths
}

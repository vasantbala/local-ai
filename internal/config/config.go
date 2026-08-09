// Package config loads and saves local-ai's on-disk configuration and
// resolves where that configuration and its related state (models, presets,
// keys, logs) live on disk.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is local-ai's persisted configuration (config.yaml).
type Config struct {
	GatewayHost string `yaml:"gateway_host"`
	GatewayPort int    `yaml:"gateway_port"`

	InternalHost string `yaml:"internal_host"`
	InternalPort int    `yaml:"internal_port"`
	// InternalAPIKey is the shared secret between the supervisor and the
	// gateway; llama-server never exposes this to the network since it only
	// listens on InternalHost.
	InternalAPIKey string `yaml:"internal_api_key"`

	LlamaServerPath string `yaml:"llama_server_path"`
	ModelsDir       string `yaml:"models_dir"`
	ModelsMax       int    `yaml:"models_max"`
	// IdleTimeoutSeconds maps to llama-server's --sleep-idle-seconds.
	IdleTimeoutSeconds int `yaml:"idle_timeout_seconds"`

	// ModelOverrides holds per-model llama-server flag overrides, written
	// through verbatim into presets.ini. Keys are long flag names without
	// leading dashes (e.g. "ctx-size", "gpu-layers").
	ModelOverrides map[string]map[string]string `yaml:"model_overrides"`

	LogLevel string `yaml:"log_level"`
}

// Paths are the on-disk locations derived from a data directory.
type Paths struct {
	DataDir     string
	ConfigPath  string
	ModelsDir   string
	PresetsPath string
	KeysPath    string
	LogsDir     string
}

// DataDir resolves local-ai's data directory. Priority: explicit override,
// then LOCAL_AI_DATA_DIR env var, then %PROGRAMDATA%\local-ai.
func DataDir(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("LOCAL_AI_DATA_DIR"); v != "" {
		return v
	}
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "local-ai")
}

// PathsFor derives all on-disk locations from a data directory.
func PathsFor(dataDir string) Paths {
	return Paths{
		DataDir:     dataDir,
		ConfigPath:  filepath.Join(dataDir, "config.yaml"),
		ModelsDir:   filepath.Join(dataDir, "models"),
		PresetsPath: filepath.Join(dataDir, "presets.ini"),
		KeysPath:    filepath.Join(dataDir, "keys.json"),
		LogsDir:     filepath.Join(dataDir, "logs"),
	}
}

func defaults(paths Paths) (*Config, error) {
	secret, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generating internal api key: %w", err)
	}
	return &Config{
		GatewayHost:        "0.0.0.0",
		GatewayPort:        11535,
		InternalHost:       "127.0.0.1",
		InternalPort:       11536,
		InternalAPIKey:     secret,
		LlamaServerPath:    "llama-server.exe",
		ModelsDir:          paths.ModelsDir,
		ModelsMax:          4,
		IdleTimeoutSeconds: 600,
		ModelOverrides:     map[string]map[string]string{},
		LogLevel:           "info",
	}, nil
}

// Load reads config.yaml from dataDir, creating it with defaults (and
// ensuring the data directory tree exists) if it isn't present yet.
func Load(dataDir string) (*Config, Paths, error) {
	paths := PathsFor(dataDir)

	for _, dir := range []string{paths.DataDir, paths.ModelsDir, paths.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, paths, fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	data, err := os.ReadFile(paths.ConfigPath)
	if os.IsNotExist(err) {
		cfg, err := defaults(paths)
		if err != nil {
			return nil, paths, err
		}
		if err := Save(paths, cfg); err != nil {
			return nil, paths, err
		}
		return cfg, paths, nil
	}
	if err != nil {
		return nil, paths, fmt.Errorf("reading %s: %w", paths.ConfigPath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, paths, fmt.Errorf("parsing %s: %w", paths.ConfigPath, err)
	}
	if cfg.ModelOverrides == nil {
		cfg.ModelOverrides = map[string]map[string]string{}
	}
	return &cfg, paths, nil
}

// Save writes cfg to paths.ConfigPath.
func Save(paths Paths, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.MkdirAll(paths.DataDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", paths.DataDir, err)
	}
	if err := os.WriteFile(paths.ConfigPath, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", paths.ConfigPath, err)
	}
	return nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

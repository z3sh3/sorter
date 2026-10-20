// Package config loads, applies defaults and environment overrides, then
// validates the application settings.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root application configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	LLM       LLMConfig       `yaml:"llm"`
	Storage   StorageConfig   `yaml:"storage"`
	Extractor ExtractorConfig `yaml:"extractor"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type AuthConfig struct {
	// TokenHash is "sha256:<hex>" of the pre-shared access token.
	TokenHash string        `yaml:"token_hash"`
	JWTTTL    time.Duration `yaml:"jwt_ttl"`
}

type LLMConfig struct {
	// BaseURL is an OpenAI-compatible endpoint, e.g. https://api.openai.com/v1
	BaseURL string        `yaml:"base_url"`
	APIKey  string        `yaml:"api_key"`
	Model   string        `yaml:"model"`
	Timeout time.Duration `yaml:"timeout"`
}

type StorageConfig struct {
	DBPath    string `yaml:"db_path"`
	UploadDir string `yaml:"upload_dir"`
}

type ExtractorConfig struct {
	Workers int `yaml:"workers"`
}

// Load reads the YAML config file, applies defaults and env overrides,
// then validates required fields.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Server:    ServerConfig{Addr: ":8080"},
		Auth:      AuthConfig{JWTTTL: 7 * 24 * time.Hour},
		LLM:       LLMConfig{Timeout: 60 * time.Second},
		Storage:   StorageConfig{DBPath: "./data/sorter.db", UploadDir: "./data/uploads"},
		Extractor: ExtractorConfig{Workers: 2},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config file: %w", err)
		}
		// Missing file is tolerated: rely on defaults + env vars.
	} else if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	applyEnvOverrides(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	setStr := func(dst *string, env string) {
		if v := os.Getenv(env); v != "" {
			*dst = v
		}
	}
	setStr(&cfg.Server.Addr, "SORTER_ADDR")
	setStr(&cfg.Auth.TokenHash, "SORTER_TOKEN_HASH")
	setStr(&cfg.LLM.BaseURL, "SORTER_LLM_BASE_URL")
	setStr(&cfg.LLM.APIKey, "SORTER_LLM_API_KEY")
	setStr(&cfg.LLM.Model, "SORTER_LLM_MODEL")
	setStr(&cfg.Storage.DBPath, "SORTER_DB_PATH")
	setStr(&cfg.Storage.UploadDir, "SORTER_UPLOAD_DIR")
}

func (c *Config) validate() error {
	if c.Auth.TokenHash == "" {
		return fmt.Errorf("auth.token_hash is required (generate with: sorter hash-token <token>)")
	}
	if !strings.HasPrefix(c.Auth.TokenHash, "sha256:") {
		return fmt.Errorf("auth.token_hash must be in format \"sha256:<hex>\"")
	}
	if c.LLM.BaseURL == "" || c.LLM.APIKey == "" || c.LLM.Model == "" {
		return fmt.Errorf("llm.base_url, llm.api_key and llm.model are all required")
	}
	if c.Extractor.Workers < 1 {
		c.Extractor.Workers = 1
	}
	return nil
}

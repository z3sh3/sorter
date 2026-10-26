package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validYAML = `
server:
  addr: ":9090"
auth:
  token_hash: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
  jwt_ttl: 24h
llm:
  base_url: "https://example.test/v1"
  api_key: "key"
  model: "model"
  timeout: 30s
storage:
  db_path: "./data/test.db"
  upload_dir: "./data/uploads"
extractor:
  workers: 3
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Addr != ":9090" || cfg.Extractor.Workers != 3 || cfg.LLM.Timeout != 30*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

// Typos must fail loudly instead of silently falling back to defaults.
func TestLoadRejectsUnknownFields(t *testing.T) {
	body := validYAML + "\nunknown_section:\n  enabled: true\n"
	if _, err := Load(writeConfig(t, body)); err == nil {
		t.Fatal("unknown field was ignored")
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("SORTER_ADDR", ":7070")
	t.Setenv("SORTER_LLM_MODEL", "env-model")
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Addr != ":7070" || cfg.LLM.Model != "env-model" {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
}

func TestLoadValidationFailures(t *testing.T) {
	cases := map[string]string{
		"missing token hash": strings.Replace(validYAML, `token_hash: "sha256:0000000000000000000000000000000000000000000000000000000000000000"`, `token_hash: ""`, 1),
		"short token hash":   strings.Replace(validYAML, "sha256:0000000000000000000000000000000000000000000000000000000000000000", "sha256:abcd", 1),
		"bad base url":       strings.Replace(validYAML, "https://example.test/v1", "ftp://example.test", 1),
		"url with query":     strings.Replace(validYAML, "https://example.test/v1", "https://example.test/v1?x=1", 1),
		"missing api key":    strings.Replace(validYAML, `api_key: "key"`, `api_key: ""`, 1),
		"zero workers":       strings.Replace(validYAML, "workers: 3", "workers: 0", 1),
		"excessive timeout":  strings.Replace(validYAML, "timeout: 30s", "timeout: 10m", 1),
		"missing db path":    strings.Replace(validYAML, `db_path: "./data/test.db"`, `db_path: ""`, 1),
	}
	for name, body := range cases {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Errorf("%s: config was accepted", name)
		}
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	t.Setenv("SORTER_TOKEN_HASH", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	t.Setenv("SORTER_LLM_BASE_URL", "https://example.test/v1")
	t.Setenv("SORTER_LLM_API_KEY", "key")
	t.Setenv("SORTER_LLM_MODEL", "model")
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Addr != ":8080" || cfg.Extractor.Workers != 2 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

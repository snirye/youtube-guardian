package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	token := strings.Repeat("ab", 32)
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	t.Run("defaults", func(t *testing.T) {
		writeConfig(t, path, `{"api_token":"`+token+`"}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIToken != token || cfg.ListenAddress() != "127.0.0.1:8765" || cfg.DatabasePath != filepath.Join(dir, "guardian.sqlite") {
			t.Fatal("configuration did not preserve the token and defaults")
		}
	})

	t.Run("custom port and path", func(t *testing.T) {
		writeConfig(t, path, `{"port":9876,"database_path":"data/store.sqlite","api_token":"`+token+`"}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ListenAddress() != "127.0.0.1:9876" || cfg.DatabasePath != filepath.Join(dir, "data", "store.sqlite") {
			t.Fatal("configuration did not use the configured port and path")
		}
	})

	invalid := map[string]string{
		"missing token":       `{}`,
		"nonhex token":        `{"api_token":"` + strings.Repeat("g", 64) + `"}`,
		"invalid port":        `{"port":65536,"api_token":"` + token + `"}`,
		"empty database path": `{"database_path":"","api_token":"` + token + `"}`,
		"allow all":           `{"allowAll":true,"api_token":"` + token + `"}`,
		"external listener":   `{"host":"0.0.0.0","api_token":"` + token + `"}`,
		"trailing object":     `{"api_token":"` + token + `"} {}`,
		"null document":       `null`,
	}
	for name, body := range invalid {
		t.Run(name, func(t *testing.T) {
			writeConfig(t, path, body)
			_, err := Load(path)
			if err == nil {
				t.Fatal("accepted invalid configuration")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatal("error exposed the token")
			}
		})
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

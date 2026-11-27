package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const DefaultPort = 8765

var telegramToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{15,}$`)

type Config struct {
	Port          int    `json:"port"`
	DatabasePath  string `json:"database_path"`
	APIToken      string `json:"api_token"`
	TelegramToken string `json:"telegram_bot_token"`
	ParentID      int64  `json:"parent_telegram_user_id"`
	RetentionDays int    `json:"retention_days"`
}

func Load(path string) (Config, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve configuration path: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()

	cfg := Config{Port: DefaultPort, DatabasePath: "guardian.sqlite", RetentionDays: 30}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	// Decode through a pointer so a top-level null can be rejected.
	loaded := &cfg
	if err := decoder.Decode(&loaded); err != nil {
		// JSON decoding errors can include supplied configuration values.
		return Config{}, errors.New("configuration contains invalid JSON or unrecognized fields")
	}
	if loaded == nil || decoder.Decode(new(any)) != io.EOF {
		return Config{}, errors.New("configuration must contain exactly one JSON object")
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		return Config{}, errors.New("port must be between 1 and 65535")
	}
	if cfg.DatabasePath == "" {
		return Config{}, errors.New("database_path must not be empty")
	}
	if len(cfg.APIToken) != 64 {
		return Config{}, errors.New("api_token must be a 64-character hexadecimal string")
	}
	if _, err := hex.DecodeString(cfg.APIToken); err != nil {
		return Config{}, errors.New("api_token must be a 64-character hexadecimal string")
	}
	if cfg.RetentionDays < 1 || cfg.RetentionDays > 3650 {
		return Config{}, errors.New("retention_days must be between 1 and 3650")
	}
	if cfg.TelegramToken != "" || cfg.ParentID != 0 {
		if cfg.ParentID <= 0 || !telegramToken.MatchString(cfg.TelegramToken) {
			return Config{}, errors.New("set both a valid telegram_bot_token and a positive parent_telegram_user_id")
		}
	}
	if !filepath.IsAbs(cfg.DatabasePath) {
		cfg.DatabasePath = filepath.Join(filepath.Dir(path), cfg.DatabasePath)
	}
	return cfg, nil
}

func (cfg Config) ListenAddress() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port))
}

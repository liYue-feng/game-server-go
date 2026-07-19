package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMapsDevelopmentConfiguration(t *testing.T) {
	path := writeConfigFile(t, `development:
  enabled: true
  login_enabled: true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Development.Enabled {
		t.Fatal("Development.Enabled = false, want true")
	}
	if !cfg.Development.LoginEnabled {
		t.Fatal("Development.LoginEnabled = false, want true")
	}
}

func TestLoadMapsNestedEnvironmentVariables(t *testing.T) {
	path := writeConfigFile(t, `development:
  enabled: false
`)
	t.Setenv("GAME_DEVELOPMENT_ENABLED", "true")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Development.Enabled {
		t.Fatal("Development.Enabled = false, want environment override true")
	}
}

func TestCommittedProductionConfigContainsNoCredentials(t *testing.T) {
	for _, key := range []string{
		"GAME_MYSQL_PASSWORD",
		"GAME_REDIS_PASSWORD",
		"GAME_WECHAT_APP_ID",
		"GAME_WECHAT_APP_SECRET",
	} {
		t.Setenv(key, "")
	}

	configPath := filepath.Join("..", "..", "configs", "config.yaml")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load(%q) error = %v", configPath, err)
	}

	for name, value := range map[string]string{
		"mysql.password":    cfg.MySQL.Password,
		"redis.password":    cfg.Redis.Password,
		"wechat.app_id":     cfg.Wechat.AppID,
		"wechat.app_secret": cfg.Wechat.AppSecret,
	} {
		if value != "" {
			t.Errorf("%s is populated in committed production config", name)
		}
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

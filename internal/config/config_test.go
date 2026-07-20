package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadMapsDevelopmentAndGMConfiguration(t *testing.T) {
	path := writeConfigFile(t, `development:
  enabled: true
  login_enabled: true
gm:
  admin_uids: [101, 202]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Development.Enabled || !cfg.Development.LoginEnabled {
		t.Fatalf("Development = %#v, want both flags enabled", cfg.Development)
	}
	if len(cfg.GM.AdminUIDs) != 2 || cfg.GM.AdminUIDs[0] != 101 || cfg.GM.AdminUIDs[1] != 202 {
		t.Fatalf("GM.AdminUIDs = %v, want [101 202]", cfg.GM.AdminUIDs)
	}
}

func TestLoadMapsNestedEnvironmentVariables(t *testing.T) {
	path := writeConfigFile(t, `development:
  enabled: false
mysql:
  password: "from-file"
`)
	t.Setenv("GAME_DEVELOPMENT_ENABLED", "true")
	t.Setenv("GAME_MYSQL_PASSWORD", "from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Development.Enabled {
		t.Fatal("Development.Enabled = false, want environment override true")
	}
	if cfg.MySQL.Password != "from-env" {
		t.Fatalf("MySQL.Password = %q, want environment override", cfg.MySQL.Password)
	}
}

func TestDevelopmentConfigEnablesLocalLogin(t *testing.T) {
	v := readConfigFileOnly(t, filepath.Join("..", "..", "configs", "config.dev.yaml"))
	if !v.GetBool("development.enabled") || !v.GetBool("development.login_enabled") {
		t.Fatal("config.dev.yaml must enable development and development login")
	}
}

func TestProductionConfigContainsNoMySQLPassword(t *testing.T) {
	v := readConfigFileOnly(t, filepath.Join("..", "..", "configs", "config.yaml"))
	if v.GetString("mysql.password") != "" {
		t.Fatalf("MySQL.Password is committed in configs/config.yaml, want empty")
	}
}

func readConfigFileOnly(t *testing.T, path string) *viper.Viper {
	t.Helper()

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig(%q) error = %v", path, err)
	}
	return v
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

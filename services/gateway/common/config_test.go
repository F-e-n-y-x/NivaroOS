package common

import (
	"os"
	"path/filepath"
	"testing"
)

// viper 1.20 dropped the ini codec: the gateway panicked on start ("decoder
// not found for this format") and the 1.5.3 update rolled back. Load and save
// a real gateway.ini the way main.go does.
func TestLoadConfigReadsAndWritesGatewayIni(t *testing.T) {
	dir := t.TempDir()
	ini := "[common]\nRuntimePath=/var/run/nivaroos\n\n[gateway]\nLogPath=/var/log/nivaroos\nPort=80\n"
	if err := os.WriteFile(filepath.Join(dir, "gateway.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NIVAROOS_CONFIG_PATH", dir)
	t.Chdir(dir)
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := c.GetString(ConfigKeyGatewayPort); got != "80" {
		t.Fatalf("port %q", got)
	}
	c.Set(ConfigKeyGatewayPort, "8080")
	if err := c.WriteConfig(); err != nil {
		t.Fatalf("write: %v", err)
	}
	c2, err := LoadConfig()
	if err != nil || c2.GetString(ConfigKeyGatewayPort) != "8080" {
		t.Fatalf("reload: %v %q", err, c2.GetString(ConfigKeyGatewayPort))
	}
}

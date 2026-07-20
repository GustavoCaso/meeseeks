//go:build linux

package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxService_Create_ServiceAlreadyExists(t *testing.T) {
	testDir := setupTestDir(t)

	service := &linuxService{}

	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".meeseeks-test")

	t.Cleanup(func() {
		os.RemoveAll(configDir)
	})

	ctx := t.Context()

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigPath:     configPath,
		ConfigDir:      configDir,
	}

	if _, err := service.Create(ctx, config); err != nil {
		t.Fatalf("First Create() failed: %v", err)
	}

	_, err := service.Create(ctx, config)
	if err == nil {
		t.Error("Second Create() should have failed because service already exists")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected error about service already existing, got: %v", err)
	}
}

func TestLinuxService_Enable_RequiresSystemd(t *testing.T) {
	// Force systemctl lookup to fail by emptying PATH.
	t.Setenv("PATH", "")

	service := &linuxService{}

	err := service.Enable(t.Context(), Defintion("/tmp/meeseeks.service"))
	if err == nil {
		t.Fatal("Enable() should fail when systemd is unavailable")
	}
	if !strings.Contains(err.Error(), "systemd is required") {
		t.Errorf("expected systemd-required error, got: %v", err)
	}
}

func TestLinuxService_Create_RendersUnit(t *testing.T) {
	testDir := setupTestDir(t)

	service := &linuxService{}

	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	configDir := filepath.Join(testDir, "config")

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigPath:     configPath,
		ConfigDir:      configDir,
	}

	def, err := service.Create(t.Context(), config)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	data, err := os.ReadFile(string(def))
	if err != nil {
		t.Fatalf("failed to read unit file: %v", err)
	}

	content := string(data)
	for _, want := range []string{
		"Restart=always",
		"WantedBy=default.target",
		execPath,
		configPath,
		"MEESEEKS_CONFIG_DIR=" + configDir,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("unit file missing %q\n---\n%s", want, content)
		}
	}
}

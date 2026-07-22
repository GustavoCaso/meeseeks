//go:build linux

package login

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxService_Create_ServiceAlreadyExists(t *testing.T) {
	setupLoginTestDir(t)
	testDir := setMeeseeksConfigDirForTest(t)

	service := &linuxService{}

	execPath := createTestExecutable(t, testDir)
	createTestConfig(t, testDir)

	ctx := t.Context()

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigDir:      testDir,
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
	setupLoginTestDir(t)
	testDir := setMeeseeksConfigDirForTest(t)

	service := &linuxService{}

	execPath := createTestExecutable(t, testDir)
	createTestConfig(t, testDir)

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigDir:      testDir,
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
		"WantedBy=multi-user.target",
		execPath,
		"MEESEEKS_CONFIG_DIR=" + testDir,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("unit file missing %q\n---\n%s", want, content)
		}
	}
}

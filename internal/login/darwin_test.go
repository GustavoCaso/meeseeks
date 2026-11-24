//go:build darwin

package login

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDarwinService_Create_RestartsOnlyAfterFailure(t *testing.T) {
	setupLoginTestDir(t)
	testDir := setMeeseeksConfigDirForTest(t)

	service := &darwinService{}
	execPath := createTestExecutable(t, testDir)
	createTestConfig(t, testDir)

	def, err := service.Create(t.Context(), ServiceConfig{
		ExecutablePath: execPath,
		ConfigDir:      testDir,
	})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	output, err := exec.CommandContext(
		t.Context(),
		"plutil", "-extract", "KeepAlive.SuccessfulExit", "raw", "-o", "-", string(def),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("KeepAlive.SuccessfulExit missing from plist: %v: %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "false" {
		t.Fatalf("KeepAlive.SuccessfulExit = %s, want false", got)
	}
}

func TestDarwinService_Validate_ServiceAlreadyExists(t *testing.T) {
	setupLoginTestDir(t)
	testDir := setMeeseeksConfigDirForTest(t)

	service := &darwinService{}

	// Create test executable and config
	execPath := createTestExecutable(t, testDir)
	createTestConfig(t, testDir)

	ctx := t.Context()

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigDir:      testDir,
	}

	// Create the service first time
	_, err := service.Create(ctx, config)
	if err != nil {
		t.Fatalf("First Create() failed: %v", err)
	}

	// Try to create it again - should fail
	_, err = service.Create(ctx, config)
	if err == nil {
		t.Error("Second Create() should have failed because service already exists")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected error about service already existing, got: %v", err)
	}
}

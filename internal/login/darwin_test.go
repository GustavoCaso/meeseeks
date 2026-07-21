//go:build darwin

package login

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinService_Validate_ServiceAlreadyExists(t *testing.T) {
	testDir := setupTestDir(t)

	service := &darwinService{}

	// Create test executable and config
	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	configDir := filepath.Join(testDir, "config")

	ctx := t.Context()

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigPath:     configPath,
		ConfigDir:      configDir,
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

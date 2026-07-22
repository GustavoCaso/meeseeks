//go:build darwin

package login

import (
	"strings"
	"testing"
)

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

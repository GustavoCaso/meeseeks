//go:build darwin

package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()

	testDir := t.TempDir()

	// Set the test directory environment variable
	t.Setenv("MEESEEKS_TEST_LOGIN_DIR", testDir)

	return testDir
}

func createTestExecutable(t *testing.T, dir string) string {
	t.Helper()

	execPath := filepath.Join(dir, "meeseeks")
	err := os.WriteFile(execPath, []byte("#!/bin/bash\necho test"), 0755)
	if err != nil {
		t.Fatalf("failed to create test executable: %v", err)
	}

	return execPath
}

func createTestConfig(t *testing.T, dir string) string {
	t.Helper()

	configPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(configPath, []byte("programs: []"), 0644)
	if err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	return configPath
}

func TestDarwinService_Validate_ServiceAlreadyExists(t *testing.T) {
	testDir := setupTestDir(t)

	service := &darwinService{}

	// Create test executable and config
	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	// Use home directory for config dir to pass validation
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".meeseeks-test")

	t.Cleanup(func() {
		os.RemoveAll(configDir)
	})

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigPath:     configPath,
		ConfigDir:      configDir,
	}

	// Create the service first time
	_, err := service.Create(config)
	if err != nil {
		t.Fatalf("First Create() failed: %v", err)
	}

	// Try to create it again - should fail
	_, err = service.Create(config)
	if err == nil {
		t.Error("Second Create() should have failed because service already exists")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected error about service already existing, got: %v", err)
	}
}

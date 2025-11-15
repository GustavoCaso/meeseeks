//go:build linux

package login

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GustavoCaso/meeseeks/internal/logger"
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

func TestLinuxService_Create_Success(t *testing.T) {
	testDir := setupTestDir(t)

	service := &linuxService{
		logger: logger.NewLogger(os.Stdout, "test"),
	}

	// Create test executable and config
	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".meeseeks-test")

	t.Cleanup(func() {
		os.RemoveAll(configDir)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := ServiceConfig{
		ExecutablePath: execPath,
		ConfigPath:     configPath,
		ConfigDir:      configDir,
	}

	// Create the service
	unitPath, err := service.Create(ctx, config)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify the unit file was created
	if _, statErr := os.Stat(string(unitPath)); os.IsNotExist(statErr) {
		t.Errorf("Unit file was not created at %s", unitPath)
	}

	// Read and verify the unit file content
	content, err := os.ReadFile(string(unitPath))
	if err != nil {
		t.Fatalf("Failed to read unit file: %v", err)
	}

	unitContent := string(content)

	// Verify essential sections exist
	if !strings.Contains(unitContent, "[Unit]") {
		t.Error("Unit file missing [Unit] section")
	}
	if !strings.Contains(unitContent, "[Service]") {
		t.Error("Unit file missing [Service] section")
	}
	if !strings.Contains(unitContent, "[Install]") {
		t.Error("Unit file missing [Install] section")
	}

	// Verify configuration values are present
	if !strings.Contains(unitContent, execPath) {
		t.Errorf("Unit file missing executable path: %s", execPath)
	}
	if !strings.Contains(unitContent, configPath) {
		t.Errorf("Unit file missing config path: %s", configPath)
	}
	if !strings.Contains(unitContent, configDir) {
		t.Errorf("Unit file missing config directory: %s", configDir)
	}

	// Verify systemd-specific settings
	if !strings.Contains(unitContent, "Type=simple") {
		t.Error("Unit file missing Type=simple")
	}
	if !strings.Contains(unitContent, "Restart=on-failure") {
		t.Error("Unit file missing Restart=on-failure")
	}
	if !strings.Contains(unitContent, "WantedBy=default.target") {
		t.Error("Unit file missing WantedBy=default.target")
	}
	if !strings.Contains(unitContent, "MEESEEKS_CONFIG_DIR") {
		t.Error("Unit file missing MEESEEKS_CONFIG_DIR environment variable")
	}
}

func TestLinuxService_Create_ServiceAlreadyExists(t *testing.T) {
	testDir := setupTestDir(t)

	service := &linuxService{
		logger: logger.NewLogger(os.Stdout, "test"),
	}

	// Create test executable and config
	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".meeseeks-test")

	t.Cleanup(func() {
		os.RemoveAll(configDir)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

func TestLinuxService_Status_NotEnabled(t *testing.T) {
	setupTestDir(t)

	service := &linuxService{
		logger: logger.NewLogger(os.Stdout, "test"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check status when service doesn't exist
	status, err := service.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}

	if status.Enabled {
		t.Error("Status should show Enabled=false when service doesn't exist")
	}

	if status.Running {
		t.Error("Status should show Running=false when service doesn't exist")
	}
}

func TestLinuxService_Disable_ServiceNotFound(t *testing.T) {
	setupTestDir(t)

	service := &linuxService{
		logger: logger.NewLogger(os.Stdout, "test"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Try to disable a service that doesn't exist
	err := service.Disable(ctx)
	if err == nil {
		t.Error("Disable() should fail when service doesn't exist")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected error about service not found, got: %v", err)
	}
}

func TestLinuxService_GetSystemdUnitPath(t *testing.T) {
	// Test with environment variable set
	testDir := t.TempDir()
	t.Setenv("MEESEEKS_TEST_LOGIN_DIR", testDir)

	path := getSystemdUnitPath()
	expectedPath := filepath.Join(testDir, serviceName)

	if path != expectedPath {
		t.Errorf("getSystemdUnitPath() = %s, want %s", path, expectedPath)
	}
}

func TestLinuxService_GetSystemdUnitPath_Default(t *testing.T) {
	// Test without environment variable (default path)
	homeDir, _ := os.UserHomeDir()
	expectedPath := filepath.Join(homeDir, ".config", "systemd", "user", serviceName)

	path := getSystemdUnitPath()

	if path != expectedPath {
		t.Errorf("getSystemdUnitPath() = %s, want %s", path, expectedPath)
	}
}

func TestLinuxService_GetLogPath(t *testing.T) {
	testCases := []struct {
		name     string
		logType  string
		expected string
	}{
		{
			name:     "out log",
			logType:  "out",
			expected: "meeseeks.out.log",
		},
		{
			name:     "error log",
			logType:  "error",
			expected: "meeseeks.error.log",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := getLogPath(tc.logType)

			if !strings.HasSuffix(path, tc.expected) {
				t.Errorf("getLogPath(%s) = %s, want path ending with %s", tc.logType, path, tc.expected)
			}
		})
	}
}

func TestLinuxService_GetLogPath_WithConfigDir(t *testing.T) {
	testDir := t.TempDir()
	t.Setenv("MEESEEKS_CONFIG_DIR", testDir)

	path := getLogPath("out")
	expectedPath := filepath.Join(testDir, "meeseeks.out.log")

	if path != expectedPath {
		t.Errorf("getLogPath() = %s, want %s", path, expectedPath)
	}
}

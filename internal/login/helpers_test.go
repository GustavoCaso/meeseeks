//go:build darwin || linux || windows

package login

import (
	"os"
	"path/filepath"
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

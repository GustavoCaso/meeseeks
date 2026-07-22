//go:build darwin || linux || windows

package login

import (
	"os"
	"path/filepath"
	"testing"
)

func setMeeseeksConfigDirForTest(t *testing.T) string {
	t.Helper()

	// Make sure running tests while having a production
	// instance of meeseeks running do not cause problems
	customDir := filepath.Join("/tmp/", t.Name())

	err := os.MkdirAll(customDir, 0750)
	if err != nil {
		t.Fatalf("error creating temp folder for tests %s", err.Error())
	}

	t.Setenv("MEESEEKS_CONFIG_DIR", customDir)

	t.Cleanup(func() {
		os.RemoveAll(customDir)
	})

	return customDir
}

func setupLoginTestDir(t *testing.T) {
	t.Helper()

	testDir := t.TempDir()

	// Set the test directory environment variable
	t.Setenv("MEESEEKS_TEST_LOGIN_DIR", testDir)
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

func createTestConfig(t *testing.T, dir string) {
	t.Helper()

	configPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(configPath, []byte("programs: []"), 0644)
	if err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}
}

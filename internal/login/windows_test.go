//go:build windows

package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestWindowsService_Create_ServiceAlreadyExists(t *testing.T) {
	testDir := setupTestDir(t)

	service := &windowsService{}

	execPath := createTestExecutable(t, testDir)
	configPath := createTestConfig(t, testDir)

	configDir := filepath.Join(testDir, "config")

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

func TestWindowsService_Create_RendersTaskXML(t *testing.T) {
	testDir := setupTestDir(t)

	service := &windowsService{}

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

	raw, err := os.ReadFile(string(def))
	if err != nil {
		t.Fatalf("failed to read task file: %v", err)
	}

	// File must be UTF-16LE with BOM for schtasks /xml.
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
		t.Fatalf("task xml missing UTF-16LE BOM, got prefix %x", raw[:min(2, len(raw))])
	}

	content := decodeUTF16LE(t, raw[2:])
	for _, want := range []string{
		"<LogonTrigger>",
		"<RestartOnFailure>",
		execPath,
		configPath,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("task xml missing %q\n---\n%s", want, content)
		}
	}
}

func decodeUTF16LE(t *testing.T, b []byte) string {
	t.Helper()
	if len(b)%2 != 0 {
		t.Fatalf("odd-length UTF-16 body: %d bytes", len(b))
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u16))
}

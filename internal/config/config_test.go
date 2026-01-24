package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		filename    string
		wantErr     bool
		errContains string
		expected    *Config
	}{
		{
			name:     "valid YAML config",
			filename: "config.yaml",
			content: `programs:
  - name: "test-program"
    command: "echo"
    args: ["hello"]
    env: ["VAR=value"]
    interval: "30s"
    initial_delay: "15s"
    buffer_size_limit: "3MB"
    keep_stdin_open: true
    stdout: "/tmp/out.log"
    stderr: "/tmp/err.log"`,
			expected: &Config{
				Programs: []ProgramConfig{
					{
						Name:            "test-program",
						Command:         "echo",
						Args:            []string{"hello"},
						Env:             []string{"VAR=value"},
						Interval:        "30s",
						InitialDelay:    "15s",
						BufferSizeLimit: "3MB",
						KeepStdinOpen:   true,
						Stdout:          "/tmp/out.log",
						Stderr:          "/tmp/err.log",
					},
				},
			},
		},
		{
			name:     "valid JSON config",
			filename: "config.json",
			content: `{
  "programs": [
    {
      "name": "json-program",
      "command": "ls",
      "args": ["-la"],
      "interval": "1m"
    }
  ]
}`,
			expected: &Config{
				Programs: []ProgramConfig{
					{
						Name:     "json-program",
						Command:  "ls",
						Args:     []string{"-la"},
						Interval: "1m",
					},
				},
			},
		},
		{
			name:     "minimal config",
			filename: "minimal.yaml",
			content: `programs:
  - name: "minimal"
    command: "echo"`,
			expected: &Config{
				Programs: []ProgramConfig{
					{
						Name:    "minimal",
						Command: "echo",
					},
				},
			},
		},
		{
			name:        "file not found",
			filename:    "nonexistent.yaml",
			wantErr:     true,
			errContains: "failed to read config file",
		},
		{
			name:     "invalid YAML",
			filename: "invalid.yaml",
			content: `programs:
  - name: "test"
    command: "echo"
    invalid_yaml: [unclosed`,
			wantErr:     true,
			errContains: "failed to parse YAML config",
		},
		{
			name:     "invalid JSON",
			filename: "invalid.json",
			content: `{
  "programs": [
    {
      "name": "test",
      "command": "echo",
      "invalid": json
    }
  ]
}`,
			wantErr:     true,
			errContains: "failed to parse JSON config",
		},
		{
			name:        "unsupported format",
			filename:    "config.txt",
			content:     "some content",
			wantErr:     true,
			errContains: "unsupported config file format",
		},
		{
			name:        "empty programs",
			filename:    "empty.yaml",
			content:     "programs: []",
			wantErr:     true,
			errContains: "no programs defined",
		},
		{
			name:     "missing program name",
			filename: "no-name.yaml",
			content: `programs:
  - command: "echo"`,
			wantErr:     true,
			errContains: "missing name",
		},
		{
			name:     "missing program command",
			filename: "no-command.yaml",
			content: `programs:
  - name: "test"`,
			wantErr:     true,
			errContains: "missing command",
		},
		{
			name:     "duplicate program names",
			filename: "duplicate.yaml",
			content: `programs:
  - name: "test"
    command: "echo"
  - name: "test"
    command: "ls"`,
			wantErr:     true,
			errContains: "duplicate program name",
		},
		{
			name:     "invalid interval",
			filename: "bad-interval.yaml",
			content: `programs:
  - name: "test"
    command: "echo"
    interval: "invalid"`,
			wantErr:     true,
			errContains: "invalid interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tempFile string
			if tt.content != "" {
				// Create temporary file
				tmpDir := t.TempDir()
				tempFile = filepath.Join(tmpDir, tt.filename)
				err := os.WriteFile(tempFile, []byte(tt.content), 0644)
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
			} else {
				// Use non-existent file path
				tempFile = tt.filename
			}

			config, err := LoadConfig(tempFile)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadConfig() expected error but got none")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Fatalf(
						"LoadConfig() error = %q, want error containing %q",
						err.Error(),
						tt.errContains,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadConfig() unexpected error = %v", err)
				return
			}

			if !configsEqual(config, tt.expected) {
				t.Fatalf("LoadConfig() = %+v, want %+v", config, tt.expected)
			}
		})
	}
}

func TestProgramConfig_GetInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		interval    string
		expected    time.Duration
		wantErr     bool
		errContains string
	}{
		{
			name:     "empty interval",
			interval: "",
			expected: 0,
		},
		{
			name:     "valid seconds",
			interval: "30s",
			expected: 30 * time.Second,
		},
		{
			name:     "valid minutes",
			interval: "5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "valid hours",
			interval: "2h",
			expected: 2 * time.Hour,
		},
		{
			name:     "complex duration",
			interval: "1h30m45s",
			expected: 1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name:        "invalid interval",
			interval:    "invalid",
			wantErr:     true,
			errContains: "time: invalid duration",
		},
		{
			name:     "negative interval",
			interval: "-5s",
			expected: -5 * time.Second, // time.ParseDuration allows negative durations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pc := &ProgramConfig{Interval: tt.interval}

			duration, err := pc.GetInterval()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetInterval() expected error but got none")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Fatalf(
						"GetInterval() error = %q, want error containing %q",
						err.Error(),
						tt.errContains,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetInterval() unexpected error = %v", err)
			}

			if duration != tt.expected {
				t.Fatalf("GetInterval() = %v, want %v", duration, tt.expected)
			}
		})
	}
}

func TestProgramConfig_GetInitialDelay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		interval    string
		expected    time.Duration
		wantErr     bool
		errContains string
	}{
		{
			name:     "empty interval",
			interval: "",
			expected: 0,
		},
		{
			name:     "valid seconds",
			interval: "30s",
			expected: 30 * time.Second,
		},
		{
			name:     "valid minutes",
			interval: "5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "valid hours",
			interval: "2h",
			expected: 2 * time.Hour,
		},
		{
			name:     "complex duration",
			interval: "1h30m45s",
			expected: 1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name:        "invalid interval",
			interval:    "invalid",
			wantErr:     true,
			errContains: "time: invalid duration",
		},
		{
			name:     "negative interval",
			interval: "-5s",
			expected: -5 * time.Second, // time.ParseDuration allows negative durations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pc := &ProgramConfig{InitialDelay: tt.interval}

			duration, err := pc.GetInitialDelay()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetInitialDelay() expected error but got none")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Fatalf(
						"GetInitialDelay() error = %q, want error containing %q",
						err.Error(),
						tt.errContains,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetInitialDelay() unexpected error = %v", err)
			}

			if duration != tt.expected {
				t.Fatalf("GetInitialDelay() = %v, want %v", duration, tt.expected)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test1", Command: "echo"},
					{Name: "test2", Command: "ls"},
				},
			},
		},
		{
			name:        "no programs",
			config:      &Config{Programs: []ProgramConfig{}},
			wantErr:     true,
			errContains: "no programs defined",
		},
		{
			name: "missing program name",
			config: &Config{
				Programs: []ProgramConfig{
					{Command: "echo"},
				},
			},
			wantErr:     true,
			errContains: "missing name",
		},
		{
			name: "missing program command",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test"},
				},
			},
			wantErr:     true,
			errContains: "missing command",
		},
		{
			name: "duplicate program names",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test", Command: "echo"},
					{Name: "test", Command: "ls"},
				},
			},
			wantErr:     true,
			errContains: "duplicate program name",
		},
		{
			name: "invalid interval",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test", Command: "echo", Interval: "invalid"},
				},
			},
			wantErr:     true,
			errContains: "invalid interval",
		},
		{
			name: "valid interval",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test", Command: "echo", Interval: "30s"},
				},
			},
		},
		{
			name: "invalid deadline",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test", Command: "echo", Deadline: "invalid"},
				},
			},
			wantErr:     true,
			errContains: "invalid deadline",
		},
		{
			name: "valid deadline",
			config: &Config{
				Programs: []ProgramConfig{
					{Name: "test", Command: "echo", Deadline: "5m"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error but got none")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Fatalf(
						"Validate() error = %q, want error containing %q",
						err.Error(),
						tt.errContains,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error = %v", err)
			}
		})
	}
}

// Helper functions

func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(len(substr) == 0 ||
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())
}

func configsEqual(a, b *Config) bool {
	if len(a.Programs) != len(b.Programs) {
		return false
	}

	for i, progA := range a.Programs {
		progB := b.Programs[i]
		if !programConfigsEqual(progA, progB) {
			return false
		}
	}

	return true
}

func programConfigsEqual(a, b ProgramConfig) bool {
	if a.Name != b.Name || a.Command != b.Command || a.Interval != b.Interval ||
		a.KeepStdinOpen != b.KeepStdinOpen || a.Stdout != b.Stdout || a.Stderr != b.Stderr {
		return false
	}

	if !stringSlicesEqual(a.Args, b.Args) || !stringSlicesEqual(a.Env, b.Env) {
		return false
	}

	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestProgramConfig_GetBufferSizeLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		bufferSizeLimit string
		expected        int
	}{
		{"empty", "", 0},
		{"bytes", "512B", 512},
		{"kilobytes", "2KB", 2048},
		{"megabytes", "3MB", 3145728},
		{"gigabytes", "1GB", 1073741824},
		{"terabytes", "1TB", 1099511627776},
		{"zero", "0B", 0},
		{"invalid format", "invalid", 0},
		{"no unit", "1000", 0},
		{"negative", "-5MB", 0},
		{"decimal", "1.5MB", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pc := &ProgramConfig{BufferSizeLimit: tt.bufferSizeLimit}
			result := pc.GetBufferSizeLimit()
			if result != tt.expected {
				t.Fatalf("GetBufferSizeLimit() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestProgramConfig_GetDeadline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		deadline    string
		expected    time.Duration
		wantErr     bool
		errContains string
	}{
		{"empty deadline", "", 0, false, ""},
		{"valid seconds", "30s", 30 * time.Second, false, ""},
		{"valid minutes", "5m", 5 * time.Minute, false, ""},
		{"valid hours", "2h", 2 * time.Hour, false, ""},
		{"complex duration", "1h30m45s", 1*time.Hour + 30*time.Minute + 45*time.Second, false, ""},
		{"invalid deadline", "invalid", 0, true, "time: invalid duration"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pc := &ProgramConfig{Deadline: tt.deadline}

			duration, err := pc.GetDeadline()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetDeadline() expected error but got none")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Fatalf(
						"GetDeadline() error = %q, want error containing %q",
						err.Error(),
						tt.errContains,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetDeadline() unexpected error = %v", err)
			}

			if duration != tt.expected {
				t.Fatalf("GetDeadline() = %v, want %v", duration, tt.expected)
			}
		})
	}
}

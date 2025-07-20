package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Programs []ProgramConfig `yaml:"programs" json:"programs"`
}

type ProgramConfig struct {
	Name          string   `yaml:"name"                      json:"name"`
	Command       string   `yaml:"command"                   json:"command"`
	Args          []string `yaml:"args,omitempty"            json:"args,omitempty"`
	Env           []string `yaml:"env,omitempty"             json:"env,omitempty"`
	Interval      string   `yaml:"interval,omitempty"        json:"interval,omitempty"`
	KeepStdinOpen bool     `yaml:"keep_stdin_open,omitempty" json:"keep_stdin_open,omitempty"`
	Stdout        string   `yaml:"stdout,omitempty"          json:"stdout,omitempty"`
	Stderr        string   `yaml:"stderr,omitempty"          json:"stderr,omitempty"`
}

func (pc *ProgramConfig) GetInterval() (time.Duration, error) {
	if pc.Interval == "" {
		return 0, nil
	}
	return time.ParseDuration(pc.Interval)
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config file format: %s (supported: .yaml, .yml, .json)", ext)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if len(c.Programs) == 0 {
		return errors.New("no programs defined in config")
	}

	programNames := make(map[string]bool)
	for i, program := range c.Programs {
		if program.Name == "" {
			return fmt.Errorf("program at index %d missing name", i)
		}
		if program.Command == "" {
			return fmt.Errorf("program %s missing command", program.Name)
		}
		if programNames[program.Name] {
			return fmt.Errorf("duplicate program name: %s", program.Name)
		}
		programNames[program.Name] = true

		if program.Interval != "" {
			if _, err := time.ParseDuration(program.Interval); err != nil {
				return fmt.Errorf("invalid interval for program %s: %w", program.Name, err)
			}
		}
	}

	return nil
}

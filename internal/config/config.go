package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Programs []ProgramConfig `yaml:"programs" json:"programs"`
}

type ProgramConfig struct {
	Name            string   `yaml:"name"                        json:"name"`
	Command         string   `yaml:"command"                     json:"command"`
	Args            []string `yaml:"args,omitempty"              json:"args,omitempty"`
	Env             []string `yaml:"env,omitempty"               json:"env,omitempty"`
	Interval        string   `yaml:"interval,omitempty"          json:"interval,omitempty"`
	InitialDelay    string   `yaml:"initial_delay,omitempty"     json:"initial_delay,omitempty"`
	KeepStdinOpen   bool     `yaml:"keep_stdin_open,omitempty"   json:"keep_stdin_open,omitempty"`
	Stdout          string   `yaml:"stdout,omitempty"            json:"stdout,omitempty"`
	Stderr          string   `yaml:"stderr,omitempty"            json:"stderr,omitempty"`
	BufferSizeLimit string   `yaml:"buffer_size_limit,omitempty" json:"buffer_size_limit,omitempty"`
}

func (pc *ProgramConfig) GetInterval() (time.Duration, error) {
	if pc.Interval == "" {
		return 0, nil
	}
	return time.ParseDuration(pc.Interval)
}

func (pc *ProgramConfig) GetInitialDelay() (time.Duration, error) {
	if pc.InitialDelay == "" {
		return 0, nil
	}
	return time.ParseDuration(pc.InitialDelay)
}

var sizeRegex = regexp.MustCompile(`^(?P<amount>\d+)(?P<unit>B|KB|MB|GB|TB)$`)

func (pc *ProgramConfig) GetBufferSizeLimit() int {
	if pc.BufferSizeLimit == "" {
		return 0
	}

	match := sizeRegex.FindStringSubmatch(pc.BufferSizeLimit)

	if len(match) > 0 {
		amount := match[1]
		unit := match[2]

		var multiplier int64
		switch unit {
		case "B":
			multiplier = 1
		case "KB":
			multiplier = 1024
		case "MB":
			multiplier = 1024 * 1024
		case "GB":
			multiplier = 1024 * 1024 * 1024
		case "TB":
			multiplier = 1024 * 1024 * 1024 * 1024
		default:
			multiplier = 1
		}

		v64, err := strconv.ParseInt(amount, 10, 64)
		if err != nil {
			return 0
		}
		return int(v64 * multiplier)
	}

	return 0
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
		if err = yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err = json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config file format: %s (supported: .yaml, .yml, .json)", ext)
	}

	if err = config.Validate(); err != nil {
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

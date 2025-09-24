# Meeseeks

<div align="center">
  <img src="https://raw.githubusercontent.com/GustavoCaso/meeseeks/refs/heads/main/images/logo.png" alt="Meeseeks Logo" width="80%" height="auto" style="border-radius:20%">
</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/GustavoCaso/meeseeks)](https://goreportcard.com/report/github.com/GustavoCaso/meeseeks)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/GustavoCaso/meeseeks)](https://golang.org/)

A simple and lightweight process manager for Go applications. Meeseeks can be used both as a standalone CLI tool for managing processes and as a reusable Go package for embedding process management into your applications.

## Features

- **Dual Usage**: CLI tool and Go package
- **Process Management**: Start, stop, and monitor multiple processes
- **Daemon Mode**: Run processes in the background with Docker Compose-like commands
- **Auto-Start at Login**: Cross-platform service management for macOS, Linux (TODO), and Windows (TODO)
- **Configuration Files**: YAML and JSON support
- **Scheduled Execution**: Run processes at intervals
- **Real-time Monitoring**: Process status, logs, and statistics
- **Output Redirection**: Capture or redirect stdout/stderr
- **Graceful Shutdown**: Context-based cancellation and signal handling
- **Security-First**: Input validation, privilege minimization, and secure defaults

## Installation

### As a CLI Tool

```bash
go install github.com/GustavoCaso/meeseeks/cmd/meeseeks@latest
```

### As a Go Package

```bash
go get github.com/GustavoCaso/meeseeks
```

## Quick Start

### CLI Usage

1. **Create a configuration file** (`config.yaml`):

```yaml
programs:
  - name: "web-server"
    command: "python"
    args: ["-m", "http.server", "8080"]
    
  - name: "health-check"
    command: "curl"
    args: ["http://localhost:8080"]
    interval: "30s"
```

2. **Start in detached mode**:

```bash
meeseeks start -d -config config.yaml
```

3. **Check status**:

```bash
meeseeks status
meeseeks status web-server
```

5. **Run particular program**

```bash
meeseeks run health-check
```

4. **View logs**:

```bash
meeseeks logs web-server
```

5. **Stop processes**:

```bash
meeseeks stop web-server  # Stop specific program
meeseeks stop             # Stop all programs
```

6. **Configure auto-start at login**:

```bash
meeseeks start-at-login enable
meeseeks start-at-login status
meeseeks start-at-login disable
```

### Go Package Usage

```go
package main

import (
    "context"
    "time"
    
    "github.com/GustavoCaso/meeseeks/pkg/meeseeks"
    "github.com/GustavoCaso/meeseeks/pkg/program"
)

func main() {
    // Create a new meeseeks instance
    m := meeseeks.New()
    interval := 30*time.Second

    // Add programs
    webServer := program.New("web-server", "python", 
        program.Args("-m", "http.server", "8080"),
        programs.BufferSizeLimit(3 * 1024 * 1024) // 3MB
    )
    
    healthCheck := program.New("health-check", "curl", 
      program.Args("http://localhost:8080")
    )

    m.AddProgram(meeseeks.NewProgram(webServer))
    m.AddProgram(meeseeks.NewProgram(healthCheck, &interval))

    // Start all programs
    ctx := context.Background()
    m.Start(ctx)

    // Wait for completion or cancellation
    m.Wait(ctx)
    
    // Print statistics
    for _, stat := range m.Statistics() {
        fmt.Printf("Program: %s, Runs: %d, Successful: %d, Failed: %d\n", 
            stat.ProgramName, stat.TotalRuns, stat.Successful, stat.Failed)
    }
}
```

## Configuration

### File Formats

Meeseeks supports both YAML and JSON configuration files. Format is automatically detected by file extension.

### Configuration Schema

```yaml
programs:
  - name: "unique-program-name"          # Required: Unique identifier
    command: "executable"                # Required: Command to run
    args: ["arg1", "arg2"]              # Optional: Command arguments
    env: ["VAR=value"]                  # Optional: Environment variables
    interval: "30s"                     # Optional: Run every interval (e.g., "1m", "30s")
    keep_stdin_open: true               # Optional: Keep stdin open for input (default: false)
    stdout: "/path/to/stdout.log"       # Optional: Redirect stdout to file
    stderr: "/path/to/stderr.log"       # Optional: Redirect stderr to file
    buffer_size_limit: "1MB"            # Optional: Limit memory usage for output buffers
```

#### Buffer Size Limits

Control memory usage by limiting output buffer sizes. When the limit is reached, older output is discarded and a truncation message is added.

**Valid formats:** `512B`, `2KB`, `1MB`, `1GB`, `1TB`
**Default:** Unlimited (if not specified)
**Recommended:** `1MB` for most applications

### Example Configurations

#### Web Application with Monitoring

```yaml
programs:
  - name: "api-server"
    command: "go"
    args: ["run", "main.go"]
    env: ["PORT=8080", "ENV=production"]
    stdout: "/var/log/api.log"
    stderr: "/var/log/api.error.log"
    
  - name: "health-monitor"
    command: "curl"
    args: ["-f", "http://localhost:8080/health"]
    interval: "60s"
    
  - name: "log-rotator"
    command: "logrotate"
    args: ["/etc/logrotate.conf"]
    interval: "24h"
```

#### Development Environment

```yaml
programs:
  - name: "frontend"
    command: "npm"
    args: ["run", "dev"]
    
  - name: "backend"
    command: "go"
    args: ["run", "main.go"]
    env: ["DEBUG=true"]
    
  - name: "database"
    command: "docker"
    args: ["run", "--rm", "-p", "5432:5432", "postgres:13"]
```

## Environment Variables

Meeseeks uses a single environment variable to simplify configuration:

| Variable | Default | Description |
|----------|---------|-------------|
| `MEESEEKS_CONFIG_DIR` | `~/.meeseeks` | Base directory for all configuration and runtime files |

When `MEESEEKS_CONFIG_DIR` is set, all meeseeks files are placed in this directory:
- `config.yaml` - Default configuration file
- `meeseeks.sock` - Unix socket for daemon IPC  
- `meeseeks.pid` - PID file for process tracking
- `meeseeks.log` - Daemon internal log file

### Examples

```bash
# Use custom config directory - all files go here
export MEESEEKS_CONFIG_DIR="/opt/meeseeks"
meeseeks start

# Use default value ~/.meeseeks/config.yaml
meeseeks start -d
```

## CLI Commands

### `meeseeks start`

Start programs from a configuration file.

```bash
meeseeks start -config config.yaml        # Run in foreground with specific config
meeseeks start                            # Run in foreground with default config
meeseeks start -d                         # Run in detached mode
```

**Options:**
- `-config <file>`: Path to configuration file (defaults to $MEESEEKS_CONFIG_DIR/config.yaml or ~/.meeseeks/config.yaml)
- `-d`: Run in detached mode (daemon)

### `meeseeks status`

Show status of running programs.

```bash
meeseeks status                          # Show all programs (table format)
meeseeks status web-server               # Show specific program (table format)
meeseeks status -format json             # Show all programs in JSON format
meeseeks status -f json web-server       # Show specific program in JSON format
```

**Options:**
- `-format <format>`: Output format: table (default), json
- `-f <format>`: Output format shorthand

### `meeseeks logs`

Show logs for a specific program.

```bash
meeseeks logs web-server
```

### `meeseeks stop`

Stop running programs.

```bash
meeseeks stop web-server               # Stop specific program
meeseeks stop web-server -timeout 10s  # Stop specifc program with custom timeout
```

### `meeseeks exit`

Stop and programs and meeseks process. Useful to stop meeseeks when running in detached mode

```bash
meeseeks exit
```

### `meeseeks start-at-login`

Manage automatic startup of meeseeks at user login across platforms.

```bash
# Enable auto-start
meeseeks start-at-login enable

# Check current status
meeseeks start-at-login status

# Disable auto-start
meeseeks start-at-login disable
```

**Platform Support:**
- **macOS**: Creates LaunchAgent plist files
- **Linux**: TODO
- **Windows**: TODO

**Security Features:**
- Runs only in user context (no admin privileges required)
- Input validation prevents injection attacks
- Secure file permissions and directory restrictions

### `meeseeks version`

Show version information.

```bash
meeseeks version
```

## Program Options

When using Meeseeks as a Go package, you can configure programs with various options:

```go
program.New("name", "command",
    program.Args("arg1", "arg2"),           // Command arguments
    program.Envs("VAR=value"),              // Environment variables
    program.KeepStdinOpen(),                // Keep stdin open
    program.Stdout(file),                   // Redirect stdout
    program.Stderr(file),                   // Redirect stderr
    program.Stdin(reader),                  // Provide stdin input
    program.BufferSizeLimit(1024 * 1024),   // Limit output buffers (1MB)
)
```

## Use Cases

### Development Environment

Replace complex Docker Compose setups for local development:

```yaml
programs:
  - name: "database"
    command: "docker"
    args: ["run", "--rm", "-p", "5432:5432", "postgres:13"]
    
  - name: "redis"
    command: "redis-server"
    
  - name: "api"
    command: "go"
    args: ["run", "cmd/api/main.go"]
    env: ["DB_URL=postgres://localhost:5432/mydb"]
```

### Production Monitoring

Simple process supervision with auto-start capability:

```yaml
programs:
  - name: "app"
    command: "./myapp"
    stdout: "/var/log/app.log"
    stderr: "/var/log/app.error.log"
    
  - name: "health-check"
    command: "curl"
    args: ["-f", "http://localhost:8080/health"]
    interval: "30s"
    
  - name: "metrics-collector"
    command: "./collect-metrics"
    interval: "5m"
```

**Enable auto-start at login:**
```bash
# Configure meeseeks to start automatically
meeseeks start-at-login enable

# Verify it's working
meeseeks start-at-login status
```

### Scheduled Tasks

Cron-like functionality with better process management:

```yaml
programs:
  - name: "backup"
    command: "rsync"
    args: ["-av", "/data/", "/backup/"]
    interval: "6h"
    
  - name: "cleanup"
    command: "find"
    args: ["/tmp", "-type", "f", "-mtime", "+7", "-delete"]
    interval: "24h"
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Run linting: `golangci-lint run`
6. Submit a pull request

## License

MIT License - see LICENSE file for details.

## Similar Projects

- **PM2**: Process manager for Node.js applications
- **Supervisor**: Process control system for Unix-like systems
- **Docker Compose**: Container orchestration (inspiration for CLI design)
- **Foreman**: Process manager inspired by Heroku's process model

Meeseeks aims to provide similar functionality with Go's simplicity and performance, suitable for both development and lightweight production environments.

# Meeseeks

A simple and lightweight process manager for Go applications. Meeseeks can be used both as a standalone CLI tool for managing processes and as a reusable Go package for embedding process management into your applications.

## Features

- **Dual Usage**: CLI tool and Go package
- **Process Management**: Start, stop, and monitor multiple processes
- **Daemon Mode**: Run processes in the background with Docker Compose-like commands
- **Configuration Files**: YAML and JSON support
- **Scheduled Execution**: Run processes at intervals
- **Real-time Monitoring**: Process status, logs, and statistics
- **Output Redirection**: Capture or redirect stdout/stderr
- **Graceful Shutdown**: Context-based cancellation and signal handling

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

2. **Run in daemon mode**:

```bash
meeseeks run -d -config config.yaml
```

3. **Check status**:

```bash
meeseeks status
meeseeks status web-server
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

    // Add programs
    webServer := program.New("web-server", "python", 
        program.Args("-m", "http.server", "8080"),
    )
    
    healthCheck := program.New("health-check", "curl",
        program.Args("http://localhost:8080"),
        program.Interval(30*time.Second),
    )

    m.AddProgram(webServer)
    m.AddProgram(healthCheck)

    // Start all programs
    ctx := context.Background()
    m.Start(ctx)

    // Wait for completion or cancellation
    m.Wait(ctx)
    
    // Print statistics
    for _, stat := range m.Statistics() {
        fmt.Println(stat.String())
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
```

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

## CLI Commands

### `meeseeks run`

Start programs from a configuration file.

```bash
meeseeks run -config config.yaml        # Run in foreground
meeseeks run -d -config config.yaml     # Run in daemon mode (detached)
```

**Options:**
- `-config <file>`: Path to configuration file (required)
- `-d`: Run in detached mode (daemon)

### `meeseeks status`

Show status of running programs.

```bash
meeseeks status                 # Show all programs
meeseeks status web-server      # Show specific program
```

### `meeseeks logs`

Show logs for a specific program.

```bash
meeseeks logs web-server
```

### `meeseeks stop`

Stop running programs.

```bash
meeseeks stop web-server        # Stop specific program
meeseeks stop                   # Stop all programs and daemon
```

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
    program.Interval(30*time.Second),       // Run every interval
    program.KeepStdinOpen(),                // Keep stdin open
    program.Stdout(file),                   // Redirect stdout
    program.Stderr(file),                   // Redirect stderr
    program.Stdin(reader),                  // Provide stdin input
)
```

## Architecture

Meeseeks follows a modular architecture with clear separation of concerns:

- **`pkg/program`**: Individual process execution and management
- **`pkg/meeseeks`**: Central process manager coordinating multiple programs
- **`pkg/config`**: Configuration file parsing and validation
- **`pkg/daemon`**: Daemon mode with Unix socket IPC for CLI communication
- **`cmd/meeseeks`**: CLI application entry point

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

Simple process supervision and monitoring:

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

## Dependencies

- **Go 1.21+**
- **Runtime**: `gopkg.in/yaml.v3` (YAML support)
- **Development**: `github.com/golangci/golangci-lint/v2` (linting)

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

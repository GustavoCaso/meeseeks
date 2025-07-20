package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Daemon struct {
	meeseeks meeseeks.Meeseek
	listener net.Listener
	sockPath string
	mu       sync.RWMutex
	running  bool
}

type Request struct {
	Command string                 `json:"command"`
	Args    map[string]interface{} `json:"args,omitempty"`
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func New(sockPath string) *Daemon {
	return &Daemon{
		meeseeks: meeseeks.New(),
		sockPath: sockPath,
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return fmt.Errorf("daemon already running")
	}

	if err := os.RemoveAll(d.sockPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(d.sockPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	listener, err := net.Listen("unix", d.sockPath)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}

	d.listener = listener
	d.running = true

	go d.acceptConnections(ctx)
	return nil
}

func (d *Daemon) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	d.running = false
	if d.listener != nil {
		_ = d.listener.Close()
	}
	_ = os.RemoveAll(d.sockPath)
	return nil
}

func (d *Daemon) AddProgram(prog program.Program) error {
	return d.meeseeks.AddProgram(prog)
}

func (d *Daemon) StartPrograms(ctx context.Context) {
	d.meeseeks.Start(ctx)
}

func (d *Daemon) Wait(ctx context.Context) error {
	return d.meeseeks.Wait(ctx)
}

func (d *Daemon) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := d.listener.Accept()
			if err != nil {
				if d.running {
					continue
				}
				return
			}
			go d.handleConnection(conn)
		}
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req Request
	if err := decoder.Decode(&req); err != nil {
		_ = encoder.Encode(Response{
			Success: false,
			Error:   fmt.Sprintf("failed to decode request: %v", err),
		})
		return
	}

	resp := d.handleRequest(req)
	_ = encoder.Encode(resp)
}

func (d *Daemon) handleRequest(req Request) Response {
	switch req.Command {
	case "status":
		programName, _ := req.Args["program"].(string)
		if programName == "" {
			stats := d.meeseeks.Statistics()
			return Response{Success: true, Data: stats}
		}

		status, err := d.meeseeks.Status(programName)
		if err != nil {
			return Response{Success: false, Error: err.Error()}
		}
		return Response{Success: true, Data: status}

	case "logs":
		programName, ok := req.Args["program"].(string)
		if !ok {
			return Response{Success: false, Error: "program name required"}
		}

		stats := d.meeseeks.Statistics()
		for _, stat := range stats {
			if stat.ProgramName == programName {
				return Response{Success: true, Data: map[string]interface{}{
					"last_output": stat.LastOutput,
					"last_error":  stat.LastError,
				}}
			}
		}
		return Response{Success: false, Error: "program not found"}

	case "stop":
		return Response{Success: false, Error: "stop command not yet implemented"}

	default:
		return Response{Success: false, Error: "unknown command"}
	}
}

func GetSocketPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meeseeks", "meeseeks.sock")
}

func GetPidFile() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meeseeks", "meeseeks.pid")
}

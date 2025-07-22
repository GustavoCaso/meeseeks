package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Server struct {
	meeseeks meeseeks.Meeseek
	server   *http.Server
	sockPath string
	mu       sync.RWMutex
	running  bool
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func New(sockPath string) *Server {
	s := &Server{
		meeseeks: meeseeks.New(),
		sockPath: sockPath,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/stop", s.handleStop)

	s.server = &http.Server{
		Handler: mux,
	}

	return s
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return errors.New("server already running")
	}

	if err := os.RemoveAll(s.sockPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.sockPath), 0750); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	listener, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}

	s.running = true

	go func() {
		err := s.server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			// Log error but don't fail startup
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	errs := []error{}

	s.running = false

	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.server.Shutdown(ctx)
		errs = append(errs, err)
	}

	errs = append(errs, os.RemoveAll(s.sockPath))
	errs = append(errs, s.meeseeks.Kill())
	return errors.Join(errs...)
}

func (s *Server) AddProgram(prog program.Program) error {
	return s.meeseeks.AddProgram(prog)
}

func (s *Server) StartPrograms(ctx context.Context) {
	s.meeseeks.Start(ctx)
}

func (s *Server) Wait(ctx context.Context) error {
	return s.meeseeks.Wait(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	programName := r.URL.Query().Get("program")

	var resp Response
	if programName == "" {
		stats := s.meeseeks.Statistics()
		resp = Response{Success: true, Data: stats}
	} else {
		status, err := s.meeseeks.Status(programName)
		if err != nil {
			resp = Response{Success: false, Error: err.Error()}
		} else {
			resp = Response{Success: true, Data: status}
		}
	}

	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	programName := r.URL.Query().Get("program")
	if programName == "" {
		resp := Response{Success: false, Error: "program name required"}
		json.NewEncoder(w).Encode(resp)
		return
	}

	stats := s.meeseeks.Statistics()
	for _, stat := range stats {
		if stat.ProgramName == programName {
			resp := Response{Success: true, Data: map[string]interface{}{
				"last_output": stat.LastOutput,
				"last_error":  stat.LastError,
			}}
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	resp := Response{Success: false, Error: "program not found"}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := Response{Success: false, Error: "stop command not yet implemented"}
	json.NewEncoder(w).Encode(resp)
}

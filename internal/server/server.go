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

	"github.com/GustavoCaso/meeseeks/internal/config"
	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
)

type Server struct {
	meeseeks   meeseeks.Meeseek
	server     *http.Server
	sockPath   string
	configPath string
	mu         sync.RWMutex
	running    bool
	logger     *logger.Logger
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func New(sockPath string, configPath string, logger *logger.Logger) (*Server, error) {
	s := &Server{
		meeseeks:   meeseeks.New(meeseeks.Logger(logger)),
		sockPath:   sockPath,
		configPath: configPath,
		logger:     logger,
	}

	err := s.loadConfig()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/statistics", s.handleStatistics)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/stop", s.handleStop)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	return s, nil
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

	s.meeseeks.Start(ctx)

	go func() {
		if err = s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Fatal("Failed to start server", "error", err)
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
	errs = append(errs, s.meeseeks.Shutdown(5*time.Second))
	return errors.Join(errs...)
}

func (s *Server) Wait(ctx context.Context) error {
	return s.meeseeks.Wait(ctx)
}

func (s *Server) loadConfig() error {
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for _, programConfig := range cfg.Programs {
		prog, programErr := createProgramFromConfig(programConfig, s.logger)
		if programErr != nil {
			return fmt.Errorf("failed to create program %s: %w", programConfig.Name, programErr)
		}

		var p meeseeks.Program

		// Check if this program has an interval - pass it to AddProgram
		if programConfig.Interval != "" {
			interval, intervalErr := programConfig.GetInterval()
			if intervalErr != nil {
				return fmt.Errorf("failed to parse interval for program %s: %w", programConfig.Name, intervalErr)
			}
			p = meeseeks.NewProgram(prog, &interval)
		} else {
			p = meeseeks.NewProgram(prog, nil)
		}

		if addErr := s.meeseeks.AddProgram(p); addErr != nil {
			return fmt.Errorf("failed to add scheduled program %s: %w", programConfig.Name, addErr)
		}
	}

	return nil
}

func (s *Server) handleStatistics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	programName := r.URL.Query().Get("program")

	var resp Response
	if programName == "" {
		stats := s.meeseeks.Statistics()
		resp = Response{Success: true, Data: stats}
	} else {
		stats, err := s.meeseeks.Statistic(programName)
		if err != nil {
			resp = Response{Success: false, Error: err.Error()}
		} else {
			resp = Response{Success: true, Data: stats}
		}
	}
	handleResponse(w, resp)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	programName := r.URL.Query().Get("program")
	if programName == "" {
		resp := Response{Success: false, Error: "program name required"}
		handleResponse(w, resp)
	}

	stats := s.meeseeks.Statistics()
	stat, exits := stats[programName]
	if !exits {
		resp := Response{Success: false, Error: "program not found"}
		handleResponse(w, resp)
		return
	}

	resp := Response{Success: true, Data: map[string]interface{}{
		"last_output": stat.LastOutput,
		"last_error":  stat.LastError,
	}}

	handleResponse(w, resp)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	programName := r.URL.Query().Get("program")
	timeoutString := r.URL.Query().Get("timeout")
	if programName == "" {
		resp := Response{Success: false, Error: "program name required"}
		handleResponse(w, resp)
	}

	duration, err := time.ParseDuration(timeoutString)
	if err != nil {
		resp := Response{Success: false, Error: fmt.Sprintf("error parsing timeout %s. %s", timeoutString, err.Error())}
		handleResponse(w, resp)
	}

	err = s.meeseeks.Stop(programName, duration)
	if err != nil {
		resp := Response{Success: false, Error: err.Error()}
		handleResponse(w, resp)
	}
	resp := Response{Success: true, Data: fmt.Sprintf("%s stopped", programName)}
	handleResponse(w, resp)
}

func handleResponse(w http.ResponseWriter, resp Response) {
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		resp = Response{Success: false, Error: err.Error()}
		nestedError := json.NewEncoder(w).Encode(resp)
		if nestedError != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "500 - %s", nestedError.Error())
		}
	}
}

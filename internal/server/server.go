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
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Server struct {
	meeseeks   meeseeks.Meeseek
	server     *http.Server
	sockPath   string
	configPath string
	mu         sync.RWMutex
	running    bool
	logger     *logger.Logger
	timeout    time.Duration
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func New(sockPath string, configPath string, logger *logger.Logger, timeout time.Duration) (*Server, error) {
	s := &Server{
		meeseeks:   meeseeks.New(meeseeks.Logger(logger)),
		sockPath:   sockPath,
		configPath: configPath,
		logger:     logger,
		timeout:    timeout,
	}

	err := s.loadConfig()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/statistics", s.handleStatistics)
	mux.HandleFunc("/reload", s.handleReload)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/follow-logs", s.handleFollowLogs)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/run-program", s.handleRunProgram)

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
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "unix", s.sockPath)
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
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		defer cancel()
		err := s.server.Shutdown(ctx)
		errs = append(errs, err)
	}

	errs = append(errs, os.RemoveAll(s.sockPath))
	errs = append(errs, s.meeseeks.Shutdown(s.timeout))
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
			return programErr
		}

		if addErr := s.meeseeks.AddProgram(prog); addErr != nil {
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

//nolint:gocognit //The complexity is acceptable
func (s *Server) handleFollowLogs(w http.ResponseWriter, r *http.Request) {
	programName := r.URL.Query().Get("program")

	if programName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		resp := Response{Success: false, Error: "program name required"}
		handleResponse(w, resp)
		return
	}

	ctx := r.Context()
	channel, err := s.meeseeks.SubscribeLogs(ctx, programName)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		resp := Response{Success: false, Error: err.Error()}
		handleResponse(w, resp)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	// Since we do not know how often programs log
	// we want to keep the connection open as long as the user want.
	_ = rc.SetWriteDeadline(time.Time{})

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-channel:
			if !ok {
				return
			}
			if len(msg.Message) > 0 {
				data, dataErr := json.Marshal(map[string]interface{}{
					"message":  msg.Message,
					"is_error": msg.IsError,
				})
				if dataErr != nil {
					return
				}
				_, err = fmt.Fprintf(w, "data: %s\n\n", data)
				if err != nil {
					return
				}
				err = rc.Flush()
				if err != nil {
					return
				}
			}
		}
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	programName := r.URL.Query().Get("program")
	if programName == "" {
		resp := Response{Success: false, Error: "program name required"}
		handleResponse(w, resp)
		return
	}

	stats, err := s.meeseeks.Statistic(programName)
	if err != nil {
		resp := Response{Success: false, Error: "program not found"}
		handleResponse(w, resp)
		return
	}

	resp := Response{Success: true, Data: map[string]interface{}{
		"output": stats.Output,
		"error":  stats.Error,
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
		return
	}

	duration, err := time.ParseDuration(timeoutString)
	if err != nil {
		resp := Response{Success: false, Error: fmt.Sprintf("error parsing timeout %s. %s", timeoutString, err.Error())}
		handleResponse(w, resp)
		return
	}

	err = s.meeseeks.Stop(programName, duration)
	if err != nil {
		resp := Response{Success: false, Error: err.Error()}
		handleResponse(w, resp)
		return
	}
	resp := Response{Success: true, Data: fmt.Sprintf("%s stopped", programName)}
	handleResponse(w, resp)
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	timeoutString := r.URL.Query().Get("timeout")

	duration, err := time.ParseDuration(timeoutString)
	if err != nil {
		resp := Response{Success: false, Error: fmt.Sprintf("error parsing timeout %s. %s", timeoutString, err.Error())}
		handleResponse(w, resp)
		return
	}

	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		resp := Response{Success: false, Error: fmt.Sprintf("failed to load config: %s", err.Error())}
		handleResponse(w, resp)
		return
	}

	programs := []program.Program{}

	for _, programConfig := range cfg.Programs {
		prog, programErr := createProgramFromConfig(programConfig, s.logger)
		if programErr != nil {
			resp := Response{
				Success: false,
				Error: fmt.Sprintf(
					"failed to create program from configuration %s: %s",
					programConfig.Name,
					programErr.Error(),
				),
			}
			handleResponse(w, resp)
			return
		}

		programs = append(programs, prog)
	}

	s.meeseeks.Reload(context.Background(), programs, duration)
	resp := Response{Success: true, Data: "meeseek configuration reloaded!"}
	handleResponse(w, resp)
}

func (s *Server) handleRunProgram(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	programName := r.URL.Query().Get("program")
	if programName == "" {
		resp := Response{Success: false, Error: "program name required"}
		handleResponse(w, resp)
		return
	}

	err := s.meeseeks.Run(programName)

	if err != nil {
		resp := Response{
			Success: false,
			Error:   fmt.Sprintf("error executing program '%s'. %s", programName, err.Error()),
		}
		handleResponse(w, resp)
		return
	}

	resp := Response{Success: true}
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

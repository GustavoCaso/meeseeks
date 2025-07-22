package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Meeseek interface {
	AddProgram(program.Program) error
	Start(ctx context.Context)
	Results(w io.Writer)
	Status(program string) (string, error)
	Wait(ctx context.Context) error
	Statistics() []program.Statistics
	Kill() error
}

type meeseek struct {
	startTime time.Time
	endTime   time.Time
	programs  map[string]program.Program
	wg        *sync.WaitGroup
	mu        sync.RWMutex
}

func (m *meeseek) AddProgram(p program.Program) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.programs[p.Name()]; ok {
		return fmt.Errorf("duplicated %s program", p.Name())
	}

	m.programs[p.Name()] = p
	return nil
}

func (m *meeseek) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.startTime = time.Now()
	m.wg.Add(len(m.programs))

	for _, p := range m.programs {
		go func(prog program.Program) {
			done, err := prog.Start(ctx)
			if err != nil {
				slog.Error("failed to start program", "program", prog.Name(), "error", err.Error())
			}
			<-done
			m.wg.Done()
		}(p)
	}
}

func (m *meeseek) Results(w io.Writer) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fmt.Fprintln(w, "=== Meeseeks Execution Summary ===")

	if m.endTime.IsZero() {
		fmt.Fprintln(w, "Execution still in progress")
		fmt.Fprintln(w, "Program Statuses (may still be running):")
	} else {
		executionTime := m.endTime.Sub(m.startTime)
		fmt.Fprintf(w, "Total Execution Time: %s\n", executionTime)
		fmt.Fprintln(w, "Program Statuses:")
	}

	for _, p := range m.programs {
		fmt.Fprintf(w, "  - %s\n", p.Status())
		if errMsg := p.Error(); errMsg != "" {
			fmt.Fprintf(w, "    Error: %s\n", errMsg)
		}
	}
}

func (m *meeseek) Statistics() []program.Statistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statistics := []program.Statistics{}

	for _, p := range m.programs {
		statistics = append(statistics, p.Statistics())
	}

	return statistics
}

func (m *meeseek) Status(program string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.programs[program]; !ok {
		return "", fmt.Errorf("program %s not present", program)
	}

	return m.programs[program].Status(), nil
}

func (m *meeseek) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return errors.New("context cancelled while waiting for programs to finalize")
	case <-done:
		m.endTime = time.Now()
		return nil
	}
}

func (m *meeseek) Kill() error {
	errs := []error{}

	for _, p := range m.programs {
		errs = append(errs, p.Kill())
	}

	return errors.Join(errs...)
}

func New() Meeseek {
	return &meeseek{
		wg:       &sync.WaitGroup{},
		programs: map[string]program.Program{},
	}
}

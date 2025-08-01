package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Meeseek interface {
	AddProgram(program.Program) error
	Start(ctx context.Context)
	Stop(programName string, timeout time.Duration) error
	Wait(ctx context.Context) error
	Statistic(program string) (program.Statistics, error)
	Statistics() []program.Statistics
	Shutdown(timeout time.Duration) error
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

func (m *meeseek) Statistic(programName string) (program.Statistics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.programs[programName]; !ok {
		return program.Statistics{}, fmt.Errorf("program %s not present", programName)
	}

	return m.programs[programName].Statistics(), nil
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

func (m *meeseek) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	defer func() {
		m.endTime = time.Now()
	}()

	select {
	case <-ctx.Done():
		return errors.New("context cancelled while waiting for programs to finalize")
	case <-done:
		return nil
	}
}

func (m *meeseek) Stop(programName string, timeout time.Duration) error {
	if _, ok := m.programs[programName]; !ok {
		return fmt.Errorf("program %s not present", programName)
	}

	return m.programs[programName].Shutdown(timeout)
}

func (m *meeseek) Shutdown(timeout time.Duration) error {
	errs := []error{}

	for _, p := range m.programs {
		errs = append(errs, p.Shutdown(timeout))
	}

	return errors.Join(errs...)
}

func New() Meeseek {
	return &meeseek{
		wg:       &sync.WaitGroup{},
		programs: map[string]program.Program{},
	}
}

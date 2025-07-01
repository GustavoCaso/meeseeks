package meeseek

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/program"
)

type Meeseek interface {
	AddProgram(program.Program) error
	Start(ctx context.Context)
	Results(w io.Writer)
	Status(program string) (string, error)
	Wait()
}

type meeseek struct {
	startTime time.Time
	endTime   time.Time
	programs  map[string]program.Program
	wg        *sync.WaitGroup
}

func (m *meeseek) AddProgram(p program.Program) error {
	if _, ok := m.programs[p.Name()]; ok {
		return fmt.Errorf("duplicated %s program", p.Name())
	}

	m.programs[p.Name()] = p
	return nil
}

func (m *meeseek) Start(ctx context.Context) {
	m.startTime = time.Now()
	m.wg.Add(len(m.programs))

	for _, p := range m.programs {
		go func() {
			defer m.wg.Done()

			err := p.Start(ctx)
			if err != nil {
				log.Printf("failed to start program '%s' error '%s'\n", p.Name(), err)
			}
		}()
	}
}

func (m *meeseek) Results(w io.Writer) {
	executionTime := m.endTime.Sub(m.startTime)

	fmt.Fprintln(w, "=== Meeseeks Execution Summary ===")
	fmt.Fprintf(w, "Total Execution Time: %s\n", executionTime)
	fmt.Fprintln(w, "Program Statuses:")

	for name, p := range m.programs {
		fmt.Fprintf(w, "  - %s: %s\n", name, p.Status())
		if errMsg := p.Error(); errMsg != "" {
			fmt.Fprintf(w, "    Error: %s\n", errMsg)
		}
	}
}

func (m *meeseek) Status(program string) (string, error) {
	if _, ok := m.programs[program]; !ok {
		return "", fmt.Errorf("program %s not present", program)
	}

	return m.programs[program].Status(), nil
}

func (m *meeseek) Wait() {
	m.wg.Wait()
	m.endTime = time.Now()
}

func New() Meeseek {
	return &meeseek{
		wg:       &sync.WaitGroup{},
		programs: map[string]program.Program{},
	}
}

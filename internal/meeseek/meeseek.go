package meeseek

import (
	"context"
	"log"
	"sync"

	"github.com/GustavoCaso/meeseeks/internal/program"
)

type Meeseek interface {
	AddProgram(program.Program)
	Start(ctx context.Context)
	Status() []string
	Wait()
}

type meeseek struct {
	programs []program.Program
	wg       *sync.WaitGroup
}

func (m *meeseek) AddProgram(p program.Program) {
	m.programs = append(m.programs, p)
}

func (m *meeseek) Start(ctx context.Context) {
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

func (m *meeseek) Status() []string {
	var status []string
	for _, p := range m.programs {
		status = append(status, p.Status())
	}
	return status
}

func (m *meeseek) Wait() {
	m.wg.Wait()
}

func New() Meeseek {
	return &meeseek{
		wg: &sync.WaitGroup{},
	}
}

package meeseek

import (
	"github.com/GustavoCaso/meeseeks/internal/program"
)

type Meeseek interface {
	AddProgram(program.Program)
	Start()
}

type meeseek struct {
	programs []program.Program
}

func (m *meeseek) AddProgram(p program.Program) {
	m.programs = append(m.programs, p)
}

func (m *meeseek) Start() {
	for _, p := range m.programs {
		p.Start()
	}
}

func New() Meeseek {
	return &meeseek{}
}

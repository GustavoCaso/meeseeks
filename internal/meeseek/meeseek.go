package meeseek

import (
	"github.com/GustavoCaso/meeseeks/internal/program"
)

type Meeseek interface {
	AddProgram(program.Program)
	Start()
}

func New() Meeseek {
	return nil
}

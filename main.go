package main

import (
	"github.com/GustavoCaso/meeseeks/internal/meeseek"
	"github.com/GustavoCaso/meeseeks/internal/program"
)

func main() {
	meeseek := meeseek.New()

	meeseek.AddProgram(program.New())

	meeseek.Start()
}

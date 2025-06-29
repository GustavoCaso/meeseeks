package main

import (
	"os"

	"github.com/GustavoCaso/meeseeks/internal/meeseek"
	"github.com/GustavoCaso/meeseeks/internal/program"
)

func main() {
	meeseek := meeseek.New()

	meeseek.AddProgram(program.New("ls", program.Args("-la"), program.Output(os.Stdout)))

	meeseek.Start()
}

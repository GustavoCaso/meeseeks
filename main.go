package main

import (
	"fmt"

	"github.com/GustavoCaso/meeseeks/internal/meeseek"
	"github.com/GustavoCaso/meeseeks/internal/program"
)

func main() {
	meeseek := meeseek.New()

	meeseek.AddProgram(program.New("ls command", "ls", program.Args("-la")))
	meeseek.AddProgram(program.LongRunning("print command", "bash", program.Args("-c", "for i in {1..5}; do echo \"Iteration $i\"; sleep 1; done")))

	meeseek.Start()

	meeseek.Wait()

	for _, s := range meeseek.Status() {
		fmt.Println(s)
	}
}

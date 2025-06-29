package program

import (
	"io"
	"log"
	"os"
	"os/exec"
)

type Program interface {
	Start()
}

type Option func(*program)

var Output = func(o io.Writer) Option {
	return func(p *program) {
		p.output = o
	}
}

var Args = func(args ...string) Option {
	return func(p *program) {
		p.arguments = args
	}
}

type program struct {
	command   string
	arguments []string
	output    io.Writer
	daemon    bool
}

func (p *program) Start() {
	cmd := exec.Command(p.command, p.arguments...)
	cmd.Env = os.Environ()
	cmd.Stdout = p.output
	var err error
	if p.daemon {
		err = cmd.Start()
	} else {
		err = cmd.Run()
	}
	if err != nil {
		log.Fatal(err)
	}
}

func New(command string, opts ...Option) Program {
	p := &program{
		command: command,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

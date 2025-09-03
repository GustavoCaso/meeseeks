package server

import (
	"github.com/GustavoCaso/meeseeks/internal/config"
	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func createProgramFromConfig(pc config.ProgramConfig, logger *logger.Logger) program.Program {
	var opts []program.Option

	if len(pc.Args) > 0 {
		opts = append(opts, program.Args(pc.Args...))
	}

	if len(pc.Env) > 0 {
		opts = append(opts, program.Envs(pc.Env...))
	}

	if pc.KeepStdinOpen {
		opts = append(opts, program.KeepStdinOpen())
	}

	if pc.Stdout != "" {
		opts = append(opts, program.StdoutFile(pc.Stdout))
	}

	if pc.Stderr != "" {
		opts = append(opts, program.StderrFile(pc.Stderr))
	}

	opts = append(opts, program.Logger(logger))

	return program.New(pc.Name, pc.Command, opts...)
}

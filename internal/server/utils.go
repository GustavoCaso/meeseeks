package server

import (
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/config"
	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func createProgramFromConfig(pc config.ProgramConfig, logger *logger.Logger) (program.Program, error) {
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
		file, err := os.OpenFile(pc.Stdout, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open stdout file %s: %w", pc.Stdout, err)
		}
		opts = append(opts, program.Stdout(file))
	}

	if pc.Stderr != "" {
		file, err := os.OpenFile(pc.Stderr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open stderr file %s: %w", pc.Stderr, err)
		}
		opts = append(opts, program.Stderr(file))
	}

	opts = append(opts, program.Logger(logger))

	return program.New(pc.Name, pc.Command, opts...), nil
}

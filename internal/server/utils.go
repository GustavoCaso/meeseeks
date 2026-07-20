package server

import (
	"github.com/GustavoCaso/meeseeks/internal/config"
	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func createProgramFromConfig(
	pc config.ProgramConfig,
	logger *logger.Logger,
) (program.Program, error) {
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

	bufferSizeLimit, err := pc.GetBufferSizeLimit()
	if err != nil {
		return nil, err
	}
	opts = append(opts, program.BufferSizeLimit(bufferSizeLimit))

	if pc.OnSuccessCallback != "" {
		opts = append(opts, program.OnSuccess(&program.Callback{
			Command: pc.OnSuccessCallback,
			Args:    pc.OnSuccessCallbackArgs,
		}))
	}

	if pc.OnFailureCallback != "" {
		opts = append(opts, program.OnFailure(&program.Callback{
			Command: pc.OnFailureCallback,
			Args:    pc.OnFailureCallbackArgs,
		}))
	}

	opts = append(opts, program.Logger(logger))

	interval, err := pc.GetInterval()
	if err != nil {
		return nil, err
	}
	opts = append(opts, program.Interval(interval))

	initialDelay, err := pc.GetInitialDelay()
	if err != nil {
		return nil, err
	}
	opts = append(opts, program.InitialDelay(initialDelay))

	if pc.RetryCount > 0 {
		opts = append(opts, program.RetryCount(pc.RetryCount))
	}

	retryDelay, err := pc.GetRetryDelay()
	if err != nil {
		return nil, err
	}
	opts = append(opts, program.RetryDelay(retryDelay))

	deadline, err := pc.GetDeadline()
	if err != nil {
		return nil, err
	}
	opts = append(opts, program.Deadline(deadline))

	return program.New(pc.Name, pc.Command, opts...), nil
}

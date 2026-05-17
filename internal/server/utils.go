package server

import (
	"os"
	"os/exec"

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

	if pc.OnSuccess != "" {
		opts = append(opts, program.OnSuccess(func(programName string) {
			runCallbackCommand(pc.OnSuccess, programName, "success", nil, logger)
		}))
	}

	if pc.OnFailure != "" {
		opts = append(opts, program.OnFailure(func(programName string, callbackErr error) {
			runCallbackCommand(pc.OnFailure, programName, "failure", callbackErr, logger)
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

func runCallbackCommand(
	command string,
	programName string,
	status string,
	callbackErr error,
	logger *logger.Logger,
) {
	cmd := exec.Command("sh", "-c", command)
	env := append(
		os.Environ(),
		"MEESEEKS_PROGRAM="+programName,
		"MEESEEKS_STATUS="+status,
	)

	if callbackErr != nil {
		env = append(env, "MEESEEKS_ERROR="+callbackErr.Error())
	}

	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		if logger != nil {
			logger.Error(
				"callback command failed",
				"program",
				programName,
				"status",
				status,
				"error",
				err.Error(),
				"output",
				string(output),
			)
		}
		return
	}

	if logger != nil && len(output) > 0 {
		logger.Info(
			"callback command output",
			"program",
			programName,
			"status",
			status,
			"output",
			string(output),
		)
	}
}

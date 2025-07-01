package program

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type Program interface {
	LongRunning() bool
	Name() string
	Start(ctx context.Context) error
	Status() string
	LastLine() string
	Output() string
	Error() string
}

type ProcessState int

const (
	StateNotStarted ProcessState = iota
	StateRunning
	StateFinished
	StateError
)

type Option func(*program)

var Stdout = func(o io.Writer) Option {
	return func(p *program) {
		p.customStdout = o
	}
}

var Stderr = func(o io.Writer) Option {
	return func(p *program) {
		p.customStderr = o
	}
}

var Args = func(args ...string) Option {
	return func(p *program) {
		p.arguments = args
	}
}

type program struct {
	cmd       *exec.Cmd
	name      string
	command   string
	arguments []string

	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	customStdout io.Writer
	customStderr io.Writer

	state    ProcessState
	exitCode int

	stdoutLock   sync.RWMutex
	stderrLock   sync.RWMutex
	wg           sync.WaitGroup
	outputBuffer strings.Builder
	errorBuffer  strings.Builder
	lastError    string
	lastLine     string

	daemon bool
}

func (p *program) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, p.command, p.arguments...)
	cmd.Env = os.Environ()
	p.cmd = cmd

	if p.daemon {
		return p.startDaemon()
	} else {
		return p.oneShot()
	}
}

func (p *program) oneShot() error {
	defer func() {
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}()

	var err error

	if p.customStdout == nil && p.customStderr == nil {

		output, err := p.cmd.CombinedOutput()
		if err != nil {
			p.state = StateError
			p.errorBuffer.Write(output)
			return err
		}
		p.outputBuffer.Write(output)
	} else if p.customStdout == nil {

		var stdout bytes.Buffer
		p.cmd.Stdout = &stdout
		p.cmd.Stderr = p.customStderr

		err = p.cmd.Run()
		if err != nil {
			p.state = StateError
			return err
		}
		p.outputBuffer.Write(stdout.Bytes())
	} else if p.customStderr == nil {

		var stderr bytes.Buffer
		p.cmd.Stdout = p.customStdout
		p.cmd.Stderr = &stderr

		err = p.cmd.Run()
		if err != nil {
			p.state = StateError
			p.errorBuffer.Write(stderr.Bytes())
			return err
		}
	} else {
		p.cmd.Stdout = p.customStdout
		p.cmd.Stderr = p.customStderr

		err = p.cmd.Run()
		if err != nil {
			p.state = StateError
			return err
		}
	}

	p.state = StateFinished
	return nil
}

func (p *program) startDaemon() error {
	stdin, err := p.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return err
	}

	p.stdout = stdout
	p.stdin = stdin
	p.stderr = stderr

	err = p.cmd.Start()
	if err != nil {
		return err
	}
	p.state = StateRunning
	p.monitorProcess()
	return nil
}

func (p *program) monitorProcess() {
	p.wg.Add(2)

	go func() {
		defer p.wg.Done()
		defer p.stdout.Close()
		p.readOutput(p.stdout, false)
	}()

	go func() {
		defer p.wg.Done()
		defer p.stderr.Close()
		p.readOutput(p.stderr, true)
	}()

	err := p.cmd.Wait()
	if err != nil {
		p.stderrLock.Lock()
		p.errorBuffer.WriteString(err.Error())
		p.errorBuffer.WriteString("\n")
		p.lastError = err.Error()
		p.stderrLock.Unlock()
		p.state = StateError
	} else {
		p.state = StateFinished
	}
	p.exitCode = p.cmd.ProcessState.ExitCode()
}

func (p *program) readOutput(reader io.ReadCloser, isError bool) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()

		if isError {
			p.stderrLock.Lock()
			p.errorBuffer.WriteString(line)
			p.errorBuffer.WriteString("\n")
			p.lastError = line
			p.stderrLock.Unlock()
		} else {
			p.stderrLock.Lock()
			p.outputBuffer.WriteString(line)
			p.outputBuffer.WriteString("\n")
			p.lastLine = line
			p.stderrLock.Unlock()
		}
	}
}

func (p *program) LongRunning() bool {
	return p.daemon
}

func (p *program) Name() string {
	return p.name
}

func (p *program) Status() string {
	switch p.state {
	case StateRunning:
		return fmt.Sprintf("[%s] running, pid: %d, last line: %s", p.name, p.cmd.Process.Pid, p.LastLine())
	case StateFinished:
		return fmt.Sprintf("[%s] finished with exit code: %d", p.name, p.exitCode)
	case StateError:
		return fmt.Sprintf("[%s] error code: %d", p.name, p.exitCode)
	default:
		return fmt.Sprintf("[%s] not started", p.name)
	}
}

func (p *program) Output() string {
	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()
	return p.outputBuffer.String()
}

func (p *program) LastLine() string {
	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()
	return p.lastLine
}

func (p *program) Error() string {
	p.stderrLock.RLock()
	defer p.stderrLock.RUnlock()
	return p.errorBuffer.String()
}

func New(name, command string, opts ...Option) Program {
	p := &program{
		name:    name,
		command: command,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func LongRunning(name, command string, opts ...Option) Program {
	p := &program{
		name:    name,
		command: command,
		daemon:  true,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

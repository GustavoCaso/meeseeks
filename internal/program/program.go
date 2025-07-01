package program

import (
	"bufio"
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
	State() ProcessState
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

var Stdin = func(o io.Reader) Option {
	return func(p *program) {
		p.customStdin = o
	}
}

var Args = func(args ...string) Option {
	return func(p *program) {
		p.arguments = args
	}
}

var Envs = func(envs ...string) Option {
	return func(p *program) {
		p.customEnv = envs
	}
}

type program struct {
	cmd       *exec.Cmd
	name      string
	command   string
	arguments []string
	daemon    bool

	customStdout io.Writer
	customStderr io.Writer
	customStdin  io.Reader
	customEnv    []string

	state    ProcessState
	exitCode int

	stdoutLock   sync.RWMutex
	stderrLock   sync.RWMutex
	outputBuffer strings.Builder
	errorBuffer  strings.Builder
	lastError    string
	lastLine     string
}

func (p *program) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, p.command, p.arguments...)
	cmd.Env = append(os.Environ(), p.customEnv...)
	p.cmd = cmd

	outReader, outWriter := io.Pipe()
	errReader, errWriter := io.Pipe()

	if p.customStdout != nil {
		p.cmd.Stdout = io.MultiWriter(p.customStdout, outWriter)
	} else {
		p.cmd.Stdout = outWriter
	}

	if p.customStderr != nil {
		p.cmd.Stderr = io.MultiWriter(p.customStderr, errWriter)
	} else {
		p.cmd.Stderr = errWriter
	}

	if p.customStdin != nil {
		p.cmd.Stdin = p.customStdin
	}

	if p.daemon {
		return p.startDaemon(outReader, errReader, outWriter, errWriter)
	} else {
		return p.oneShot(outReader, errReader, outWriter, errWriter)
	}
}

func (p *program) oneShot(outReader io.ReadCloser, errReader io.ReadCloser, outWriter io.WriteCloser, errWriter io.WriteCloser) error {
	defer func() {
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}()

	// WaitGroup ensures readOutput goroutines finish reading all data.
	// This prevents race conditions where callers might see incomplete output.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		p.readOutput(context.Background(), outReader, false)
	}()

	go func() {
		defer wg.Done()
		p.readOutput(context.Background(), errReader, true)
	}()

	err := p.cmd.Run()

	outWriter.Close()
	errWriter.Close()

	// Wait for readers to finish processing all data
	wg.Wait()

	outReader.Close()
	errReader.Close()

	if err != nil {
		p.state = StateError
		return err
	}

	p.state = StateFinished
	return nil
}

func (p *program) startDaemon(outReader io.ReadCloser, errReader io.ReadCloser, outWriter io.WriteCloser, errWriter io.WriteCloser) error {
	err := p.cmd.Start()
	if err != nil {
		return err
	}
	p.state = StateRunning
	p.monitorProcess(outReader, errReader, outWriter, errWriter)
	return nil
}

func (p *program) monitorProcess(outReader io.ReadCloser, errReader io.ReadCloser, outWriter io.WriteCloser, errWriter io.WriteCloser) {
	ctx, cancel := context.WithCancel(context.Background())

	go p.readOutput(ctx, outReader, false)
	go p.readOutput(ctx, errReader, true)

	err := p.cmd.Wait()

	// Close writers first so readers receive EOF
	outWriter.Close()
	errWriter.Close()

	// Cancel context to signal readers to stop
	cancel()

	outReader.Close()
	errReader.Close()

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

func (p *program) readOutput(ctx context.Context, reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil && err != io.EOF {
					if isError {
						p.stderrLock.Lock()
						p.errorBuffer.WriteString("Scanner error: " + err.Error())
						p.errorBuffer.WriteString("\n")
						p.stderrLock.Unlock()
					}
				}
				return
			}
			line := scanner.Text()

			if isError {
				p.stderrLock.Lock()
				p.errorBuffer.WriteString(line + "\n")
				p.lastError = line
				p.stderrLock.Unlock()
			} else {
				p.stdoutLock.Lock()
				p.outputBuffer.WriteString(line + "\n")
				p.lastLine = line
				p.stdoutLock.Unlock()
			}
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

func (p *program) State() ProcessState {
	return p.state
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

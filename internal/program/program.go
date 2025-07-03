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
	Async() bool
	Name() string
	Start(ctx context.Context) error
	Status() string
	Done() bool
	Send([]byte) error
	CloseStdin() error
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

var KeepStdinOpen = func() Option {
	return func(p *program) {
		p.keepStdinOpen = true
	}
}

var Async = func() Option {
	return func(p *program) {
		p.async = true
	}
}

type program struct {
	cmd       *exec.Cmd
	name      string
	command   string
	arguments []string
	async     bool

	customStdout  io.Writer
	customStderr  io.Writer
	customStdin   io.Reader
	keepStdinOpen bool
	customEnv     []string

	state    ProcessState
	exitCode int

	stdoutLock   sync.RWMutex
	stderrLock   sync.RWMutex
	outputBuffer strings.Builder
	errorBuffer  strings.Builder
	lastError    string
	lastLine     string

	pipes *pipes
}

type pipes struct {
	outReader *io.PipeReader
	outWriter *io.PipeWriter
	errReader *io.PipeReader
	errWriter *io.PipeWriter
	inReader  *io.PipeReader
	inWriter  *io.PipeWriter
}

func (p *pipes) closeWriters() {
	p.outWriter.Close()
	p.errWriter.Close()
	p.inWriter.Close()
}

func (p *pipes) closeReaders() {
	p.outReader.Close()
	p.errReader.Close()
	p.inReader.Close()
}

func (p *program) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, p.command, p.arguments...)
	cmd.Env = append(os.Environ(), p.customEnv...)
	p.cmd = cmd

	outReader, outWriter := io.Pipe()
	errReader, errWriter := io.Pipe()
	inReader, inWriter := io.Pipe()

	pipes := &pipes{
		outReader: outReader,
		outWriter: outWriter,
		errReader: errReader,
		errWriter: errWriter,
		inReader:  inReader,
		inWriter:  inWriter,
	}

	p.pipes = pipes

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
		p.cmd.Stdin = io.MultiReader(p.customStdin, inReader)
	} else {
		p.cmd.Stdin = inReader
	}

	if !p.keepStdinOpen {
		inWriter.Close()
	}

	if p.async {
		return p.runAsync()
	} else {
		return p.run()
	}
}

func (p *program) run() error {
	defer func() {
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}()
	p.state = StateRunning

	// WaitGroup ensures readOutput goroutines finish reading all data.
	// This prevents race conditions where callers might see incomplete output.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		p.readOutput(p.pipes.outReader, false)
	}()

	go func() {
		defer wg.Done()
		p.readOutput(p.pipes.errReader, true)
	}()

	err := p.cmd.Run()

	p.pipes.closeWriters()

	// Wait for readers to finish processing all data
	wg.Wait()

	p.pipes.closeReaders()

	if err != nil {
		p.errorBuffer.WriteString(err.Error())
		p.errorBuffer.WriteString("\n")
		p.lastError = err.Error()
		p.state = StateError
		return err
	}

	p.state = StateFinished
	return nil
}

func (p *program) runAsync() error {
	err := p.cmd.Start()
	if err != nil {
		return err
	}
	p.state = StateRunning
	go p.monitorProcess()
	return nil
}

func (p *program) monitorProcess() {
	defer func() {
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}()

	// WaitGroup ensures readOutput goroutines finish reading all data.
	// This prevents race conditions where callers might see incomplete output.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		p.readOutput(p.pipes.outReader, false)
	}()

	go func() {
		defer wg.Done()
		p.readOutput(p.pipes.errReader, true)
	}()

	err := p.cmd.Wait()

	p.pipes.closeWriters()

	// Wait for readers to finish processing all data
	wg.Wait()

	p.pipes.closeReaders()

	if err != nil {
		p.errorBuffer.WriteString(err.Error())
		p.errorBuffer.WriteString("\n")
		p.lastError = err.Error()
		p.state = StateError
	} else {
		p.state = StateFinished
	}
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
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

	if err := scanner.Err(); err != nil && err != io.EOF {
		if isError {
			p.stderrLock.Lock()
			p.errorBuffer.WriteString("Scanner error: " + err.Error())
			p.errorBuffer.WriteString("\n")
			p.stderrLock.Unlock()
		}
	}
}

func (p *program) Send(data []byte) error {
	if p.state != StateRunning {
		return fmt.Errorf("can not send data to a non-running program")
	}

	if !p.keepStdinOpen {
		return fmt.Errorf("to send data to a running please use the KeepStdinOpen option when initialazing the program")
	}

	_, err := p.pipes.inWriter.Write(data)
	return err
}

func (p *program) CloseStdin() error {
	if p.state != StateRunning {
		return fmt.Errorf("closing stdin of non-running process has no effect")
	}

	if !p.keepStdinOpen {
		return fmt.Errorf("stding is already closed please KeepStdinOpen option when initialazing the program to have full control over stdin")
	}

	return p.pipes.inWriter.Close()
}

func (p *program) Async() bool {
	return p.async
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

func (p *program) Done() bool {
	return p.state == StateFinished || p.state == StateError
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

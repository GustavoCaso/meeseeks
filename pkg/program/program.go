package program

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/logger"
)

type Program interface {
	Async() bool
	Name() string
	Start(ctx context.Context) (<-chan struct{}, error)
	Send([]byte) error
	CloseStdin() error
	LastLine() string
	Output() string
	Error() string
	State() ProcessState
	String() string
	Shutdown(timeout time.Duration) error
}

type ProcessState int

const (
	StateNotStarted ProcessState = iota
	StateRunning
	StateFinished
	StateError
)

//nolint:gochecknoglobals // This gloabls is convinient
var StateToString = map[ProcessState]string{
	StateNotStarted: "not started",
	StateRunning:    "running",
	StateFinished:   "finished",
	StateError:      "error",
}

type Option func(*program)

func Stdout(o io.Writer) Option {
	return func(p *program) {
		p.customStdout = o
	}
}

func Stderr(o io.Writer) Option {
	return func(p *program) {
		p.customStderr = o
	}
}

func Stdin(o io.Reader) Option {
	return func(p *program) {
		p.customStdin = o
	}
}

func Args(args ...string) Option {
	return func(p *program) {
		p.arguments = args
	}
}

func Envs(envs ...string) Option {
	return func(p *program) {
		p.customEnv = envs
	}
}

func KeepStdinOpen() Option {
	return func(p *program) {
		p.keepStdinOpen = true
	}
}

func Async() Option {
	return func(p *program) {
		p.async = true
	}
}

func Logger(logger logger.Logger) Option {
	return func(p *program) {
		p.logger = logger
	}
}

type program struct {
	cmd       *exec.Cmd
	name      string
	command   string
	arguments []string
	async     bool
	done      chan struct{}

	customStdout  io.Writer
	customStderr  io.Writer
	customStdin   io.Reader
	keepStdinOpen bool
	customEnv     []string

	state        ProcessState
	exitCode     int
	outputBuffer strings.Builder
	errorBuffer  strings.Builder
	lastError    string
	lastLine     string

	dataLock sync.RWMutex
	cmdLock  sync.Mutex

	pipes  *pipes
	logger logger.Logger
}

type pipes struct {
	outReader *io.PipeReader
	outWriter *io.PipeWriter
	errReader *io.PipeReader
	errWriter *io.PipeWriter
	inReader  *io.PipeReader
	inWriter  *io.PipeWriter
}

func (p *pipes) closeWriters() error {
	err := p.outWriter.Close()
	if err != nil {
		return err
	}
	err = p.errWriter.Close()
	if err != nil {
		return err
	}
	err = p.inWriter.Close()
	if err != nil {
		return err
	}
	return nil
}

func (p *pipes) closeReaders() error {
	err := p.outReader.Close()
	if err != nil {
		return err
	}
	err = p.errReader.Close()
	if err != nil {
		return err
	}
	err = p.inReader.Close()
	if err != nil {
		return err
	}
	return nil
}

func (p *program) Start(ctx context.Context) (<-chan struct{}, error) {
	return p.start(ctx)
}

func (p *program) signalDone() {
	close(p.done)
}

func (p *program) start(ctx context.Context) (<-chan struct{}, error) {
	//nolint:gosec // We accept the arguments the users have manually defined
	cmd := exec.CommandContext(
		ctx,
		p.command,
		p.arguments...)
	cmd.Env = append(os.Environ(), p.customEnv...)

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
		cmd.Stdout = io.MultiWriter(p.customStdout, outWriter)
	} else {
		cmd.Stdout = outWriter
	}

	if p.customStderr != nil {
		cmd.Stderr = io.MultiWriter(p.customStderr, errWriter)
	} else {
		cmd.Stderr = errWriter
	}

	if p.customStdin != nil {
		cmd.Stdin = io.MultiReader(p.customStdin, inReader)
	} else {
		cmd.Stdin = inReader
	}

	p.cmdLock.Lock()
	p.cmd = cmd
	p.cmdLock.Unlock()

	if !p.keepStdinOpen {
		_ = inWriter.Close()
	}

	p.done = make(chan struct{}, 1)

	return p.done, p.run()
}

func (p *program) run() error {
	p.cmdLock.Lock()
	err := p.cmd.Start()
	p.cmdLock.Unlock()

	if err != nil {
		p.dataLock.Lock()

		p.errorBuffer.WriteString(err.Error())
		p.errorBuffer.WriteString("\n")
		p.lastError = err.Error()

		p.state = StateError
		p.dataLock.Unlock()
		p.signalDone()
		return err
	}

	p.dataLock.Lock()
	p.state = StateRunning
	p.dataLock.Unlock()

	if p.async {
		go p.monitorProcess()
		return nil
	}

	p.monitorProcess()
	return nil
}

func (p *program) monitorProcess() {
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

	p.cmdLock.Lock()
	cmd := p.cmd
	p.cmdLock.Unlock()
	err := cmd.Wait()

	writersErr := p.pipes.closeWriters()

	if writersErr != nil {
		if p.logger != nil {
			p.logger.Error(
				"error closing writers",
				"program",
				p.name,
				"error",
				writersErr.Error(),
			)
		}
	}

	// Wait for readers to finish processing all data
	wg.Wait()

	readersErr := p.pipes.closeReaders()

	if readersErr != nil {
		if p.logger != nil {
			p.logger.Error(
				"error closing readers",
				"program",
				p.name,
				"error",
				readersErr.Error(),
			)
		}
	}

	p.exitCode = cmd.ProcessState.ExitCode()

	p.dataLock.Lock()
	if err != nil {
		p.errorBuffer.WriteString(err.Error())
		p.errorBuffer.WriteString("\n")
		p.lastError = err.Error()
		p.state = StateError
	} else {
		p.state = StateFinished
	}
	p.dataLock.Unlock()

	p.signalDone()
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		p.dataLock.Lock()
		if isError {
			p.errorBuffer.WriteString(line + "\n")
			p.lastError = line
		} else {
			p.outputBuffer.WriteString(line + "\n")
			p.lastLine = line
		}
		p.dataLock.Unlock()
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if isError {
			p.dataLock.Lock()
			p.errorBuffer.WriteString("Scanner error: " + err.Error())
			p.errorBuffer.WriteString("\n")
			p.dataLock.Unlock()
		}
	}
}

func (p *program) Send(data []byte) error {
	p.dataLock.RLock()
	canSend := p.state == StateRunning
	p.dataLock.RUnlock()

	if !canSend {
		return errors.New("can not send data to a non-running program")
	}

	if !p.keepStdinOpen {
		return errors.New("to send data to a running please use the KeepStdinOpen option when initialazing the program")
	}

	_, err := p.pipes.inWriter.Write(data)
	return err
}

func (p *program) CloseStdin() error {
	p.dataLock.RLock()
	canClose := p.state == StateRunning
	p.dataLock.RUnlock()

	if !canClose {
		return errors.New("closing stdin of non-running process has no effect")
	}

	if !p.keepStdinOpen {
		return errors.New(
			"stding is already closed please KeepStdinOpen option when initialazing the program to have full control over stdin",
		)
	}

	return p.pipes.inWriter.Close()
}

func (p *program) Async() bool {
	return p.async
}

func (p *program) Name() string {
	return p.name
}

func (p *program) Output() string {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()

	return p.outputBuffer.String()
}

func (p *program) LastLine() string {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()

	return p.lastLine
}

func (p *program) Error() string {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()

	return p.errorBuffer.String()
}

func (p *program) State() ProcessState {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()
	return p.state
}

func (p *program) Shutdown(timeout time.Duration) error {
	p.cmdLock.Lock()
	if p.cmd == nil || p.cmd.Process == nil {
		p.cmdLock.Unlock()
		return nil
	}

	// Send SIGTERM for graceful shutdown
	err := p.cmd.Process.Signal(syscall.SIGTERM)
	p.cmdLock.Unlock()
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		// If SIGTERM fails, fall back to force kill
		return p.forcekill()
	}

	// Wait for the existing monitoring to handle process exit
	select {
	case <-p.done:
		return nil
	case <-time.After(timeout):
		// Timeout exceeded, force kill
		return p.forcekill()
	}
}

func (p *program) String() string {
	return fmt.Sprintf("%s [%s %s]", p.name, p.command, strings.Join(p.arguments, " "))
}

func (p *program) forcekill() error {
	p.cmdLock.Lock()
	if p.cmd == nil || p.cmd.Process == nil {
		p.cmdLock.Unlock()
		return nil
	}

	err := p.cmd.Process.Kill()
	p.cmdLock.Unlock()
	p.dataLock.Lock()
	p.state = StateError
	p.dataLock.Unlock()

	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}

	return nil
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

package program

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	StateCancelled
)

//nolint:gochecknoglobals // This gloabls is convinient
var StateToString = map[ProcessState]string{
	StateNotStarted: "not started",
	StateRunning:    "running",
	StateFinished:   "finished",
	StateError:      "error",
	StateCancelled:  "cancelled",
}

type Option func(*program)

func StdoutFile(file string) Option {
	return func(p *program) {
		p.stdoutFile = file
	}
}

func StderrFile(file string) Option {
	return func(p *program) {
		p.stderrFile = file
	}
}

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

func BufferSizeLimit(limit int) Option {
	return func(p *program) {
		p.bufferLimit = limit
	}
}

type program struct {
	cmd       *exec.Cmd
	name      string
	command   string
	arguments []string
	async     bool
	done      chan struct{}

	customStdout io.Writer
	customStderr io.Writer
	customStdin  io.Reader
	stdoutFile   string
	stderrFile   string

	keepStdinOpen bool
	customEnv     []string

	state        ProcessState
	exitCode     int
	outputBuffer strings.Builder
	errorBuffer  strings.Builder
	bufferLimit  int

	dataLock sync.RWMutex
	cmdLock  sync.Mutex

	pipes      *pipes
	logger     logger.Logger
	finalizers []func() error
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

func (p *program) finalize() {
	for _, finalizer := range p.finalizers {
		err := finalizer()
		if err != nil {
			if p.logger != nil {
				p.logger.Warn("error when executing finalizers", "error", err.Error())
			}
		}
	}

	close(p.done)
}

func (p *program) Start(ctx context.Context) (<-chan struct{}, error) {
	cmd, err := p.setupCmd(ctx)

	if err != nil {
		done := make(chan struct{}, 1)
		close(done)
		return done, err
	}

	p.cmdLock.Lock()
	p.cmd = cmd
	p.cmdLock.Unlock()

	p.done = make(chan struct{}, 1)

	return p.done, p.run()
}

func (p *program) setupCmd(ctx context.Context) (*exec.Cmd, error) {
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

	outputWriters := []io.Writer{
		outWriter,
	}
	stderrWriters := []io.Writer{
		errWriter,
	}
	stdinReaders := []io.Reader{
		inReader,
	}

	if p.customStdout != nil {
		outputWriters = append(outputWriters, p.customStdout)
	}
	if p.customStderr != nil {
		stderrWriters = append(stderrWriters, p.customStderr)
	}
	if p.customStdin != nil {
		stdinReaders = append(stdinReaders, p.customStdin)
	}

	prepareFile := func(filePath string) (*os.File, error) {
		err := os.MkdirAll(filepath.Dir(filePath), 0750)
		if err != nil {
			return nil, err
		}
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, err
		}
		p.finalizers = append(p.finalizers, file.Close)
		return file, nil
	}

	if p.stdoutFile != "" {
		file, err := prepareFile(p.stdoutFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open stdout file %s: %w", p.stdoutFile, err)
		}
		outputWriters = append(outputWriters, file)
	}

	if p.stderrFile != "" {
		file, err := prepareFile(p.stderrFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open stderr file %s: %w", p.stderrFile, err)
		}
		stderrWriters = append(stderrWriters, file)
	}

	cmd.Stdout = io.MultiWriter(outputWriters...)
	cmd.Stderr = io.MultiWriter(stderrWriters...)
	cmd.Stdin = io.MultiReader(stdinReaders...)

	if !p.keepStdinOpen {
		_ = inWriter.Close()
	}

	return cmd, nil
}

func (p *program) run() error {
	p.cmdLock.Lock()
	err := p.cmd.Start()
	p.cmdLock.Unlock()

	if err != nil {
		p.dataLock.Lock()
		p.writeOutput(&p.errorBuffer, err.Error())

		p.state = StateError
		p.dataLock.Unlock()
		p.finalize()
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
		if strings.Contains(err.Error(), "signal: killed") {
			p.state = StateCancelled
		} else {
			p.state = StateError
		}
		p.writeOutput(&p.errorBuffer, err.Error())
	} else {
		p.state = StateFinished
	}
	p.dataLock.Unlock()

	p.finalize()
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		p.dataLock.Lock()
		if isError {
			p.writeOutput(&p.errorBuffer, line)
		} else {
			p.writeOutput(&p.outputBuffer, line)
		}
		p.dataLock.Unlock()
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if isError {
			p.dataLock.Lock()
			p.writeOutput(&p.errorBuffer, "Scanner error: "+err.Error())
			p.dataLock.Unlock()
		}
	}
}

// writeOutput handles buffer management with proper truncation and thread safety.
func (p *program) writeOutput(buffer *strings.Builder, s string) {
	newContent := s + "\n"

	if p.bufferLimit <= 0 {
		buffer.WriteString(newContent)
		return
	}

	spaceNeeded := len(newContent)
	currentSize := buffer.Len()

	// Check if we need to truncate
	threshold := int(float64(p.bufferLimit) * 0.95)

	if currentSize+spaceNeeded > threshold {
		buffer.Reset()
		fmt.Fprintf(buffer, "[%s] truncated due to buffer limit: %d bytes\n", time.Now(), p.bufferLimit)
	}

	buffer.WriteString(newContent)
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
		name:       name,
		command:    command,
		finalizers: []func() error{},
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.bufferLimit > 0 {
		p.outputBuffer.Grow(p.bufferLimit)
		p.errorBuffer.Grow(p.bufferLimit)
	}

	return p
}

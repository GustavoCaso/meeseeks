// Package program provides individual process execution and management capabilities.
// It offers comprehensive process lifecycle management, I/O handling, and monitoring.
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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/logger"
)

// Equal performs semantic comparison of two programs to determine if they have
// identical configuration. This compares:
// - Program string representation (name, command, arguments)
// - Interval configuration.
func Equal(program, other Program) bool {
	if program.String() != other.String() {
		return false
	}

	return program.Interval() == other.Interval()
}

// LogLine represent a log line sent to the subscription channel.
type LogLine struct {
	Message string `json:"message"`
	IsError bool   `json:"is_error"`
}

// Program defines the interface for managing individual external processes.
// It provides methods for execution, monitoring, I/O control, and lifecycle management
// with support for both synchronous and asynchronous execution modes.
type Program interface {
	// Async returns true if the program runs asynchronously, false for synchronous execution.
	Async() bool
	// Name returns the human-readable name of this program.
	Name() string
	// Start begins program execution and returns a channel that closes when execution completes.
	// Returns an error if the program cannot be started.
	Start(ctx context.Context) (<-chan struct{}, error)
	// Interval return the interval information if configured using the Interval Option
	Interval() time.Duration
	// Send writes data to the program's stdin. Requires KeepStdinOpen option.
	// Returns an error if stdin is closed or the program is not running.
	Send([]byte) error
	// CloseStdin closes the program's stdin pipe. Requires KeepStdinOpen option.
	// Returns an error if stdin is already closed or the program is not running.
	CloseStdin() error
	// Output returns all stdout content captured from the program.
	Output() string
	// Error returns all stderr content and error messages from the program.
	Error() string
	// SubscribeLogs return a channel to consume logs in real-time.
	// The caller is responsible for closing the context which ensure the channel is closed.
	SubscribeLogs(ctx context.Context) <-chan LogLine
	// State returns the current execution state of the program.
	State() ProcessState
	// String returns a human-readable representation of the program including name and command.
	String() string
	// Shutdown gracefully terminates the program with SIGTERM, falling back to SIGKILL after timeout.
	// Returns an error if the shutdown process fails.
	Shutdown(timeout time.Duration) error
}

// ProcessState represents the current execution state of a program.
type ProcessState int

// Process state constants define the possible execution states.
const (
	// StateNotStarted indicates the program has not been started yet.
	StateNotStarted ProcessState = iota
	// StateRunning indicates the program is currently executing.
	StateRunning
	// StateFinished indicates the program completed successfully.
	StateFinished
	// StateError indicates the program failed with an error.
	StateError
	// StateCancelled indicates the program was terminated by a signal.
	StateCancelled
)

// StateToString provides human-readable string representations of process states.
//
//nolint:gochecknoglobals // This gloabls is convinient
var StateToString = map[ProcessState]string{
	StateNotStarted: "not started",
	StateRunning:    "running",
	StateFinished:   "finished",
	StateError:      "error",
	StateCancelled:  "cancelled",
}

// Option defines a function type for configuring program instances.
type Option func(*program)

// StdoutFile redirects program stdout to the specified file path.
// The file will be created if it doesn't exist, with output appended.
func StdoutFile(file string) Option {
	return func(p *program) {
		p.stdoutFile = file
	}
}

// StderrFile redirects program stderr to the specified file path.
// The file will be created if it doesn't exist, with output appended.
func StderrFile(file string) Option {
	return func(p *program) {
		p.stderrFile = file
	}
}

// Stdout redirects program stdout to the provided io.Writer.
// Output will be written to both the internal buffer and the provided writer.
func Stdout(o io.Writer) Option {
	return func(p *program) {
		p.customStdout = o
	}
}

// Stderr redirects program stderr to the provided io.Writer.
// Output will be written to both the internal buffer and the provided writer.
func Stderr(o io.Writer) Option {
	return func(p *program) {
		p.customStderr = o
	}
}

// Stdin provides input to the program from the specified io.Reader.
// Input will be available to the program's stdin in addition to any data sent via Send().
func Stdin(o io.Reader) Option {
	return func(p *program) {
		p.customStdin = o
	}
}

// Args sets the command-line arguments for the program.
// These arguments will be passed to the program when it starts.
func Args(args ...string) Option {
	return func(p *program) {
		p.arguments = args
	}
}

// Envs sets additional environment variables for the program.
// These are added to the current environment and should be in KEY=VALUE format.
func Envs(envs ...string) Option {
	return func(p *program) {
		p.customEnv = envs
	}
}

// KeepStdinOpen keeps the program's stdin pipe open for sending data.
// Required if you plan to use Send() or CloseStdin() methods.
func KeepStdinOpen() Option {
	return func(p *program) {
		p.keepStdinOpen = true
	}
}

// Async configures the program to run asynchronously.
// When async, Start() returns immediately without waiting for completion.
func Async() Option {
	return func(p *program) {
		p.async = true
	}
}

// Logger sets the logger instance for the program.
// The logger will be used for internal logging operations and error reporting.
func Logger(logger logger.Logger) Option {
	return func(p *program) {
		p.logger = logger
	}
}

// Interval sets the interval information for the program
// This information is used by the meeseeks package to schedule the program in a cron-like style.
func Interval(duration time.Duration) Option {
	return func(p *program) {
		p.interval = duration
	}
}

// BufferSizeLimit sets the maximum size in bytes for stdout/stderr buffers.
// When the limit is reached, buffers are truncated to prevent memory issues.
// A limit of 0 means no limit (buffers can grow indefinitely).
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
	interval  time.Duration
	done      chan struct{}

	customStdout io.Writer
	customStderr io.Writer
	customStdin  io.Reader
	stdoutFile   string
	stderrFile   string

	keepStdinOpen bool
	customEnv     []string

	state                   ProcessState
	exitCode                int
	outputBuffer            strings.Builder
	errorBuffer             strings.Builder
	bufferLimit             int
	subscriptionIDCounter   atomic.Uint32
	logSubscriptionChannels map[uint32]chan LogLine
	subsLock                sync.RWMutex

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
	p.finalizers = []func() error{}

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
		p.writeOutput(&p.errorBuffer, err.Error(), true)

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
		p.state = StateError
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			status, _ := exitError.Sys().(syscall.WaitStatus)
			if status.Signaled() {
				p.state = StateCancelled
			}
		}
		p.writeOutput(&p.errorBuffer, err.Error(), true)
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
			p.writeOutput(&p.errorBuffer, line, true)
		} else {
			p.writeOutput(&p.outputBuffer, line, false)
		}
		p.dataLock.Unlock()
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if isError {
			p.dataLock.Lock()
			p.writeOutput(&p.errorBuffer, "Scanner error: "+err.Error(), true)
			p.dataLock.Unlock()
		}
	}
}

// writeOutput handles buffer management with proper truncation and thread safety.
func (p *program) writeOutput(buffer *strings.Builder, s string, isError bool) {
	newContent := s + "\n"

	if p.bufferLimit <= 0 {
		buffer.WriteString(newContent)
		// We use `s` because since we are broadcasting each line we do not need the new line from `newContent`
		p.broadcastLogSubscriptions(s, isError)
		return
	}

	spaceNeeded := len(newContent)
	currentSize := buffer.Len()

	// Check if we need to truncate
	threshold := int(float64(p.bufferLimit) * 0.95)

	if currentSize+spaceNeeded > threshold {
		buffer.Reset()
		fmt.Fprintf(buffer, "[%s] truncated due to buffer limit: %d bytes\n", time.Now(), p.bufferLimit)
		if p.logger != nil {
			p.logger.Info("buffer truncated", "program", p.name)
		}
	}

	buffer.WriteString(newContent)
	// We use `s` because since we are broadcasting each line we do not need the new line from `newContent`
	p.broadcastLogSubscriptions(s, isError)
}

func (p *program) SubscribeLogs(ctx context.Context) <-chan LogLine {
	ch := make(chan LogLine, 1000)
	id := p.subscriptionIDCounter.Add(1)

	p.subsLock.Lock()
	p.logSubscriptionChannels[id] = ch
	p.subsLock.Unlock()

	p.dataLock.RLock()
	existingOutput := p.outputBuffer.String()
	existingError := p.errorBuffer.String()
	p.dataLock.RUnlock()

	// Send existing log lines
	if existingOutput != "" {
		for _, line := range strings.Split(existingOutput, "\n") {
			if line != "" {
				select {
				case ch <- LogLine{Message: line, IsError: false}:
				case <-ctx.Done():
				default:
					// Channel full - drop the log line
				}
			}
		}
	}

	if existingError != "" {
		for _, line := range strings.Split(existingError, "\n") {
			if line != "" {
				select {
				case ch <- LogLine{Message: line, IsError: true}:
				case <-ctx.Done():
				default:
					// Channel full - drop the log line
				}
			}
		}
	}

	go func() {
		<-ctx.Done()
		p.subsLock.Lock()
		delete(p.logSubscriptionChannels, id)
		p.subsLock.Unlock()
		close(ch)
	}()

	return ch
}

func (p *program) broadcastLogSubscriptions(content string, isError bool) {
	logLine := LogLine{
		Message: content,
		IsError: isError,
	}

	p.subsLock.RLock()
	defer p.subsLock.RUnlock()
	for _, ch := range p.logSubscriptionChannels {
		select {
		case ch <- logLine:
		default:
			// Channel full - drop the log line
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

func (p *program) Interval() time.Duration {
	return p.interval
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

// New creates a new Program instance with the provided options.
func New(name, command string, opts ...Option) Program {
	p := &program{
		name:                    name,
		command:                 command,
		finalizers:              []func() error{},
		logSubscriptionChannels: make(map[uint32]chan LogLine),
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

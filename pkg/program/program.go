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

// LogLine represent a log line sent to the subscription channel.
type LogLine struct {
	Message string    `json:"message"`
	IsError bool      `json:"is_error"`
	Time    time.Time `json:"time"`
}

// Program defines the interface for managing individual external processes.
// It provides methods for execution, monitoring, I/O control, and lifecycle management
// with support for both synchronous and asynchronous execution modes.
type Program interface {
	// Name returns the human-readable name of this program.
	Name() string
	// Start begins program execution and returns a channel that closes when execution completes.
	// Returns an error if the program cannot be started.
	Start(ctx context.Context) (<-chan struct{}, error)
	// Interval return the interval information if configured using the Interval Option
	Interval() time.Duration
	// InitialDelay return the initial delay information if configured using the InitialDelay Option
	InitialDelay() time.Duration
	// RetryCount return the number of retries
	RetryCount() int
	// RetryDelay return the delay between retries
	RetryDelay() time.Duration
	// Deadline return program's deadline
	Deadline() time.Duration
	// Send writes data to the program's stdin. Requires KeepStdinOpen option.
	// Returns an error if stdin is closed or the program is not running.
	Send([]byte) error
	// CloseStdin closes the program's stdin pipe. Requires KeepStdinOpen option.
	// Returns an error if stdin is already closed or the program is not running.
	CloseStdin() error
	// Stdout returns all stdout content captured from the program.
	Stdout() string
	// Stderr returns all stderr content and error messages from the program.
	Stderr() string
	// SubscribeLogs return a channel to consume logs in real-time.
	// The caller is responsible for closing the context which ensure the channel is closed.
	SubscribeLogs(ctx context.Context, subscribeToPreviousLogs bool) <-chan LogLine
	// State returns the current execution state of the program.
	State() ProcessState
	// String returns a human-readable representation of the program including name, command and options.
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
//nolint:gochecknoglobals // This global is convenient
var StateToString = map[ProcessState]string{
	StateNotStarted: "not started",
	StateRunning:    "running",
	StateFinished:   "finished",
	StateError:      "error",
	StateCancelled:  "cancelled",
}

type program struct {
	cmd          *exec.Cmd
	name         string
	command      string
	arguments    []string
	async        bool
	interval     time.Duration
	initialDelay time.Duration
	retryCount   int
	retryDelay   time.Duration
	deadline     time.Duration
	done         chan struct{}

	customStdout io.Writer
	customStderr io.Writer
	customStdin  io.Reader
	stdoutFile   string
	stderrFile   string

	keepStdinOpen bool
	customEnv     []string

	state                   ProcessState
	buffer                  []LogLine
	bufferSize              int // current byte size of buffer contents
	bufferLimit             int // max bytes allowed in buffer (0 = unlimited)
	subscriptionIDCounter   atomic.Uint32
	logSubscriptionChannels map[uint32]chan LogLine
	subsLock                sync.RWMutex

	dataLock sync.RWMutex
	cmdLock  sync.Mutex

	pipes      *pipes
	logger     logger.Logger
	finalizers []func() error
	onSuccess  *Callback
	onFailure  *Callback

	consecutiveFailures int
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
	return errors.Join(p.outWriter.Close(), p.errWriter.Close(), p.inWriter.Close())
}

func (p *pipes) closeReaders() error {
	return errors.Join(p.outReader.Close(), p.errReader.Close(), p.inReader.Close())
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

	p.buffer = []LogLine{}

	return p
}

func (p *program) Name() string {
	return p.name
}

func (p *program) Start(ctx context.Context) (<-chan struct{}, error) {
	p.dataLock.Lock()
	if p.state == StateRunning {
		p.dataLock.Unlock()
		done := make(chan struct{})
		close(done)
		return done, errors.New("program already running")
	}

	p.state = StateRunning
	p.dataLock.Unlock()
	cmd, err := p.setupCmd(ctx)

	if err != nil {
		// Failed setup, run any finalizers registered before the failure
		// and revert state
		p.runFinalizers()
		p.dataLock.Lock()
		p.state = StateError
		p.dataLock.Unlock()
		done := make(chan struct{})
		p.triggerFailureIfNeeded(StateError, err)
		close(done)
		return done, err
	}

	p.cmdLock.Lock()
	p.cmd = cmd
	p.cmdLock.Unlock()

	done := make(chan struct{})
	p.dataLock.Lock()
	p.done = done
	p.dataLock.Unlock()

	return done, p.run(done)
}

func (p *program) Interval() time.Duration {
	return p.interval
}

func (p *program) InitialDelay() time.Duration {
	return p.initialDelay
}

func (p *program) RetryCount() int {
	return p.retryCount
}

func (p *program) RetryDelay() time.Duration {
	return p.retryDelay
}

func (p *program) Deadline() time.Duration {
	return p.deadline
}

func (p *program) Send(data []byte) error {
	p.dataLock.RLock()
	canSend := p.state == StateRunning
	p.dataLock.RUnlock()

	if !canSend {
		return errors.New("can not send data to a non-running program")
	}

	if !p.keepStdinOpen {
		return errors.New(
			"to send data to a running program please use the KeepStdinOpen option when initializing the program",
		)
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
			"stdin is already closed, use the KeepStdinOpen option when initializing the program to have full control over stdin",
		)
	}

	return p.pipes.inWriter.Close()
}

func (p *program) Stdout() string {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()

	var lines []string
	for _, entry := range p.buffer {
		if !entry.IsError {
			lines = append(lines, entry.Message)
		}
	}

	return strings.Join(lines, "\n")
}

func (p *program) Stderr() string {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()

	var lines []string
	for _, entry := range p.buffer {
		if entry.IsError {
			lines = append(lines, entry.Message)
		}
	}

	return strings.Join(lines, "\n")
}

func (p *program) SubscribeLogs(ctx context.Context, subscribeToPreviousLogs bool) <-chan LogLine {
	const liveBuffer = 1000

	id := p.subscriptionIDCounter.Add(1)

	// Holding dataLock blocks writeOutput (which broadcasts under dataLock),
	// so filling the backlog and registering the subscription here guarantees
	// no log line is dropped, duplicated, or delivered out of order.
	p.dataLock.RLock()

	capacity := liveBuffer
	if subscribeToPreviousLogs {
		capacity += len(p.buffer)
	}
	ch := make(chan LogLine, capacity)

	if subscribeToPreviousLogs {
		for _, logLine := range p.buffer {
			ch <- logLine
		}
	}

	p.subsLock.Lock()
	p.logSubscriptionChannels[id] = ch
	p.subsLock.Unlock()

	p.dataLock.RUnlock()

	go func() {
		<-ctx.Done()
		p.subsLock.Lock()
		delete(p.logSubscriptionChannels, id)
		p.subsLock.Unlock()
		close(ch)
	}()

	return ch
}

func (p *program) State() ProcessState {
	p.dataLock.RLock()
	defer p.dataLock.RUnlock()
	return p.state
}

func (p *program) String() string {
	s := fmt.Sprintf("name: %s, command: %s, arguments: (%s)", p.name,
		p.command,
		strings.Join(p.arguments, ", "))

	if p.interval > 0 {
		s += fmt.Sprintf(", interval: %s", p.interval)
	}

	if p.initialDelay > 0 {
		s += fmt.Sprintf(", initial delay: %s", p.initialDelay)
	}

	if p.retryCount > 0 {
		s += fmt.Sprintf(", retry count: %d", p.retryCount)
	}

	if p.retryDelay > 0 {
		s += fmt.Sprintf(", retry delay: %s", p.retryDelay)
	}

	if p.deadline > 0 {
		s += fmt.Sprintf(", deadline: %s", p.deadline)
	}

	if len(p.customEnv) > 0 {
		s += fmt.Sprintf(", env: (%s)", strings.Join(p.customEnv, ", "))
	}

	if p.stdoutFile != "" {
		s += fmt.Sprintf(", stdout file: %s", p.stdoutFile)
	}

	if p.stderrFile != "" {
		s += fmt.Sprintf(", stderr file: %s", p.stderrFile)
	}

	if p.keepStdinOpen {
		s += ", keep stdin open: true"
	}

	if p.bufferLimit > 0 {
		s += fmt.Sprintf(", buffer limit: %d", p.bufferLimit)
	}

	if p.onSuccess != nil {
		s += fmt.Sprintf(", success callback: %s %s", p.onSuccess.Command, p.onSuccess.Args)
	}

	if p.onFailure != nil {
		s += fmt.Sprintf(", failure callback: %s %s", p.onFailure.Command, p.onFailure.Args)
	}

	return s
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

	p.dataLock.RLock()
	done := p.done
	p.dataLock.RUnlock()

	// Wait for the existing monitoring to handle process exit
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		// Timeout exceeded, force kill
		return p.forcekill()
	}
}

// runFinalizers must run before the final state is published, so a caller
// cannot observe a finished program and Start() it again while cleanup of the
// previous run is still mutating p.finalizers.
func (p *program) runFinalizers() {
	for _, finalizer := range p.finalizers {
		err := finalizer()
		if err != nil {
			if p.logger != nil {
				p.logger.Warn("error when executing finalizers", "error", err.Error())
			}
		}
	}
	p.finalizers = []func() error{}
}

func (p *program) setupCmd(ctx context.Context) (*exec.Cmd, error) {
	var cancel context.CancelFunc

	if p.deadline > 0 {
		ctx, cancel = context.WithTimeout(ctx, p.deadline)

		p.finalizers = append(p.finalizers, func() error {
			cancel()
			return nil
		})
	}

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

func (p *program) run(done chan struct{}) error {
	p.cmdLock.Lock()
	err := p.cmd.Start()
	p.cmdLock.Unlock()

	if err != nil {
		p.runFinalizers()
		p.dataLock.Lock()
		p.writeOutput(err.Error(), true)

		p.state = StateError
		p.dataLock.Unlock()
		p.triggerFailureIfNeeded(StateError, err)
		close(done)
		return err
	}

	if p.async {
		go p.monitorProcess(done)
		return nil
	}

	p.monitorProcess(done)
	return nil
}

func (p *program) monitorProcess(done chan struct{}) {
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

	p.runFinalizers()

	p.dataLock.Lock()
	var callbackErr error
	var callbackState ProcessState
	if err != nil {
		p.state = StateError
		callbackState = StateError
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			status, _ := exitError.Sys().(syscall.WaitStatus)
			if status.Signaled() {
				p.state = StateCancelled
				callbackState = StateCancelled
			}
		}
		p.writeOutput(err.Error(), true)
		callbackErr = err
	} else {
		p.state = StateFinished
		callbackState = StateFinished
		p.consecutiveFailures = 0
	}
	p.dataLock.Unlock()

	if callbackErr != nil {
		p.triggerFailureIfNeeded(callbackState, callbackErr)
	} else {
		p.triggerSuccess()
	}

	close(done)
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		p.dataLock.Lock()
		p.writeOutput(line, isError)
		p.dataLock.Unlock()
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if isError {
			p.dataLock.Lock()
			p.writeOutput("Scanner error: "+err.Error(), true)
			p.dataLock.Unlock()
		}
	}
}

// writeOutput handles buffer management with byte-based limits and periodic compaction.
func (p *program) writeOutput(s string, isError bool) {
	logLine := LogLine{
		Message: s,
		IsError: isError,
		Time:    time.Now(),
	}
	entrySize := len(s)

	if p.bufferLimit <= 0 {
		p.buffer = append(p.buffer, logLine)
		p.bufferSize += entrySize
		p.broadcastLogSubscriptions(logLine)
		return
	}

	// Count how many entries to evict to make room for the new entry
	evicted := 0
	for p.bufferSize+entrySize > p.bufferLimit && evicted < len(p.buffer) {
		p.bufferSize -= len(p.buffer[evicted].Message)
		evicted++
	}

	// Shift remaining entries left and shrink slice (reuses backing array, no memory leak)
	if evicted > 0 {
		// copy() shifts elements left but doesn't change slice length, leaving stale
		// duplicates at the end. The reslice removes them by adjusting the length.
		// Example: [A,B,C,D,E] evict 2 -> copy gives [C,D,E,D,E] -> reslice gives [C,D,E]
		copy(p.buffer, p.buffer[evicted:])
		p.buffer = p.buffer[:len(p.buffer)-evicted]

		truncationMsg := LogLine{
			Message: fmt.Sprintf(
				"[buffer truncated: evicted %d entries to stay under %d bytes]",
				evicted,
				p.bufferLimit,
			),
			IsError: false,
			Time:    time.Now(),
		}
		p.broadcastLogSubscriptions(truncationMsg)
		if p.logger != nil {
			p.logger.Info("buffer truncated", "program", p.name, "evicted_entries", evicted)
		}
	}

	p.buffer = append(p.buffer, logLine)
	p.bufferSize += entrySize
	p.broadcastLogSubscriptions(logLine)
}

func (p *program) broadcastLogSubscriptions(logLine LogLine) {
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

// callbackDeadline caps callback execution so a hung callback cannot block
// the parent program's completion indefinitely.
const callbackDeadline = time.Minute

func (p *program) triggerSuccess() {
	if p.onSuccess == nil {
		return
	}

	env := []string{
		fmt.Sprintf("MEESEEKS_PROGRAM=%s", p.name),
		fmt.Sprintf("MEESEEKS_STATUS=%s", StateToString[StateFinished]),
	}
	p.runCallback("success", p.onSuccess, env)
}

func (p *program) triggerFailure(status ProcessState, err error) {
	if p.onFailure == nil {
		return
	}

	env := []string{
		fmt.Sprintf("MEESEEKS_PROGRAM=%s", p.name),
		fmt.Sprintf("MEESEEKS_STATUS=%s", StateToString[status]),
		fmt.Sprintf("MEESEEKS_ERROR=%s", err.Error()),
	}
	p.runCallback("failure", p.onFailure, env)
}

// runCallback executes a callback command as its own Program and blocks until
// it completes or callbackDeadline expires.
func (p *program) runCallback(kind string, callback *Callback, env []string) {
	name := fmt.Sprintf("%s_callback_%s", kind, p.name)
	cb := New(
		name,
		callback.Command,
		Args(callback.Args...),
		Envs(env...),
		Deadline(callbackDeadline),
	)

	done, err := cb.Start(context.Background())
	if err != nil {
		if p.logger != nil {
			p.logger.Error(
				"callback command failed to start",
				"program", p.name,
				"callback", kind,
				"error", err.Error(),
			)
		}
		return
	}
	<-done

	if cb.State() != StateFinished {
		if p.logger != nil {
			p.logger.Error(
				"callback command failed",
				"program", p.name,
				"callback", kind,
				"state", StateToString[cb.State()],
				"output", cb.Stdout(),
				"error", cb.Stderr(),
			)
		}
		return
	}

	if p.logger != nil && cb.Stdout() != "" {
		p.logger.Info(
			"callback command output",
			"program", p.name,
			"callback", kind,
			"output", cb.Stdout(),
		)
	}
}

func (p *program) triggerFailureIfNeeded(status ProcessState, err error) {
	if !p.shouldTriggerFailureCallback() {
		return
	}

	p.triggerFailure(status, err)
}

func (p *program) shouldTriggerFailureCallback() bool {
	p.dataLock.Lock()
	defer p.dataLock.Unlock()

	p.consecutiveFailures++

	if p.retryCount == 0 {
		p.consecutiveFailures = 0
		return true
	}

	if p.consecutiveFailures > p.retryCount {
		p.consecutiveFailures = 0
		return true
	}

	return false
}

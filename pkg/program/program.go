// Package program provides individual process execution and management capabilities.
// It offers comprehensive process lifecycle management, I/O handling, and monitoring.
package program

import (
	"bufio"
	"bytes"
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
	"unicode/utf8"

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
	group        *processGroup
	name         string
	command      string
	arguments    []string
	async        bool
	interval     time.Duration
	initialDelay time.Duration
	retryCount   int
	retryDelay   time.Duration
	deadline     time.Duration
	killGrace    time.Duration
	killGraceSet bool
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

	tracker        ProcessTracker
	trackedProcess *ProcessIdentity // Guarded by cmdLock.

	// callbackCtx is the context the current run was started with, without its
	// deadline. Cancelling it stops a callback still running after the program
	// exited, so a supervisor can stop everything a run launched. Guarded by
	// cmdLock.
	callbackCtx context.Context //nolint:containedctx // Scoped to a single run

	consecutiveFailures int
}

// waitDelayMargin is how long a run keeps waiting for inherited pipes after the
// grace period before closing them.
const waitDelayMargin = 5 * time.Second

// maxLogEntryBytes is the largest single log entry.
const maxLogEntryBytes = 64 * 1024

// gatedWriter lets a run end without a copy goroutine writing to the caller's
// writer afterwards. close waits for an in-flight write and rejects later ones,
// even if the goroutine stays blocked in a read that Close cannot interrupt.
type gatedWriter struct {
	mu     sync.Mutex
	w      io.Writer
	closed bool
}

func (g *gatedWriter) Write(data []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return 0, io.ErrClosedPipe
	}
	return g.w.Write(data)
}

func (g *gatedWriter) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
}

// pipes connects a program to its output and input. The process gets real OS
// pipes rather than Go writers, so cmd.Wait returns when the leader exits
// instead of also waiting for every descendant holding an output pipe. The
// copying is owned here and drained separately.
type pipes struct {
	outReader *io.PipeReader
	outWriter *io.PipeWriter
	errReader *io.PipeReader
	errWriter *io.PipeWriter
	inReader  *io.PipeReader
	inWriter  *io.PipeWriter

	stdin io.Reader

	parentOut *os.File
	parentErr *os.File
	parentIn  *os.File
	childEnds []*os.File
	outGate   *gatedWriter
	errGate   *gatedWriter

	outputDone chan struct{}
	copyErrMu  sync.Mutex
	copyErr    error
}

func (p *pipes) closeWriters() error {
	return errors.Join(p.outWriter.Close(), p.errWriter.Close(), p.inWriter.Close())
}

func (p *pipes) closeReaders() error {
	return errors.Join(p.outReader.Close(), p.errReader.Close(), p.inReader.Close())
}

// connect creates the OS pipes, hands the child's ends to cmd, and starts
// copying output. Those copies block until the process writes, and closeFiles
// ends them if it never starts.
func (p *pipes) connect(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) error {
	var childOut, childErr, childIn *os.File
	var err error

	if p.parentOut, childOut, err = os.Pipe(); err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	p.childEnds = append(p.childEnds, childOut)

	if p.parentErr, childErr, err = os.Pipe(); err != nil {
		p.closeFiles()
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	p.childEnds = append(p.childEnds, childErr)

	if childIn, p.parentIn, err = os.Pipe(); err != nil {
		p.closeFiles()
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	p.childEnds = append(p.childEnds, childIn)

	p.stdin = stdin
	cmd.Stdout = childOut
	cmd.Stderr = childErr
	cmd.Stdin = childIn

	p.outGate = &gatedWriter{w: stdout}
	p.errGate = &gatedWriter{w: stderr}
	p.outputDone = make(chan struct{})

	var output sync.WaitGroup
	output.Add(2)

	go func() {
		defer output.Done()
		_, copyErr := io.Copy(p.outGate, p.parentOut)
		p.recordCopyError(copyErr)
		_ = p.parentOut.Close()
	}()
	go func() {
		defer output.Done()
		_, copyErr := io.Copy(p.errGate, p.parentErr)
		p.recordCopyError(copyErr)
		_ = p.parentErr.Close()
	}()
	go func() {
		output.Wait()
		close(p.outputDone)
	}()

	return nil
}

// processStarted releases the child's pipe ends, so EOF arrives once every
// holder exits, and starts copying stdin. Stdin waits until now so a failed
// start cannot consume a caller's reader, and it is not part of outputDone
// because a reader with no more data must not hold up the end of a run.
func (p *pipes) processStarted() {
	for _, file := range p.childEnds {
		_ = file.Close()
	}

	go func() {
		_, err := io.Copy(p.parentIn, p.stdin)
		// A program may exit without reading all of its input.
		if !isClosedPipeError(err) {
			p.recordCopyError(err)
		}
		_ = p.parentIn.Close()
	}()
}

// recordCopyError keeps the first real copy failure. Closed pipes are how a
// drain is ended, so they are not failures.
func (p *pipes) recordCopyError(err error) {
	if err == nil || errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		return
	}

	p.copyErrMu.Lock()
	defer p.copyErrMu.Unlock()
	if p.copyErr == nil {
		p.copyErr = err
	}
}

func (p *pipes) firstCopyError() error {
	p.copyErrMu.Lock()
	defer p.copyErrMu.Unlock()
	return p.copyErr
}

// drain waits for output to reach EOF, up to limit or until stop closes. It
// reports whether all output was read. On false, a process still holds a pipe,
// so the parent's ends are closed and the writers are gated. That waits for any
// in-flight write, so no caller's writer is touched after the run ends.
func (p *pipes) drain(limit time.Duration, stop <-chan struct{}) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()

	drained := false
	select {
	case <-p.outputDone:
		drained = true
	case <-timer.C:
	case <-stop:
	}

	p.closeFiles()

	if !drained {
		// Release a write blocked on the log pipe, whose reader may have stopped.
		_ = p.outWriter.Close()
		_ = p.errWriter.Close()
		p.outGate.close()
		p.errGate.close()
	}
	return drained
}

// closeFiles closes every OS pipe end. Ends that are already closed are ignored.
func (p *pipes) closeFiles() {
	for _, file := range append([]*os.File{p.parentOut, p.parentErr, p.parentIn}, p.childEnds...) {
		if file != nil {
			_ = file.Close()
		}
	}
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

	// Set before setup, whose failure also runs the failure callback.
	p.cmdLock.Lock()
	p.callbackCtx = ctx
	p.cmdLock.Unlock()

	cmd, runCtx, err := p.setupCmd(ctx)

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

	group := p.configureTermination(runCtx, cmd)

	p.cmdLock.Lock()
	p.cmd = cmd
	p.group = group
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

	if p.killGraceSet {
		s += fmt.Sprintf(", kill grace: %s", p.killGrace)
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
		s += fmt.Sprintf(", success callback: %s %v", p.onSuccess.Command, p.onSuccess.Args)
	}

	if p.onFailure != nil {
		s += fmt.Sprintf(", failure callback: %s %v", p.onFailure.Command, p.onFailure.Args)
	}

	return s
}

func (p *program) Shutdown(timeout time.Duration) error {
	p.cmdLock.Lock()
	if p.cmd == nil || p.cmd.Process == nil {
		p.cmdLock.Unlock()
		return nil
	}

	// A configured kill grace is a floor, so a short stop timeout cannot cut
	// off cleanup the program was explicitly given time to finish.
	if p.killGraceSet && p.killGrace > timeout {
		timeout = p.killGrace
	}

	// Send SIGTERM to the whole process group for graceful shutdown
	group := p.group
	err := group.terminate(timeout)
	p.cmdLock.Unlock()

	p.dataLock.RLock()
	done := p.done
	p.dataLock.RUnlock()

	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			// The leader already exited on its own. Background processes are left
			// running, so only the wait for their output needs to end.
			group.stopDrain()
			select {
			case <-done:
			case <-time.After(timeout):
			}
			return nil
		}
		// If SIGTERM fails, fall back to force kill
		return p.forcekill()
	}

	// The group's timer delivers SIGKILL. Waiting on its current deadline means
	// a concurrent Shutdown that extended it is honored here too. The forced
	// kill below is only a last resort if the run fails to finish after that.
	for {
		select {
		case <-done:
			return nil
		case <-time.After(time.Until(group.killDeadline()) + waitDelayMargin):
			if time.Until(group.killDeadline())+waitDelayMargin <= 0 {
				return p.forcekill()
			}
		}
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

// setupCmd builds the command and returns it with the context bounding its run.
func (p *program) setupCmd(ctx context.Context) (*exec.Cmd, context.Context, error) {
	var cancel context.CancelFunc

	if p.deadline > 0 {
		ctx, cancel = context.WithTimeout(ctx, p.deadline)

		p.finalizers = append(p.finalizers, func() error {
			cancel()
			return nil
		})
	}

	// processGroup watches ctx, so termination can reach the whole process tree.
	//nolint:gosec,noctx // We accept the arguments the users have manually defined
	cmd := exec.Command(p.command, p.arguments...)
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
			return nil, nil, fmt.Errorf("failed to open stdout file %s: %w", p.stdoutFile, err)
		}
		outputWriters = append(outputWriters, file)
	}

	if p.stderrFile != "" {
		file, err := prepareFile(p.stderrFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open stderr file %s: %w", p.stderrFile, err)
		}
		stderrWriters = append(stderrWriters, file)
	}

	err := pipes.connect(
		cmd,
		io.MultiWriter(outputWriters...),
		io.MultiWriter(stderrWriters...),
		io.MultiReader(stdinReaders...),
	)
	if err != nil {
		return nil, nil, err
	}

	if !p.keepStdinOpen {
		_ = inWriter.Close()
	}

	return cmd, ctx, nil
}

// configureTermination runs the command in its own process group and makes
// the end of ctx terminate that group.
func (p *program) configureTermination(ctx context.Context, cmd *exec.Cmd) *processGroup {
	grace := DefaultKillGrace
	if p.killGraceSet {
		grace = p.killGrace
	}

	setProcessGroup(cmd)

	group := newProcessGroup(ctx, cmd, grace)
	group.onStarted = func(process *os.Process) { p.trackStarted(process, grace) }
	return group
}

// trackStarted reports a new process. Callers must hold cmdLock.
func (p *program) trackStarted(process *os.Process, grace time.Duration) {
	if p.tracker == nil {
		return
	}

	startTime, err := processStartTime(process.Pid)
	if err != nil {
		if p.logger != nil {
			p.logger.Warn(
				"could not record program process",
				"program", p.name,
				"error", err.Error(),
			)
		}
		return
	}

	identity := ProcessIdentity{PID: process.Pid, StartTime: startTime, KillGrace: grace}
	p.trackedProcess = &identity
	p.tracker.ProcessStarted(identity)
}

func (p *program) trackExited() {
	p.cmdLock.Lock()
	identity := p.trackedProcess
	p.trackedProcess = nil
	p.cmdLock.Unlock()

	if identity != nil {
		p.tracker.ProcessExited(*identity)
	}
}

func (p *program) run(done chan struct{}) error {
	p.cmdLock.Lock()
	err := p.group.start(func(attachErr error) {
		if p.logger != nil {
			p.logger.Warn(
				"could not track program process tree",
				"program", p.name,
				"error", attachErr.Error(),
			)
		}
	})
	if err == nil {
		p.pipes.processStarted()
	}
	p.cmdLock.Unlock()

	if err != nil {
		p.trackExited()
		p.pipes.closeFiles()
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
	group := p.group
	p.cmdLock.Unlock()
	// The leader is reaped only after signaling ends, so its group ID stays
	// reserved. Wait does not block on output, because the process has OS pipes.
	group.awaitLeaderExit()
	err := cmd.Wait()
	group.markReaped()
	p.trackExited()

	// A background process holding a pipe does not change the program's result.
	if !p.pipes.drain(group.drainLimit(), group.drainEnd) && p.logger != nil {
		p.logger.Warn("program exited but a descendant kept its output open", "program", p.name)
	}
	if err == nil {
		err = p.pipes.firstCopyError()
	}
	err = group.exitError(err)

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

// readOutput must keep reading until EOF. If it stopped early, nothing would
// drain the pipe and the program would block forever on its next write. A line
// longer than maxLogEntryBytes is stored as several entries, split between
// UTF-8 characters.
func (p *program) readOutput(reader io.Reader, isError bool) {
	lines := bufio.NewReaderSize(reader, maxLogEntryBytes)
	store := func(entry string) {
		p.dataLock.Lock()
		defer p.dataLock.Unlock()
		p.writeOutput(entry, isError)
	}

	var carry []byte // Start of a character cut off by the previous fragment.
	continued := false

	for {
		fragment, isPrefix, err := lines.ReadLine()
		if err != nil {
			if len(carry) > 0 {
				store(string(carry))
			}
			if isError && !errors.Is(err, io.EOF) {
				store("Output read error: " + err.Error())
			}
			return
		}

		entry := fragment
		if len(carry) > 0 {
			carry = append(carry, fragment...)
			entry, carry = carry, nil
		}
		if isPrefix {
			complete := completeUTF8Length(entry)
			// fragment points into the reader's buffer, so the rest must be copied.
			carry = bytes.Clone(entry[complete:])
			entry = entry[:complete]
		}

		// A line that exactly filled the buffer ends with an empty fragment,
		// which is not a blank line in the program's output.
		if len(entry) > 0 || !continued {
			store(string(entry))
		}
		continued = isPrefix
	}
}

// completeUTF8Length returns the length of data without a trailing incomplete
// UTF-8 character. Invalid encodings count as complete, so binary output is
// never held back.
func completeUTF8Length(data []byte) int {
	for back := 1; back < utf8.UTFMax && back <= len(data); back++ {
		start := len(data) - back
		if !utf8.RuneStart(data[start]) {
			continue
		}
		if utf8.FullRune(data[start:]) {
			return len(data)
		}
		return start
	}
	return len(data)
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

	err := p.group.kill()
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
		BufferSizeLimit(p.bufferLimit),
		Logger(p.logger),
		// Recorded so a daemon crash during a callback does not leave it running.
		Tracker(p.tracker),
	)

	p.cmdLock.Lock()
	ctx := p.callbackCtx
	p.cmdLock.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	done, err := cb.Start(ctx)
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

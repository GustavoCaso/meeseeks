package program

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
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
	Interval() time.Duration
	Runs() int
	Statistics() Statistics
	Shutdown(timeout time.Duration) error
}

type ProcessState int

const (
	StateNotStarted ProcessState = iota
	StateRunning
	StateFinished
	StateError
)

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

func Interval(interval time.Duration) Option {
	return func(p *program) {
		p.interval = interval
	}
}

type program struct {
	cmd       *exec.Cmd
	name      string
	command   string
	arguments []string
	async     bool
	done      chan struct{}
	stop      chan struct{} // For stopping interval programs

	customStdout  io.Writer
	customStderr  io.Writer
	customStdin   io.Reader
	keepStdinOpen bool
	customEnv     []string
	interval      time.Duration

	currentRun int
	runResults []result

	exitCode     int
	overallState ProcessState

	stdoutLock  sync.RWMutex
	stderrLock  sync.RWMutex
	resultsLock sync.RWMutex
	stateLock   sync.RWMutex
	cmdLock     sync.Mutex

	pipes *pipes
}

type result struct {
	state        ProcessState
	outputBuffer strings.Builder
	errorBuffer  strings.Builder
	lastError    string
	lastLine     string
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
	if p.interval > 0 {
		ticker := time.NewTicker(p.interval)
		intervalDone := make(chan struct{}, 1)
		go func() {
			defer func() {
				intervalDone <- struct{}{}
				ticker.Stop()
			}()
			for {
				select {
				case <-ctx.Done():
					return
				case <-p.stop:
					return
				case <-ticker.C:
					done, err := p.start(ctx)
					if err != nil {
						return
					}
					<-done

					p.stateLock.Lock()
					p.currentRun++
					p.stateLock.Unlock()
				}
			}
		}()
		// We return a separate done channel from the program struct, as interval programs are long running ones
		// We only signal that we are done with an interval program if there is an error executing the program or the program was terminated
		return intervalDone, nil
	}

	return p.start(ctx)
}

func (p *program) signalDone() {
	close(p.done)
}

func (p *program) start(ctx context.Context) (<-chan struct{}, error) {
	p.resultsLock.Lock()
	results := result{}
	p.runResults = append(p.runResults, results)
	p.resultsLock.Unlock()

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
	cmd := p.cmd
	p.cmdLock.Unlock()
	err := cmd.Start()

	if err != nil {
		p.resultsLock.Lock()
		currentIndex := len(p.runResults) - 1
		p.runResults[currentIndex].state = StateError
		p.runResults[currentIndex].errorBuffer.WriteString(err.Error())
		p.runResults[currentIndex].errorBuffer.WriteString("\n")
		p.runResults[currentIndex].lastError = err.Error()
		p.resultsLock.Unlock()

		p.stateLock.Lock()
		p.overallState = StateError
		p.stateLock.Unlock()
		p.signalDone()
		return err
	}
	p.resultsLock.Lock()
	currentIndex := len(p.runResults) - 1
	p.runResults[currentIndex].state = StateRunning
	p.resultsLock.Unlock()

	p.stateLock.Lock()
	p.overallState = StateRunning
	p.stateLock.Unlock()

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
	//nolint:sloglint //currently working on adding support for custom logger
	if writersErr != nil {
		slog.Error(
			"error closing writers",
			"program",
			p.name,
			"error",
			writersErr.Error(),
		)
	}

	// Wait for readers to finish processing all data
	wg.Wait()

	readersErr := p.pipes.closeReaders()
	//nolint:sloglint //currently working on adding support for custom logger
	if readersErr != nil {
		slog.Error(
			"error closing readers",
			"program",
			p.name,
			"error",
			readersErr.Error(),
		)
	}

	p.resultsLock.Lock()
	currentIndex := len(p.runResults) - 1
	if err != nil {
		p.runResults[currentIndex].errorBuffer.WriteString(err.Error())
		p.runResults[currentIndex].errorBuffer.WriteString("\n")
		p.runResults[currentIndex].lastError = err.Error()
		p.runResults[currentIndex].state = StateError
	} else {
		p.runResults[currentIndex].state = StateFinished
	}
	p.resultsLock.Unlock()

	p.cmdLock.Lock()
	exitCode := p.cmd.ProcessState.ExitCode()
	p.cmdLock.Unlock()

	p.stateLock.Lock()
	p.exitCode = exitCode
	if err != nil {
		p.overallState = StateError
	} else {
		p.overallState = StateFinished
	}
	p.stateLock.Unlock()

	p.signalDone()
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		p.resultsLock.RLock()
		currentIndex := len(p.runResults) - 1
		p.resultsLock.RUnlock()

		if isError {
			p.stderrLock.Lock()
			p.runResults[currentIndex].errorBuffer.WriteString(line + "\n")
			p.runResults[currentIndex].lastError = line
			p.stderrLock.Unlock()
		} else {
			p.stdoutLock.Lock()
			p.runResults[currentIndex].outputBuffer.WriteString(line + "\n")
			p.runResults[currentIndex].lastLine = line
			p.stdoutLock.Unlock()
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if isError {
			p.resultsLock.RLock()
			currentIndex := len(p.runResults) - 1
			p.resultsLock.RUnlock()
			p.stderrLock.Lock()
			p.runResults[currentIndex].errorBuffer.WriteString("Scanner error: " + err.Error())
			p.runResults[currentIndex].errorBuffer.WriteString("\n")
			p.stderrLock.Unlock()
		}
	}
}

func (p *program) Send(data []byte) error {
	p.resultsLock.RLock()
	canSend := len(p.runResults) > 0 && p.runResults[len(p.runResults)-1].state == StateRunning
	p.resultsLock.RUnlock()

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
	p.resultsLock.RLock()
	canClose := len(p.runResults) > 0 && p.runResults[len(p.runResults)-1].state == StateRunning
	p.resultsLock.RUnlock()

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
	p.resultsLock.RLock()
	defer p.resultsLock.RUnlock()

	if len(p.runResults) == 0 {
		return ""
	}

	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()

	return p.runResults[len(p.runResults)-1].outputBuffer.String()
}

func (p *program) LastLine() string {
	p.resultsLock.RLock()
	defer p.resultsLock.RUnlock()

	if len(p.runResults) == 0 {
		return ""
	}

	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()

	return p.runResults[len(p.runResults)-1].lastLine
}

func (p *program) Error() string {
	p.resultsLock.RLock()
	defer p.resultsLock.RUnlock()

	if len(p.runResults) == 0 {
		return ""
	}

	p.stderrLock.RLock()
	defer p.stderrLock.RUnlock()

	return p.runResults[len(p.runResults)-1].errorBuffer.String()
}

func (p *program) State() ProcessState {
	p.stateLock.RLock()
	defer p.stateLock.RUnlock()
	return p.overallState
}

func (p *program) Interval() time.Duration {
	return p.interval
}

func (p *program) Runs() int {
	p.stateLock.RLock()
	defer p.stateLock.RUnlock()
	return p.currentRun
}

func (p *program) Shutdown(timeout time.Duration) error {
	// Stop interval loop if this is an interval program
	if p.interval > 0 {
		select {
		case p.stop <- struct{}{}:
		default:
			// Channel might be full or closed, continue
		}
	}

	p.cmdLock.Lock()
	if p.cmd == nil || p.cmd.Process == nil || p.done == nil {
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

func (p *program) forcekill() error {
	p.cmdLock.Lock()
	if p.cmd == nil || p.cmd.Process == nil || p.done == nil {
		p.cmdLock.Unlock()
		return nil
	}

	err := p.cmd.Process.Kill()
	p.cmdLock.Unlock()
	if err != nil && errors.Is(err, os.ErrProcessDone) {
		// Process already terminated, ignore this error
		p.signalDone()
		return nil
	}
	// Don't signal done here - let the monitoring goroutine handle it
	return err
}

type Statistics struct {
	ProgramName       string        `json:"program_name"`
	State             string        `json:"state"`
	TotalRuns         int           `json:"total_runs"`
	Successful        int           `json:"successful_runs"`
	Failed            int           `json:"failed_runs"`
	Running           int           `json:"running"`
	TotalOutputLines  int           `json:"total_output_lines"`
	LastSuccessfulRun int           `json:"last_successful_run"`
	LastError         string        `json:"last_error"`
	LastOutput        string        `json:"last_output"`
	Interval          time.Duration `json:"interval"`
	HasInterval       bool          `json:"has_interval"`
}

func (s Statistics) String() string {
	if s.TotalRuns == 0 {
		return fmt.Sprintf("[%s] No runs completed yet", s.ProgramName)
	}

	var intervalInfo string
	if s.HasInterval {
		intervalInfo = fmt.Sprintf("interval: %v, ", s.Interval)
	}

	statisticsMsg := fmt.Sprintf("[%s] %stotal runs: %d, successful: %d, failed: %d",
		s.ProgramName, intervalInfo, s.TotalRuns, s.Successful, s.Failed)

	if s.Running > 0 {
		statisticsMsg += fmt.Sprintf(", running: %d", s.Running)
	}

	if s.TotalOutputLines > 0 {
		statisticsMsg += fmt.Sprintf(", total output lines: %d", s.TotalOutputLines)
	}

	if s.LastSuccessfulRun >= 0 {
		statisticsMsg += fmt.Sprintf(", last successful run: #%d", s.LastSuccessfulRun)
	}

	if s.Failed > 0 && s.LastError != "" {
		statisticsMsg += fmt.Sprintf(", last error: %s", s.LastError)
	}

	if s.LastOutput != "" {
		statisticsMsg += fmt.Sprintf(", last output: %s", s.LastOutput)
	}

	return statisticsMsg
}

func (p *program) Statistics() Statistics {
	var resultsCopy []result
	p.resultsLock.RLock()
	resultsCopy = slices.Clone(p.runResults)
	p.resultsLock.RUnlock()

	var state string
	switch p.State() {
	case StateFinished:
		state = "finished"
	case StateError:
		state = "error"
	case StateRunning:
		state = "running"
	case StateNotStarted:
		state = "not started"
	}

	stats := Statistics{
		ProgramName:       p.name,
		State:             state,
		TotalRuns:         len(resultsCopy),
		LastSuccessfulRun: -1,
		Interval:          p.interval,
		HasInterval:       p.interval > 0,
	}

	if stats.TotalRuns == 0 {
		return stats
	}

	for i, result := range resultsCopy {
		switch result.state { //nolint:exhaustive // StateNotRunning is skipped as we do not use in the Statistics struct
		case StateFinished:
			stats.Successful++
			stats.LastSuccessfulRun = i
		case StateError:
			stats.Failed++
		case StateRunning:
			stats.Running++
		}

		p.stdoutLock.RLock()
		outputContent := result.outputBuffer.String()
		lastLine := result.lastLine
		p.stdoutLock.RUnlock()

		if len(outputContent) > 0 {
			stats.TotalOutputLines += len(strings.Split(strings.TrimSpace(outputContent), "\n"))
		}

		if lastLine != "" {
			stats.LastOutput = lastLine
		}
	}

	if stats.Failed > 0 {
		for i := len(resultsCopy) - 1; i >= 0; i-- {
			if resultsCopy[i].state == StateError && resultsCopy[i].lastError != "" {
				stats.LastError = resultsCopy[i].lastError
				break
			}
		}
	}

	return stats
}

func New(name, command string, opts ...Option) Program {
	p := &program{
		name:       name,
		command:    command,
		runResults: []result{},
		currentRun: 0,
		stop:       make(chan struct{}, 1),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

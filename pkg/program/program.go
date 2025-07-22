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
	"strings"
	"sync"
	"time"
)

type Program interface {
	Async() bool
	Name() string
	Start(ctx context.Context) (<-chan struct{}, error)
	Status() string
	Send([]byte) error
	CloseStdin() error
	LastLine() string
	Output() string
	Error() string
	State() ProcessState
	Interval() time.Duration
	Runs() int
	Statistics() Statistics
	Kill() error
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

	customStdout  io.Writer
	customStderr  io.Writer
	customStdin   io.Reader
	keepStdinOpen bool
	customEnv     []string
	interval      time.Duration

	current int
	results []result

	exitCode int

	stdoutLock sync.RWMutex
	stderrLock sync.RWMutex

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
	p.current = 0
	if p.interval > 0 {
		ticker := time.NewTicker(p.interval)
		intervalDone := make(chan struct{}, 1)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					done, err := p.start(ctx)
					if err != nil {
						intervalDone <- struct{}{}
						return
					}
					<-done

					p.current++
				}
			}
		}()
		// We return a separate done channel from the program struct, as interval programs are long running ones
		// We only signal that we are done with an interval program if there is an error executing the program
		return intervalDone, nil
	}

	return p.start(ctx)
}

func (p *program) signalDone() {
	p.done <- struct{}{}
}

func (p *program) start(ctx context.Context) (<-chan struct{}, error) {
	if len(p.results) > 0 {
		previousRunState := p.results[len(p.results)-1].state
		if previousRunState == StateRunning || previousRunState == StateError {
			// We skip this run, as we do not want to have overlaping programs
			skipDone := make(chan struct{}, 1)
			skipDone <- struct{}{}
			return skipDone, nil
		}
	}

	//nolint:gosec // We accept the arguments the users have manually defined
	cmd := exec.CommandContext(
		ctx,
		p.command,
		p.arguments...)
	cmd.Env = append(os.Environ(), p.customEnv...)
	p.cmd = cmd

	results := result{}
	p.results = append(p.results, results)

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
		_ = inWriter.Close()
	}

	if p.async {
		return p.done, p.runAsync()
	}
	return p.done, p.run()
}

func (p *program) run() error {
	defer func() {
		p.signalDone()
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}()
	currentIndex := len(p.results) - 1
	p.results[currentIndex].state = StateRunning

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

	if err != nil {
		p.results[currentIndex].errorBuffer.WriteString(err.Error())
		p.results[currentIndex].errorBuffer.WriteString("\n")
		p.results[currentIndex].lastError = err.Error()
		p.results[currentIndex].state = StateError
		return err
	}

	p.results[currentIndex].state = StateFinished
	return nil
}

func (p *program) runAsync() error {
	currentIndex := len(p.results) - 1
	err := p.cmd.Start()

	if err != nil {
		p.results[currentIndex].state = StateError
		p.results[currentIndex].errorBuffer.WriteString(err.Error())
		p.results[currentIndex].errorBuffer.WriteString("\n")
		p.results[currentIndex].lastError = err.Error()
		p.signalDone()
		return err
	}
	p.results[currentIndex].state = StateRunning
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

	currentIndex := len(p.results) - 1
	if err != nil {
		p.results[currentIndex].errorBuffer.WriteString(err.Error())
		p.results[currentIndex].errorBuffer.WriteString("\n")
		p.results[currentIndex].lastError = err.Error()
		p.results[currentIndex].state = StateError
	} else {
		p.results[currentIndex].state = StateFinished
	}
	p.signalDone()
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		currentIndex := len(p.results) - 1

		if isError {
			p.stderrLock.Lock()
			p.results[currentIndex].errorBuffer.WriteString(line + "\n")
			p.results[currentIndex].lastError = line
			p.stderrLock.Unlock()
		} else {
			p.stdoutLock.Lock()
			p.results[currentIndex].outputBuffer.WriteString(line + "\n")
			p.results[currentIndex].lastLine = line
			p.stdoutLock.Unlock()
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if isError {
			currentIndex := len(p.results) - 1
			p.stderrLock.Lock()
			p.results[currentIndex].errorBuffer.WriteString("Scanner error: " + err.Error())
			p.results[currentIndex].errorBuffer.WriteString("\n")
			p.stderrLock.Unlock()
		}
	}
}

func (p *program) Send(data []byte) error {
	if len(p.results) == 0 || p.results[len(p.results)-1].state != StateRunning {
		return errors.New("can not send data to a non-running program")
	}

	if !p.keepStdinOpen {
		return errors.New("to send data to a running please use the KeepStdinOpen option when initialazing the program")
	}

	_, err := p.pipes.inWriter.Write(data)
	return err
}

func (p *program) CloseStdin() error {
	if len(p.results) == 0 || p.results[len(p.results)-1].state != StateRunning {
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

func (p *program) Status() string {
	if len(p.results) == 0 {
		return fmt.Sprintf("[%s not running] iteration: %d", p.name, p.current)
	}
	currentIndex := len(p.results) - 1
	switch p.results[currentIndex].state { //nolint:exhaustive // StateNotRunning is the default branch
	case StateRunning:
		return fmt.Sprintf(
			"[%s running] iteration: %d, pid: %d, last line: %s",
			p.name,
			p.current,
			p.cmd.Process.Pid,
			p.LastLine(),
		)
	case StateFinished:
		return fmt.Sprintf("[%s finished] iteration: %d, with exit code: %d", p.name, p.current, p.exitCode)
	case StateError:
		return fmt.Sprintf("[%s error] iteration: %d, code: %d", p.name, p.current, p.exitCode)
	default:
		return fmt.Sprintf("[%s not running] iteration: %d", p.name, p.current)
	}
}

func (p *program) Output() string {
	if len(p.results) == 0 {
		return ""
	}

	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()

	return p.results[len(p.results)-1].outputBuffer.String()
}

func (p *program) LastLine() string {
	if len(p.results) == 0 {
		return ""
	}

	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()

	return p.results[len(p.results)-1].lastLine
}

func (p *program) Error() string {
	if len(p.results) == 0 {
		return ""
	}

	p.stderrLock.RLock()
	defer p.stderrLock.RUnlock()

	return p.results[len(p.results)-1].errorBuffer.String()
}

func (p *program) State() ProcessState {
	if len(p.results) == 0 {
		return StateNotStarted
	}
	return p.results[len(p.results)-1].state
}

func (p *program) Interval() time.Duration {
	return p.interval
}

func (p *program) Runs() int {
	return p.current
}

func (p *program) Kill() error {
	defer close(p.done)

	if p.cmd == nil {
		return nil
	}

	err := p.cmd.Process.Kill()
	if err != nil && errors.Is(err, os.ErrProcessDone) {
		// Process already terminated, ignore this error
		return nil
	}
	return err
}

type Statistics struct {
	ProgramName       string
	TotalRuns         int
	Successful        int
	Failed            int
	Running           int
	TotalOutputLines  int
	LastSuccessfulRun int
	LastError         string
	LastOutput        string
	Interval          time.Duration
	HasInterval       bool
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
	stats := Statistics{
		ProgramName:       p.name,
		TotalRuns:         len(p.results),
		LastSuccessfulRun: -1,
		Interval:          p.interval,
		HasInterval:       p.interval > 0,
	}

	if stats.TotalRuns == 0 {
		return stats
	}

	for i, result := range p.results {
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
		for i := len(p.results) - 1; i >= 0; i-- {
			if p.results[i].state == StateError && p.results[i].lastError != "" {
				stats.LastError = p.results[i].lastError
				break
			}
		}
	}

	return stats
}

func New(name, command string, opts ...Option) Program {
	p := &program{
		name:    name,
		command: command,
		results: []result{},
		done:    make(chan struct{}, 1),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

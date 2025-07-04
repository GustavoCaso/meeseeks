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
	"time"
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
	Interval() time.Duration
	Runs() int
	Statistics() Statistics
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

var Interval = func(interval time.Duration) Option {
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

	p.current = 0
	if p.interval > 0 {
		ticker := time.NewTicker(p.interval)

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if p.async {
						p.start()
						for !p.Done() {
							time.Sleep(10 * time.Millisecond)
						}
					} else {
						p.start()
					}
					p.current += 1
				}
			}
		}()
		return nil
	} else {
		return p.start()
	}
}

func (p *program) start() error {
	results := result{}
	p.results = append(p.results, results)

	if p.current > 0 {
		previousRunState := p.results[p.current-1].state
		if !(previousRunState == StateRunning || previousRunState == StateError) {
			// We skip this run, as we do not want to have overlaping programs
			return nil
		}
	}

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
	p.results[p.current].state = StateRunning

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
		p.results[p.current].errorBuffer.WriteString(err.Error())
		p.results[p.current].errorBuffer.WriteString("\n")
		p.results[p.current].lastError = err.Error()
		p.results[p.current].state = StateError
		return err
	}

	p.results[p.current].state = StateFinished
	return nil
}

func (p *program) runAsync() error {
	err := p.cmd.Start()
	if err != nil {
		return err
	}
	p.results[p.current].state = StateRunning
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
		p.results[p.current].errorBuffer.WriteString(err.Error())
		p.results[p.current].errorBuffer.WriteString("\n")
		p.results[p.current].lastError = err.Error()
		p.results[p.current].state = StateError
	} else {
		p.results[p.current].state = StateFinished
	}
}

func (p *program) readOutput(reader io.Reader, isError bool) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()

		if isError {
			p.stderrLock.Lock()
			p.results[p.current].errorBuffer.WriteString(line + "\n")
			p.results[p.current].lastError = line
			p.stderrLock.Unlock()
		} else {
			p.stdoutLock.Lock()
			p.results[p.current].outputBuffer.WriteString(line + "\n")
			p.results[p.current].lastLine = line
			p.stdoutLock.Unlock()
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		if isError {
			p.stderrLock.Lock()
			p.results[p.current].errorBuffer.WriteString("Scanner error: " + err.Error())
			p.results[p.current].errorBuffer.WriteString("\n")
			p.stderrLock.Unlock()
		}
	}
}

func (p *program) Send(data []byte) error {
	if p.results[p.current].state != StateRunning {
		return fmt.Errorf("can not send data to a non-running program")
	}

	if !p.keepStdinOpen {
		return fmt.Errorf("to send data to a running please use the KeepStdinOpen option when initialazing the program")
	}

	_, err := p.pipes.inWriter.Write(data)
	return err
}

func (p *program) CloseStdin() error {
	if p.results[p.current].state != StateRunning {
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
	switch p.results[p.current].state {
	case StateRunning:
		return fmt.Sprintf("[%s running] iteration: %d, pid: %d, last line: %s", p.name, p.current, p.cmd.Process.Pid, p.LastLine())
	case StateFinished:
		return fmt.Sprintf("[%s finished] iteration: %d, with exit code: %d", p.name, p.current, p.exitCode)
	case StateError:
		return fmt.Sprintf("[%s error] iteration: %d, code: %d", p.name, p.current, p.exitCode)
	default:
		return fmt.Sprintf("[%s not running] iteration: %d", p.name, p.current)
	}
}

func (p *program) Done() bool {
	return p.results[p.current].state == StateFinished || p.results[p.current].state == StateError
}

func (p *program) Output() string {
	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()
	return p.results[p.current].outputBuffer.String()
}

func (p *program) LastLine() string {
	p.stdoutLock.RLock()
	defer p.stdoutLock.RUnlock()
	return p.results[p.current].lastLine
}

func (p *program) Error() string {
	p.stderrLock.RLock()
	defer p.stderrLock.RUnlock()
	return p.results[p.current].errorBuffer.String()
}

func (p *program) State() ProcessState {
	return p.results[p.current].state
}

func (p *program) Interval() time.Duration {
	return p.interval
}

func (p *program) Runs() int {
	return p.current
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
		switch result.state {
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
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

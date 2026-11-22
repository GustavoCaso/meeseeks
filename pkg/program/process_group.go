package program

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"
)

// DefaultKillGrace is the time a program's process group has to exit after
// SIGTERM when its context ends and no KillGrace option is set.
const DefaultKillGrace = 10 * time.Second

// processGroup coordinates termination of one run of a program, so SIGTERM is
// delivered once and no signal is sent after the run has been reaped, when the
// group ID could belong to an unrelated process.
//
// It watches the context itself rather than using exec.CommandContext, which
// force-kills the process a fixed WaitDelay after cancellation and so could
// not honor a longer Shutdown timeout.
type processGroup struct {
	cmd      *exec.Cmd
	ctx      context.Context //nolint:containedctx // Scoped to a single run
	grace    time.Duration
	finished chan struct{}

	mu          sync.Mutex
	platform    platformGroup
	terminating bool
	exited      bool
	cancelErr   error
	killTimer   *time.Timer
	killAt      time.Time
}

func newProcessGroup(ctx context.Context, cmd *exec.Cmd, grace time.Duration) *processGroup {
	return &processGroup{
		cmd:      cmd,
		ctx:      ctx,
		grace:    grace,
		finished: make(chan struct{}),
	}
}

// start launches the command, attaches it to its platform group, and begins
// watching the context. Holding the lock makes a concurrent termination wait
// until the group is attached. A failed attach is reported through
// onAttachError but does not fail the run, because the program can still be
// terminated individually.
func (g *processGroup) start(onAttachError func(error)) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.ctx.Err(); err != nil {
		return err
	}

	if err := g.cmd.Start(); err != nil {
		return err
	}

	if err := g.platform.attach(g.cmd.Process); err != nil {
		onAttachError(err)
	}

	go g.watchContext()
	return nil
}

func (g *processGroup) watchContext() {
	select {
	case <-g.finished:
		return
	case <-g.ctx.Done():
	}

	g.mu.Lock()
	if g.exited {
		g.mu.Unlock()
		return
	}
	g.cancelErr = g.ctx.Err()
	g.mu.Unlock()

	err := g.terminate(g.grace)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = g.kill()
	}
}

// terminate asks the group to exit and schedules a forced kill after grace.
// Repeated calls never resend SIGTERM, which could interrupt a cleanup trap,
// but can push the pending kill later. They never bring it forward.
func (g *processGroup) terminate(grace time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.exited {
		return os.ErrProcessDone
	}

	killAt := time.Now().Add(grace)

	if g.terminating {
		// A false Stop means the kill already fired, so there is nothing to extend.
		if killAt.After(g.killAt) && g.killTimer.Stop() {
			g.killAt = killAt
			g.killTimer.Reset(grace)
		}
		return nil
	}

	if err := g.platform.terminate(); err != nil {
		return err
	}

	g.terminating = true
	g.killAt = killAt
	g.killTimer = time.AfterFunc(grace, func() { _ = g.kill() })
	return nil
}

// kill force-kills the group unless the run has already been reaped.
func (g *processGroup) kill() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.exited {
		return os.ErrProcessDone
	}
	return g.platform.kill()
}

// markExited records that Wait returned and stops any pending kill. If the run
// was being terminated, descendants that outlived the leader are killed now,
// because the group cannot be signaled safely later.
func (g *processGroup) markExited() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.exited {
		return
	}
	g.exited = true
	close(g.finished)

	if g.killTimer != nil {
		g.killTimer.Stop()
	}
	if g.terminating {
		_ = g.platform.kill()
	}
	g.platform.close()
}

// exitError reports a context-ended run as failed even if its cleanup handler
// exited successfully, so deadlines still count as failures and trigger retries.
func (g *processGroup) exitError(waitErr error) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cancelErr != nil && (waitErr == nil || errors.Is(waitErr, exec.ErrWaitDelay)) {
		return g.cancelErr
	}
	return waitErr
}

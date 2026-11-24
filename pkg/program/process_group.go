package program

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// DefaultKillGrace is the time a program's process group has to exit after
// SIGTERM when its context ends and no KillGrace option is set.
const DefaultKillGrace = 10 * time.Second

const (
	// groupPollInterval is how often a terminating group is checked for
	// descendants that outlived the leader.
	groupPollInterval = 50 * time.Millisecond
	// killSettleTime bounds the wait for a group to disappear after its kill
	// deadline, so an unkillable process cannot block the run forever.
	killSettleTime = time.Second
)

// processGroup coordinates termination of one run of a program, so SIGTERM is
// delivered once and the per-run kill timer is the only source of a timed
// SIGKILL.
//
// The leader's exit and the end of the group are tracked separately. The leader
// stays unreaped until signaling ends, because its zombie keeps the group ID
// reserved. Reaping it earlier would let an unrelated group reuse that ID.
//
// It watches the context itself rather than using exec.CommandContext, which
// force-kills the process a fixed WaitDelay after cancellation and so could
// not honor a longer Shutdown timeout.
type processGroup struct {
	cmd      *exec.Cmd
	ctx      context.Context //nolint:containedctx // Scoped to a single run
	grace    time.Duration
	finished chan struct{}
	drainEnd chan struct{}
	endDrain sync.Once

	// onStarted runs after the process joins its group and before it is resumed,
	// which on Windows is before any of its code has executed.
	onStarted func(*os.Process)

	mu          sync.Mutex
	platform    platformGroup
	terminating bool
	exited      bool // The leader has exited, but may not be reaped yet.
	closed      bool // The group must not be signaled again.
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
		drainEnd: make(chan struct{}),
	}
}

// start launches the command, attaches it to its platform group, and begins
// watching the context. Holding the lock makes a concurrent termination wait
// until the group is attached. A failed attach is reported through
// onAttachError but does not fail the run, because the program can still be
// terminated individually. A failed resume does fail it, because the program
// would otherwise stay suspended forever.
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

	if g.onStarted != nil {
		g.onStarted(g.cmd.Process)
	}

	if err := g.platform.resume(); err != nil {
		_ = g.cmd.Process.Kill()
		_ = g.cmd.Wait()
		g.platform.close()
		return fmt.Errorf("resume process: %w", err)
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
// but can push the pending kill later, even after the leader has exited while
// descendants are still cleaning up. They never bring it forward.
func (g *processGroup) terminate(grace time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
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

// kill force-kills the group. The group is closed before the leader is reaped,
// so this can never signal a reused group ID.
func (g *processGroup) kill() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return os.ErrProcessDone
	}
	return g.platform.kill()
}

// killDeadline returns when the pending forced kill is due.
func (g *processGroup) killDeadline() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.killAt
}

// awaitLeaderExit blocks until the leader exits, without reaping it. A leader
// that exited on its own leaves its background processes running, so the group
// is released. During termination, descendants get the rest of their grace.
// Where exit cannot be observed this returns at once, and markReaped ends the
// group instead.
func (g *processGroup) awaitLeaderExit() {
	if !g.platform.waitLeader() {
		return
	}

	g.mu.Lock()
	g.exited = true
	close(g.finished)
	if !g.terminating {
		g.closeLocked()
	}
	g.mu.Unlock()

	g.awaitDescendants()
}

// markReaped records that Wait returned. No signal may follow, because the
// group ID is no longer reserved.
func (g *processGroup) markReaped() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.exited {
		g.exited = true
		close(g.finished)
	}
	if !g.closed {
		g.closeLocked()
	}
}

// awaitDescendants blocks until a terminating group has no live descendants.
// The kill timer enforces the deadline, including any extension made while
// waiting.
func (g *processGroup) awaitDescendants() {
	for {
		g.mu.Lock()
		if g.closed {
			g.mu.Unlock()
			return
		}
		if !g.platform.alive() || time.Now().After(g.killAt.Add(killSettleTime)) {
			g.closeLocked()
			g.mu.Unlock()
			return
		}
		g.mu.Unlock()

		time.Sleep(groupPollInterval)
	}
}

func (g *processGroup) closeLocked() {
	g.closed = true
	if g.killTimer != nil {
		g.killTimer.Stop()
	}
	g.platform.close()
}

// drainLimit is how long output may stay open once the group is settled. After
// termination only a process that escaped the group can still hold a pipe.
func (g *processGroup) drainLimit() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.terminating {
		return waitDelayMargin
	}
	return g.grace + waitDelayMargin
}

// stopDrain ends the wait for output held open by a background process.
func (g *processGroup) stopDrain() {
	g.endDrain.Do(func() { close(g.drainEnd) })
}

// exitError reports a context-ended run as failed even if its cleanup handler
// exited successfully, so deadlines still count as failures and trigger retries.
// A context that ends after the leader exited does not change the result.
func (g *processGroup) exitError(waitErr error) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cancelErr != nil && waitErr == nil {
		return g.cancelErr
	}
	return waitErr
}

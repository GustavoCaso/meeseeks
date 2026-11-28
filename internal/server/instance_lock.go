package server

import (
	"errors"
	"fmt"
	"os"
)

// errLockHeld is returned by lockFile when another open file holds the lock.
var errLockHeld = errors.New("lock held")

// instanceLock guarantees one daemon per socket path. The kernel owns the lock
// and drops it when the process dies, so a crash cannot leave a stale lock the
// way a PID file can. The file is not inherited by programs, so an orphaned
// program cannot keep the lock after its daemon is gone.
//
// The file is never deleted. Removing it would let one daemon lock the old
// inode while another creates and locks a new file at the same path.
type instanceLock struct {
	file *os.File
}

func instanceLockPath(sockPath string) string {
	return sockPath + ".lock"
}

func acquireInstanceLock(path string) (*instanceLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open instance lock: %w", err)
	}

	if err = lockFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errLockHeld) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("failed to acquire instance lock: %w", err)
	}

	return &instanceLock{file: file}, nil
}

// release drops the lock, which closing the file does on every platform.
func (l *instanceLock) release() error {
	return l.file.Close()
}

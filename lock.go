package devport

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

type FileLock struct {
	path string
	file *os.File
}

func NewFileLock(path string) *FileLock {
	return &FileLock{path: path}
}

// TryLock attempts a non-blocking exclusive flock.
// Returns true if the lock was acquired.
func (l *FileLock) TryLock() (bool, error) {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return false, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return false, nil
		}
		return false, err
	}
	l.file = f
	return true, nil
}

// Lock acquires a blocking exclusive flock.
func (l *FileLock) Lock() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return err
	}
	l.file = f
	return nil
}

// Unlock releases the flock and closes the file.
func (l *FileLock) Unlock() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// HolderPID returns the PID holding a flock on the given path, or 0 if none.
func HolderPID(path string) (int, error) {
	out, err := exec.Command("lsof", "-t", path).Output()
	if err != nil {
		// lsof exits non-zero when no process holds the file
		return 0, nil
	}
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		return 0, nil
	}
	var pid int
	if _, err := fmt.Sscanf(lines[0], "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

// ChildPID returns the PID of the direct child of the given parent process, or 0 if none.
func ChildPID(parentPID int) (int, error) {
	out, err := exec.Command("pgrep", "-P", fmt.Sprintf("%d", parentPID)).Output()
	if err != nil {
		// pgrep exits non-zero when no children found
		return 0, nil
	}
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		return 0, nil
	}
	var pid int
	if _, err := fmt.Sscanf(lines[0], "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

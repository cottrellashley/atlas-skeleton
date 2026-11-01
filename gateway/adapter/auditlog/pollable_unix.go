//go:build unix

package auditlog

import (
	"os"
	"syscall"
)

// pollable is a duplicate of f's descriptor in non-blocking mode, as an
// *os.File the runtime's poller drives, so its writes take a deadline. The
// mode is a property of the open file the two descriptors share, so the
// original is non-blocking too: the sink is its only writer (standard
// output, given to the audit, carries nothing else; logs go to standard
// error).
func pollable(f *os.File) (*os.File, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}
	nfd, derr := -1, error(nil)
	if err := rc.Control(func(fd uintptr) {
		if nfd, derr = syscall.Dup(int(fd)); derr == nil {
			derr = syscall.SetNonblock(nfd, true)
		}
	}); err != nil {
		return nil, err
	}
	if derr != nil {
		if nfd >= 0 {
			_ = syscall.Close(nfd)
		}
		return nil, derr
	}
	return os.NewFile(uintptr(nfd), f.Name()), nil
}

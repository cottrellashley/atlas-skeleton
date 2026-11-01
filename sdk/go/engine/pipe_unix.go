//go:build unix

package engine

import "syscall"

// pollable puts the results pipe in non-blocking mode, so os.NewFile
// returns a File whose writes a deadline can cut.
func pollable(fd int) { _ = syscall.SetNonblock(fd, true) }

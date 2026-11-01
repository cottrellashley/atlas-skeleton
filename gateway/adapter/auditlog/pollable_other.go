//go:build !unix

package auditlog

import (
	"errors"
	"os"
)

// pollable is not offered here: a pipe's writes are unbounded.
func pollable(*os.File) (*os.File, error) {
	return nil, errors.New("auditlog: no pollable descriptor on this platform")
}

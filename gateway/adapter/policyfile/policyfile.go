// Package policyfile is a PolicySource read from a JSON file: who may do
// what, kept in the deployment's own configuration, for a gateway whose
// grants change by review rather than by an administration API.
//
// The file is read at every renewal, and a change is found by its content.
// A file that is missing, unreadable, larger than MaxSize or not a valid
// policy is an error, every time it is asked for, so the gateway keeps
// deciding by the last good policy only for its bound and then refuses
// calls (R13): a bad edit never opens access by accident, and a revocation
// that did not take hold never goes unnoticed (PR02). Write the file whole
// (a temporary file, then a rename), so no read sees half of it.
package policyfile

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// MaxSize is the largest policy file read: a policy is a few kilobytes,
// and a file this large is a mistake, never a policy.
const MaxSize = 4 << 20

// Source reads a policy file.
type Source struct {
	path string

	mu  sync.Mutex
	sum [sha256.Size]byte // the content last read
	p   *ports.Policy     // what it held, when it was a policy
	err error             // why it was not
}

// New is a Source over path. It reads the file once now, so a gateway with
// a bad policy file does not start.
func New(path string) (*Source, error) {
	s := &Source{path: path}
	if _, err := s.Policy(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Policy is the policy the file holds now, or why it holds none. The read
// cannot be interrupted once it starts, so ctx is checked before it.
func (s *Source) Policy(ctx context.Context) (*ports.Policy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if sum != s.sum || s.p == nil && s.err == nil {
		s.sum = sum
		s.p, s.err = ports.ParsePolicy(b)
		if s.err != nil {
			s.err = fmt.Errorf("policyfile: %s: %w", s.path, s.err)
		}
	}
	return s.p, s.err
}

// read is the file's content, at most MaxSize bytes of it.
func (s *Source) read() ([]byte, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("policyfile: the policy cannot be read: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("policyfile: the policy cannot be read: %s: %w", s.path, err)
	}
	if len(b) > MaxSize {
		return nil, fmt.Errorf("policyfile: %s is larger than a policy may be (%d bytes)", s.path, MaxSize)
	}
	return b, nil
}

// Parse reads a policy as every policy is read (ports.ParsePolicy, R134):
// refusing a member it does not know, a policy with no revision, and an
// entry in a form the gateway does not decide by.
func Parse(b []byte) (*ports.Policy, error) {
	p, err := ports.ParsePolicy(b)
	if err != nil {
		return nil, fmt.Errorf("policyfile: %w", err)
	}
	return p, nil
}

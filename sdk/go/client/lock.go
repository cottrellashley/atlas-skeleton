package client

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// LockFile is the name of an application's lock (§3.3.10).
const LockFile = "atlas.lock"

// Lock is an application's reader profiles: for each capability version it
// uses, the exact profile it was built against. Generated types and
// resolution come from it, and an application that holds one makes no
// discovery call to know its contracts (ATLAS-VISION-004).
type Lock struct {
	Path         string            `toml:"-"`
	Capabilities map[string]string `toml:"capabilities"`
}

// LoadLock reads a lock.
func LoadLock(path string) (*Lock, error) {
	l := &Lock{Path: path}
	if _, err := toml.DecodeFile(path, l); err != nil {
		return nil, err
	}
	for pinned, exact := range l.Capabilities {
		e, err := protocol.ParseExact(exact)
		if err != nil || e.Pinned.String() != pinned {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, path+": "+pinned+" is locked to "+exact)
		}
	}
	return l, nil
}

// FindLock looks for atlas.lock in dir and each parent; nil when none.
func FindLock(dir string) (*Lock, error) {
	for {
		path := filepath.Join(dir, LockFile)
		if _, err := os.Stat(path); err == nil {
			return LoadLock(path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

// Save writes the lock, entries sorted.
func (l *Lock) Save() error {
	keys := make([]string, 0, len(l.Capabilities))
	for k := range l.Capabilities {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("# The exact profiles this application was built against. Written by atlas lock.\n[capabilities]\n")
	for _, k := range keys {
		buf.WriteString(`"` + k + `" = "` + l.Capabilities[k] + `"` + "\n")
	}
	tmp := l.Path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.Path)
}

// LockCurrent pins each capability version to its current profile, as
// atlas lock does, and saves the lock at path.
func (c *Client) LockCurrent(ctx context.Context, pinned []string, path string) (*Lock, error) {
	l := &Lock{Path: path, Capabilities: map[string]string{}}
	if existing, err := LoadLock(path); err == nil {
		l = existing
	}
	for _, p := range pinned {
		ans, err := c.Describe(ctx, p)
		if err != nil {
			return nil, err
		}
		l.Capabilities[ans.Profile.Pinned().String()] = ans.Exact
	}
	return l, l.Save()
}

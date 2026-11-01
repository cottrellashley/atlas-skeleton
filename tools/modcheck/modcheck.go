// Package modcheck proves that nothing in this repository can fetch one of
// its own modules. Their paths are under github.com/jhgonline, a private
// organisation, and are names only: every in-repo module that requires
// another requires it through a replace to that module's directory, and every
// module is in go.work, so the go command never asks the public proxy, the
// checksum database or GitHub for them.
package modcheck

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

// Private is the organisation no command may reach.
const Private = "github.com/jhgonline/"

// Module is one go.mod of the repository.
type Module struct {
	Dir  string // relative to the repository's root, with slashes
	Path string
	File *modfile.File
}

// skipped are directories that hold no module of ours.
var skipped = map[string]bool{".git": true, ".bin": true, "node_modules": true, ".venv": true, "testdata": true}

// Modules reads every go.mod under root.
func Modules(root string) ([]Module, error) {
	var out []Module
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && skipped[d.Name()] {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "go.mod" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := modfile.Parse(path, data, nil)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		if f.Module == nil {
			return fmt.Errorf("%s: no module line", path)
		}
		out = append(out, Module{Dir: filepath.ToSlash(rel), Path: f.Module.Mod.Path, File: f})
		return nil
	})
	slices.SortFunc(out, func(a, b Module) int { return strings.Compare(a.Dir, b.Dir) })
	return out, err
}

// Check lists every way the repository's modules could reach the private
// organisation: a requirement of one of its paths without a replace to the
// directory that holds it, a replace that points anywhere else, and a module
// that go.work leaves out.
func Check(root string, mods []Module, work *modfile.WorkFile) []string {
	var out []string
	byPath := map[string]Module{}
	for _, m := range mods {
		if !strings.HasPrefix(m.Path, Private) {
			out = append(out, fmt.Sprintf("%s: module %s is not under %s", m.Dir, m.Path, Private))
		}
		byPath[m.Path] = m
	}
	for _, m := range mods {
		replaced := map[string]string{}
		for _, r := range m.File.Replace {
			if strings.HasPrefix(r.Old.Path, Private) {
				replaced[r.Old.Path] = r.New.Path
				if r.New.Version != "" || !modfile.IsDirectoryPath(r.New.Path) {
					out = append(out, fmt.Sprintf("%s: replace %s => %s %s: an in-repo module is replaced by its directory, never by a module version", m.Dir, r.Old.Path, r.New.Path, r.New.Version))
				}
			}
		}
		for _, req := range m.File.Require {
			if !strings.HasPrefix(req.Mod.Path, Private) {
				continue
			}
			target, ok := byPath[req.Mod.Path]
			if !ok {
				out = append(out, fmt.Sprintf("%s: requires %s, which is no module in this repository", m.Dir, req.Mod.Path))
				continue
			}
			to, ok := replaced[req.Mod.Path]
			if !ok {
				out = append(out, fmt.Sprintf("%s: requires %s with no replace, so the go command would ask for it", m.Dir, req.Mod.Path))
				continue
			}
			if got := filepath.ToSlash(filepath.Clean(filepath.Join(m.Dir, to))); got != target.Dir {
				out = append(out, fmt.Sprintf("%s: replace %s => %s points at %s, not %s", m.Dir, req.Mod.Path, to, got, target.Dir))
			}
		}
	}
	if work == nil {
		return append(out, "go.work is missing")
	}
	used := map[string]bool{}
	for _, u := range work.Use {
		used[filepath.ToSlash(filepath.Clean(u.Path))] = true
	}
	for _, m := range mods {
		if !used[m.Dir] {
			out = append(out, fmt.Sprintf("go.work leaves out %s", m.Dir))
		}
	}
	for _, r := range work.Replace {
		if strings.HasPrefix(r.Old.Path, Private) && r.New.Version != "" {
			out = append(out, fmt.Sprintf("go.work: replace %s => %s %s: never a module version", r.Old.Path, r.New.Path, r.New.Version))
		}
	}
	return out
}

// Work reads root's go.work.
func Work(root string) (*modfile.WorkFile, error) {
	path := filepath.Join(root, "go.work")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return modfile.ParseWork(path, data, nil)
}

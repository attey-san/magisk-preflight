package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Module is a Magisk module loaded into memory, from either a directory or a
// zip. Paths always use forward slashes and are relative to the module root,
// which is whatever contains module.prop.
type Module struct {
	Root  string            // directory or zip path as given by the user
	Files map[string][]byte // relative path -> contents
}

// names returns the file paths in deterministic order.
func (m *Module) names() []string {
	out := make([]string, 0, len(m.Files))
	for n := range m.Files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// has reports whether the module contains path (case-sensitive).
func (m *Module) has(path string) bool {
	_, ok := m.Files[path]
	return ok
}

// text returns the contents of path decoded as UTF-8-ish text with NUL bytes
// stripped, plus whether the file exists. Binary files still return their raw
// bytes; rules that care check for themselves.
func (m *Module) text(path string) (string, bool) {
	b, ok := m.Files[path]
	if !ok {
		return "", false
	}
	return strings.ReplaceAll(string(b), "\x00", ""), true
}

var skipDirs = map[string]bool{
	".git":         true,
	".github":      true,
	"node_modules": true,
	"testdata":     true,
}

// loadDir reads every regular file under dir into a Module. The module root is
// dir itself; if module.prop only exists one level down (a common accident),
// load still succeeds but the nesting rule will flag it via lintZipLayout on
// zip input — directories are used as-is.
func loadDir(dir string) (*Module, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	m := &Module{Root: dir, Files: map[string][]byte{}}
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		m.Files[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// loadZip reads a Magisk module zip into a Module. Paths are normalised
// (leading ./ stripped) but no directory level is removed: a zip nested one
// folder deep stays nested so the layout rule can report it, which is what
// lint exists for. simulate and new both operate on correct layouts.
func loadZip(path string) (*Module, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	m := &Module{Root: path, Files: map[string][]byte{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		name = strings.TrimPrefix(name, "./")
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		m.Files[name] = b
	}
	return m, nil
}

// nestedPrefix finds "dir/" when every entry lives under a single top-level
// folder, so a wrongly-nested zip can still be analysed (and reported).
func nestedPrefix(m *Module) string {
	var prefix string
	first := true
	for n := range m.Files {
		i := strings.IndexByte(n, '/')
		if i < 0 {
			return ""
		}
		p := n[:i+1]
		if first {
			prefix = p
			first = false
		} else if p != prefix {
			return ""
		}
	}
	return prefix
}

// load opens a module directory or zip based on extension.
func load(path string) (*Module, error) {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return loadZip(path)
	}
	return loadDir(path)
}

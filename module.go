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

// maxEntryBytes caps a single decompressed zip entry. Module files are
// scripts, props and small images; anything near this bound is either junk or
// a decompression bomb.
const maxEntryBytes = 64 << 20

// Module is a Magisk module loaded into memory, from either a directory or a
// zip. Paths always use forward slashes and are relative to the module root,
// which is whatever contains module.prop.
type Module struct {
	Root     string            // directory or zip path as given by the user
	Files    map[string][]byte // relative path -> contents
	Symlinks map[string]string // relative path -> link target
	Warnings []string          // non-fatal load problems worth reporting
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
}

// loadDir reads every regular file under dir into a Module. Symlinks are
// recorded rather than followed: linting the target's content would report
// findings against files the module does not actually ship.
func loadDir(dir string) (*Module, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory (zip? pass the .zip file)", dir)
	}
	m := &Module{Root: dir, Files: map[string][]byte{}, Symlinks: map[string]string{}}
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
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !d.Type().IsRegular() {
			if d.Type()&os.ModeSymlink != 0 {
				target, err := os.Readlink(p)
				if err == nil {
					m.Symlinks[name] = target
				}
				return nil
			}
			m.warnf("skipped %s: not a regular file", name)
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			// A linter should degrade gracefully; one unreadable file does
			// not invalidate analysis of the rest.
			m.warnf("skipped %s: %v", name, err)
			return nil
		}
		m.Files[name] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Module) warnf(format string, args ...any) {
	m.Warnings = append(m.Warnings, fmt.Sprintf(format, args...))
}

// cleanZipName validates one archive entry name and returns it in
// module-relative form. It rejects absolute paths, .. segments, NUL bytes and
// Windows drive prefixes — none of which belong in a Magisk module, and all
// of which would otherwise corrupt path matching downstream.
func cleanZipName(name string) (string, error) {
	if strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("entry name contains NUL")
	}
	name = filepath.ToSlash(name)
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute entry path")
	}
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("windows drive prefix")
	}
	cleaned := pathSegments(name)
	if cleaned == "" {
		return "", fmt.Errorf("empty entry path")
	}
	return cleaned, nil
}

// pathSegments joins the non-empty, non-dot, non-dotdot segments of a slashed
// path. Any ".." segment makes the whole name invalid, so it returns "" then.
func pathSegments(name string) string {
	var out []string
	for _, seg := range strings.Split(name, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			return ""
		default:
			out = append(out, seg)
		}
	}
	return strings.Join(out, "/")
}

// loadZip reads a Magisk module zip into a Module. Paths are normalised but
// no directory level is removed: a zip nested one folder deep stays nested so
// the layout rule can report it, which is what lint exists for. Symlink
// entries are recorded as links, not read as their target's content.
func loadZip(path string) (*Module, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	m := &Module{Root: path, Files: map[string][]byte{}, Symlinks: map[string]string{}}
	seen := map[string]bool{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, err := cleanZipName(f.Name)
		if err != nil {
			m.warnf("skipped entry %q: %v", f.Name, err)
			continue
		}
		if seen[name] {
			m.warnf("duplicate entry %q; last one wins", name)
		}
		seen[name] = true

		if f.Mode()&os.ModeSymlink != 0 {
			b, err := readZipEntry(f)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			m.Symlinks[name] = string(b)
			continue
		}
		b, err := readZipEntry(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		m.Files[name] = b
	}
	if len(m.Files) == 0 && len(m.Symlinks) == 0 {
		return nil, fmt.Errorf("archive contains no file entries")
	}
	return m, nil
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxEntryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxEntryBytes {
		return nil, fmt.Errorf("entry larger than %d bytes after decompression", maxEntryBytes)
	}
	return b, nil
}

// nestedPrefix finds "dir/" when every entry lives under a single top-level
// folder, so the layout rule can name it in its finding.
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

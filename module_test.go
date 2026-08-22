package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipDir packs dir into a zip at dest, preserving relative paths.
func zipDir(dir, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}

func TestLoadZipKeepsNestedLayoutForReporting(t *testing.T) {
	src := t.TempDir()
	if err := writeFixture(filepath.Join(src, "wrapped"), map[string]string{
		"module.prop": goodProp,
	}); err != nil {
		t.Fatal(err)
	}
	zp := filepath.Join(t.TempDir(), "m.zip")
	if err := zipDir(src, zp); err != nil {
		t.Fatal(err)
	}
	m, err := load(zp)
	if err != nil {
		t.Fatal(err)
	}
	if m.has("module.prop") {
		t.Fatalf("nested zip must keep wrapped/ prefix for the layout rule to see it")
	}
	registerAll()
	fs := runRules(m)
	found := false
	for _, f := range fs {
		if f.Rule == "layout" && strings.Contains(f.Message, "wrapped/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("layout rule did not flag nested zip: %v", fs)
	}
}

func TestZipWithRootPropStaysFlat(t *testing.T) {
	src := t.TempDir()
	writeFixture(src, map[string]string{"module.prop": goodProp, "system/x": "y\n"})
	zp := filepath.Join(t.TempDir(), "flat.zip")
	if err := zipDir(src, zp); err != nil {
		t.Fatal(err)
	}
	m, _ := load(zp)
	if !m.has("module.prop") || !m.has("system/x") {
		t.Fatalf("flat zip should keep paths: %v", m.names())
	}
}

func TestScaffoldLintsClean(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "myscaffold")
	files := scaffold("myscaffold")
	if err := writeFixture(root, files); err != nil {
		t.Fatal(err)
	}
	m, err := loadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	registerAll()
	fs := runRules(m)
	for _, f := range fs {
		if f.Severity == SevError {
			t.Errorf("scaffold should not produce errors, got: %s", f.String())
		}
	}
	// LF endings everywhere in generated text.
	for name, body := range files {
		if strings.Contains(body, "\r\n") {
			t.Errorf("%s: scaffold wrote CRLF", name)
		}
	}
}

func TestExitCodes(t *testing.T) {
	clean := t.TempDir()
	writeFixture(clean, cleanModule)

	broken := t.TempDir()
	writeFixture(broken, cleanModule)
	writeFixture(broken, brokenByRule["prop"])

	registerAll()

	loadClean, _ := loadDir(clean)
	if worst(runRules(loadClean)) >= SevError {
		t.Error("clean fixture unexpectedly has error findings")
	}
	loadBroken, _ := loadDir(broken)
	w := worst(runRules(loadBroken))
	if w != SevError {
		t.Errorf("broken prop fixture should be error severity, got %s", w)
	}
}

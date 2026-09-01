package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newZipBuffer builds an in-memory zip via the callback and returns the bytes.
func newZipBuffer(t *testing.T, fill func(zw *zip.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fill(zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCleanZipName(t *testing.T) {
	ok := map[string]string{
		"module.prop":   "module.prop",
		"./module.prop": "module.prop",
		"././system/x":  "system/x",
		"a//b":          "a/b",
		`a\b`:           "a/b",
		"META-INF/com/google/android/update-binary": "META-INF/com/google/android/update-binary",
	}
	for in, want := range ok {
		got, err := cleanZipName(in)
		if err != nil || got != want {
			t.Errorf("cleanZipName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"/etc/passwd",
		"../evil",
		"a/../../evil",
		"..",
		"C:/x",
		"a\x00b",
		"./..",
		`..\..\etc\passwd`,
		`system\..\..\evil`,
	}
	for _, in := range bad {
		if got, err := cleanZipName(in); err == nil {
			t.Errorf("cleanZipName(%q) = %q, want error", in, got)
		}
	}
}

func TestLoadZipRejectsTraversalEntriesWithWarning(t *testing.T) {
	zp := filepath.Join(t.TempDir(), "m.zip")
	zf, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	wGood, _ := zw.Create("module.prop")
	wGood.Write([]byte(goodProp))
	w, _ := zw.Create("/abs/module.prop")
	w.Write([]byte(goodProp))
	w2, _ := zw.Create("../escape.sh")
	w2.Write([]byte("x\n"))
	zw.Close()
	zf.Close()

	m, err := load(zp)
	if err != nil {
		t.Fatalf("valid entries alongside bad ones should still load: %v", err)
	}
	if len(m.Warnings) != 2 {
		t.Errorf("expected 2 warnings for rejected entries, got %v", m.Warnings)
	}
	if m.has("module.prop") != true {
		t.Error("the real module.prop must still load")
	}
}

func TestLoadZipSymlinkEntryRecordedNotRead(t *testing.T) {
	buf := newZipBuffer(t, func(zw *zip.Writer) {
		w, _ := zw.Create("module.prop")
		w.Write([]byte(goodProp))
		link := &zip.FileHeader{Name: "service.sh", Method: zip.Deflate}
		link.SetMode(os.ModeSymlink | 0o777)
		lw, _ := zw.CreateHeader(link)
		lw.Write([]byte("/system/xbin/busybox"))
	})
	zp := filepath.Join(t.TempDir(), "m.zip")
	os.WriteFile(zp, buf, 0o644)

	m, err := load(zp)
	if err != nil {
		t.Fatal(err)
	}
	if m.has("service.sh") {
		t.Error("symlink entry must not appear as file content")
	}
	if m.Symlinks["service.sh"] != "/system/xbin/busybox" {
		t.Errorf("symlink target not recorded: %v", m.Symlinks)
	}
	registerAll()
	fs := runRules(m)
	found := false
	for _, f := range fs {
		if f.Rule == "symlink" && f.Severity == SevWarning && strings.Contains(f.Message, "/system/xbin/busybox") {
			found = true
		}
	}
	if !found {
		t.Errorf("absolute symlink should warn: %v", fs)
	}
}

func TestLoadDirRecordsSymlinksAndSkipsUnreadable(t *testing.T) {
	root := t.TempDir()
	writeFixture(root, map[string]string{"module.prop": goodProp, "real.txt": "x\n"})
	os.Symlink("/etc/hostname", filepath.Join(root, "link"))
	unreadable := filepath.Join(root, "secret.sh")
	os.WriteFile(unreadable, []byte("#!/system/bin/sh\n"), 0o000)

	m, err := loadDir(root)
	if err != nil {
		t.Fatalf("one unreadable file must not abort the load: %v", err)
	}
	if m.Symlinks["link"] != "/etc/hostname" {
		t.Errorf("symlink not recorded: %v", m.Symlinks)
	}
	if len(m.Warnings) == 0 {
		t.Error("unreadable file should produce a warning")
	}
	os.Chmod(unreadable, 0o644)
}

func TestEmptyArchiveIsCleanError(t *testing.T) {
	buf := newZipBuffer(t, func(zw *zip.Writer) {})
	zp := filepath.Join(t.TempDir(), "empty.zip")
	os.WriteFile(zp, buf, 0o644)
	_, err := load(zp)
	if err == nil || !strings.Contains(err.Error(), "no file entries") {
		t.Errorf("empty archive should say so, got: %v", err)
	}
}

// TestQuotedStringsDoNotTripCommandRules pins the false positive where the
// word appears only inside a string or comment.
func TestQuotedStringsDoNotTripCommandRules(t *testing.T) {
	cases := map[string]bool{ // line -> expect a postfsdata finding
		`echo "do not sleep in post-fs-data"`:     false,
		`# curl is banned here`:                   false,
		`MSG='wait for it'`:                       false,
		`sleep 5`:                                 true,
		`curl https://x >/dev/null`:               true,
		`echo hi # sleep would be a mistake here`: false,
		`echo "# not a comment"; sleep 1`:         true,
	}
	// Evaluate each line separately for precise attribution.
	for line, want := range cases {
		overlay := map[string]string{
			"post-fs-data.sh": "#!/system/bin/sh\n" + line + "\n",
		}
		fs := lintFixture(t, overlay)
		hits := len(fs["postfsdata"]) > 0
		if hits != want {
			t.Errorf("line %q: postfsdata fired = %v, want %v", line, hits, want)
		}
	}
}

func TestSystemWideSkipMount(t *testing.T) {
	root := t.TempDir()
	writeFixture(root, map[string]string{
		"module.prop":        goodProp,
		"system/.skip_mount": "",
		"system/bin/ls":      "x\n",
	})
	m, _ := loadDir(root)
	p := resolvePlan(m)
	if len(p.Overlays) != 1 || !p.Overlays[0].Skipped {
		t.Fatalf("system/.skip_mount should skip everything: %+v", p.Overlays)
	}
}

func TestVendorNoteSurvivesSystemWideMarkers(t *testing.T) {
	for _, marker := range []string{"system/.skip_mount", "system/.replace"} {
		root := t.TempDir()
		writeFixture(root, map[string]string{
			"module.prop":   goodProp,
			marker:          "",
			"system/bin/ls": "x\n",
			"vendor/lib/x":  "y\n",
		})
		m, _ := loadDir(root)
		p := resolvePlan(m)
		var found bool
		for _, o := range p.Overlays {
			if o.VendorBare {
				found = true
			}
		}
		if !found {
			t.Errorf("%s hid the bare vendor/ note: %+v", marker, p.Overlays)
		}
	}
}

func TestOversizedEntryWarnsAndKeepsLinting(t *testing.T) {
	zp := filepath.Join(t.TempDir(), "m.zip")
	zf, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	wGood, _ := zw.Create("module.prop")
	wGood.Write([]byte(goodProp))
	// Zeros so the archive on disk stays small; the guard trips on the
	// decompressed length.
	wBig, _ := zw.Create("system/blob")
	wBig.Write(make([]byte, maxEntryBytes+1))
	zw.Close()
	zf.Close()

	m, err := load(zp)
	if err != nil {
		t.Fatalf("one oversized entry sank the whole archive: %v", err)
	}
	if _, ok := m.Files["system/blob"]; ok {
		t.Error("oversized entry should not be loaded")
	}
	if _, ok := m.Files["module.prop"]; !ok {
		t.Error("the readable entries should survive")
	}
	if len(m.Warnings) == 0 {
		t.Error("skipping an entry should be reported")
	}
}

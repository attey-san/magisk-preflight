package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// lintFixture materialises the clean module plus one broken overlay into a
// temp dir and lints it, returning findings keyed by rule id.
func lintFixture(t *testing.T, overlay map[string]string) map[string][]Finding {
	t.Helper()
	root := filepath.Join(t.TempDir(), "mod")
	if err := writeFixture(root, cleanModule); err != nil {
		t.Fatal(err)
	}
	if err := writeFixture(root, overlay); err != nil {
		t.Fatal(err)
	}
	m, err := loadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	registerAll()
	fs := runRules(m)
	out := map[string][]Finding{}
	for _, f := range fs {
		out[f.Rule] = append(out[f.Rule], f)
	}
	return out
}

func TestCleanModulePasses(t *testing.T) {
	fs := lintFixture(t, nil)
	if len(fs) != 0 {
		for rule, list := range fs {
			for _, f := range list {
				t.Errorf("unexpected %s finding: %s", rule, f.String())
			}
		}
	}
}

// TestOneRulePerFixture walks every deliberately-broken fixture and checks
// that (a) its own rule fired and (b) no unrelated structural rule did. Rules
// that legitimately cascade (safemode reacts to other rules' output) are
// allowed to appear in company.
func TestOneRulePerFixture(t *testing.T) {
	cases := map[string]struct {
		want    string
		minSev  Severity
		excused []string // rules allowed to fire as a side effect
	}{
		"meta":       {"meta", SevError, nil},
		"prop":       {"prop", SevError, nil},
		"crlf":       {"crlf", SevError, nil},
		"shebang":    {"shebang", SevError, nil},
		"bashism":    {"bashism", SevError, nil},
		"su":         {"su", SevError, []string{"partition"}},
		"postfsdata": {"postfsdata", SevError, []string{"safemode"}},
		"partition":  {"partition", SevError, []string{"safemode"}},
		"vendordir":  {"vendordir", SevError, nil},
		"iptablesw":  {"iptablesw", SevError, []string{"safemode"}},
		"safemode":   {"safemode", SevWarning, []string{"postfsdata"}},
		"tls":        {"tls", SevError, []string{"postfsdata", "safemode"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			overlay := brokenByRule[name]
			if overlay == nil {
				t.Fatalf("no fixture for rule %q", name)
			}
			fs := lintFixture(t, overlay)

			hits := fs[tc.want]
			if len(hits) == 0 {
				t.Fatalf("rule %q did not fire; got %v", tc.want, ruleNames(fs))
			}
			if worst(hits) < tc.minSev {
				t.Errorf("rule %q fired below expected severity: %s", tc.want, hits[0].String())
			}
			for rule := range fs {
				if rule == tc.want || containsStr(tc.excused, rule) {
					continue
				}
				t.Errorf("unrelated rule %q fired: %s", rule, firstMsg(fs, rule))
			}
			// Every finding must carry file context and a real message.
			for _, f := range hits {
				if f.File == "" {
					t.Errorf("%s: finding without file", tc.want)
				}
				if f.Message == "" {
					t.Errorf("%s: finding without message", tc.want)
				}
			}
		})
	}
}

func TestZipLayoutRuleOnNestedArchive(t *testing.T) {
	nested := map[string]string{
		"inner/module.prop": goodProp,
		"inner/META-INF/com/google/android/updater-script": goodUpdater,
	}
	root := t.TempDir()
	if err := writeFixture(root, nested); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "nested.zip")
	if err := zipDir(root, zipPath); err != nil {
		t.Fatal(err)
	}
	m, err := load(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	registerAll()
	fs := runRules(m)
	found := false
	for _, f := range fs {
		if f.Rule == "layout" && strings.Contains(f.Message, "inner/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("layout rule did not flag nested zip: %v", fs)
	}
}

func TestSimulateReplaceAndSkipMount(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"module.prop":                      goodProp,
		"system/etc/.replace":              "",
		"system/etc/mkshrc":                "replaced\n",
		"system/priv-app/Skip/.skip_mount": "",
		"system/app/Real.apk":              "binary-ish\n",
	}
	if err := writeFixture(root, files); err != nil {
		t.Fatal(err)
	}
	m, err := loadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	p := resolvePlan(m)

	var etc, skip, app *overlay
	for i := range p.Overlays {
		switch o := &p.Overlays[i]; o.Target {
		case "/etc":
			etc = o
		case "/priv-app":
			skip = o
		case "/app":
			app = o
		}
	}
	if etc == nil || etc.Mode != "replace" {
		t.Fatalf("/etc should be replace, got %+v", etc)
	}
	if app == nil || app.Mode != "merge" || len(app.Files) != 1 {
		t.Fatalf("/app should merge with one file, got %+v", app)
	}
	if skip == nil || !skip.Skipped || len(skip.Files) != 0 {
		t.Fatalf(".skip_mount subtree should be skipped with no files, got %+v", skip)
	}
}

func TestSimulateScriptOrder(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"module.prop":     goodProp,
		"service.sh":      "#!/system/bin/sh\n",
		"post-fs-data.sh": "#!/system/bin/sh\n",
		"customize.sh":    "ui_print hi\n",
		"action.sh":       "echo action\n",
		"uninstall.sh":    "echo bye\n",
	}
	if err := writeFixture(root, files); err != nil {
		t.Fatal(err)
	}
	m, _ := loadDir(root)
	p := resolvePlan(m)
	got := make([]string, 0, len(p.Scripts))
	for _, s := range p.Scripts {
		got = append(got, s.Path)
	}
	want := []string{"customize.sh", "post-fs-data.sh", "service.sh", "uninstall.sh", "action.sh"}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("script order:\n got %v\nwant %v", got, want)
		}
	}
	if !p.Scripts[1].Block {
		t.Error("post-fs-data.sh must be marked blocking")
	}
}

func TestVendorBareInSimulate(t *testing.T) {
	root := t.TempDir()
	writeFixture(root, map[string]string{
		"module.prop":      goodProp,
		"vendor/lib/nv.so": "\x7fELFfake",
	})
	m, _ := loadDir(root)
	p := resolvePlan(m)
	if len(p.Overlays) == 0 || !p.Overlays[0].VendorBare {
		t.Fatalf("bare vendor/ should surface as not-mounted overlay: %+v", p.Overlays)
	}
}

func TestParseModuleProp(t *testing.T) {
	props, ok := parseModuleProp("id=x\nname=A B\ndesc=line1\nline2\n")
	if ok {
		t.Error("line without '=' should mark prop unparsable")
	}
	if props["id"] != "x" || props["desc"] != "line1" {
		t.Errorf("parsed wrong: %v", props)
	}
	// value containing '=' stays intact
	props, _ = parseModuleProp("description=a=b=c\n")
	if props["description"] != "a=b=c" {
		t.Errorf("value after second '=' lost: %q", props["description"])
	}
}

func TestAPIMention(t *testing.T) {
	if n := parseAPIMention("# supports android up to api 25"); n != 25 {
		t.Errorf("api 25 not parsed, got %d", n)
	}
	if n := parseAPIMention("nothing here"); n != 0 {
		t.Errorf("expected 0 for no mention, got %d", n)
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func ruleNames(fs map[string][]Finding) []string {
	out := []string{}
	for r := range fs {
		out = append(out, r)
	}
	return out
}

func firstMsg(fs map[string][]Finding, rule string) string {
	l := fs[rule]
	if len(l) == 0 {
		return ""
	}
	return l[0].String()
}

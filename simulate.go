package main

import (
	"path"
	"sort"
	"strings"
)

// overlay describes what Magisk will do with one directory of the module's
// system/ tree.
type overlay struct {
	Target     string   // path on the device, e.g. /system/bin
	Mode       string   // "merge" or "replace"
	Files      []string // module files that will appear under the target
	Skipped    bool     // .skip_mount marker present
	VendorBare bool     // top-level vendor/, which magisk never mounts
}

// plan is the full simulation: overlays plus the script schedule.
type plan struct {
	Overlays []overlay
	Scripts  []scriptRun
}

// scriptRun is one script that will run, in execution order.
type scriptRun struct {
	Path  string
	Stage string // e.g. "post-fs-data (blocking)"
	Block bool
}

// resolvePlan walks the module and works out what Magisk would actually do.
func resolvePlan(m *Module) plan {
	return plan{
		Overlays: resolveOverlays(m),
		Scripts:  resolveScripts(m),
	}
}

// resolveOverlays works out what Magisk mounts and where. Targets are real
// device directories, not first-level names: Magisk honours .replace and
// .skip_mount on the directory that *contains* the marker, so a marker at
// system/app/Foo/.replace swaps /system/app/Foo and leaves the rest of
// /system/app merging as normal. Reporting that at /system/app instead would
// claim every stock app disappears.
func resolveOverlays(m *Module) []overlay {
	if m.has("system/.skip_mount") {
		return []overlay{{Target: "/system", Skipped: true, Mode: "(skipped)"}}
	}
	if m.has("system/.replace") {
		return []overlay{{Target: "/system", Mode: "replace"}}
	}

	markers := map[string]string{} // dir under system/ -> "replace" or "skipped"
	dirs := map[string]bool{}      // first-level dirs that hold files
	var files []string             // payload paths, relative to system/
	bare := false                  // a file sitting directly in system/

	for _, name := range m.names() {
		rel := strings.TrimPrefix(name, "system/")
		if rel == name || rel == "" {
			continue
		}
		dir, base := path.Split(rel)
		dir = strings.TrimSuffix(dir, "/")
		switch base {
		case ".replace":
			if markers[dir] != "skipped" {
				markers[dir] = "replace"
			}
			continue
		case ".skip_mount":
			markers[dir] = "skipped"
			continue
		}
		if strings.HasPrefix(base, ".") {
			continue
		}
		files = append(files, rel)
		if i := strings.Index(rel, "/"); i > 0 {
			dirs[rel[:i]] = true
		} else {
			bare = true
		}
	}

	targets := map[string]bool{}
	for d := range dirs {
		targets[d] = true
	}
	for d := range markers {
		targets[d] = true
	}
	if bare {
		targets[""] = true
	}
	if len(targets) == 0 {
		return vendorNote(m, nil)
	}

	byTarget := map[string][]string{}
	for _, f := range files {
		t := nearestTarget(targets, f)
		byTarget[t] = append(byTarget[t], strings.TrimPrefix(strings.TrimPrefix(f, t), "/"))
	}

	var out []overlay
	for t := range targets {
		o := overlay{Target: "/" + t, Mode: "merge", Files: byTarget[t]}
		if t == "" {
			o.Target = "/system"
		}
		switch markers[t] {
		case "skipped":
			o.Skipped = true
			o.Files = nil
		case "replace":
			o.Mode = "replace"
		}
		sort.Strings(o.Files)
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return vendorNote(m, out)
}

// nearestTarget returns the longest target directory that encloses f, so a
// file under a replaced subdirectory is reported against that subdirectory
// rather than against its parent.
func nearestTarget(targets map[string]bool, f string) string {
	best := ""
	for t := range targets {
		if t == "" {
			continue
		}
		if f == t || strings.HasPrefix(f, t+"/") {
			if len(t) > len(best) {
				best = t
			}
		}
	}
	return best
}

// vendorNote prepends the warning about a top-level vendor/ tree, which magisk
// never mounts.
func vendorNote(m *Module, out []overlay) []overlay {
	for _, name := range m.names() {
		if name == "vendor" || strings.HasPrefix(name, "vendor/") {
			return append([]overlay{{
				Target:     "/vendor",
				Mode:       "(not mounted)",
				VendorBare: true,
			}}, out...)
		}
	}
	return out
}

// resolveScripts lists scripts in the order magisk runs them. post-fs-data.sh
// blocks the boot; service.sh runs after late_start and may take its time;
// uninstall.sh and action.sh are event-driven rather than staged.
func resolveScripts(m *Module) []scriptRun {
	var out []scriptRun
	add := func(path, stage string, block bool) {
		if m.has(path) {
			out = append(out, scriptRun{path, stage, block})
		}
	}
	add("customize.sh", "install time (as root, on flash)", false)
	add("post-fs-data.sh", "post-fs-data (blocking, pre-zygote)", true)
	add("service.sh", "late_start service (non-blocking)", false)
	add("uninstall.sh", "module removal", false)
	add("action.sh", "manual action from the magisk app", false)
	return out
}

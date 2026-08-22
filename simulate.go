package main

import (
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

// resolveOverlays maps each first-level directory under system/ to the device
// path magisk would mount it over. Magisk merges file-by-file unless a
// .replace marker sits in the module's copy of that directory, in which case
// the whole target is covered by the module's version and every stock file
// not shipped disappears from view. A top-level vendor/ tree is never mounted
// at all; it is surfaced here so simulate explains that too.
func resolveOverlays(m *Module) []overlay {
	var out []overlay
	seen := map[string]bool{}

	for _, name := range m.names() {
		if !strings.HasPrefix(name, "system/") {
			continue
		}
		rel := strings.TrimPrefix(name, "system/")
		if rel == "" {
			continue
		}
		top := strings.SplitN(rel, "/", 2)[0]
		if seen[top] || strings.HasPrefix(top, ".") {
			continue
		}
		seen[top] = true
		out = append(out, buildOverlay(m, top))
	}
	for _, name := range m.names() {
		if name == "vendor" || strings.HasPrefix(name, "vendor/") {
			out = append([]overlay{{
				Target:     "/vendor",
				Mode:       "(not mounted)",
				VendorBare: true,
			}}, out...)
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func buildOverlay(m *Module, top string) overlay {
	dir := "system/" + top
	o := overlay{Target: "/" + top, Mode: "merge"}

	for _, f := range m.names() {
		if !strings.HasPrefix(f, dir+"/") {
			continue
		}
		base := f[len(dir)+1:]
		if strings.HasSuffix(base, ".skip_mount") {
			o.Skipped = true
			continue
		}
		if base == ".replace" {
			o.Mode = "replace"
			continue
		}
		if !strings.HasPrefix(base, ".") {
			o.Files = append(o.Files, relTree(f, dir))
		}
	}
	if o.Skipped {
		o.Files = nil
	}
	return o
}

// relTree strips prefix/ from a path, keeping subdirectories readable.
func relTree(path, prefix string) string { return strings.TrimPrefix(path, prefix+"/") }

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

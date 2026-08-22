package main

import (
	"fmt"
	"strings"
)

// updaterRule checks the META-INF layout Magisk's install helper relies on:
// updater-script containing #MAGISK, and the update-binary stub that actually
// performs the install.
var updaterRule = Rule{
	ID: "meta",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		const us = "META-INF/com/google/android/updater-script"
		const ub = "META-INF/com/google/android/update-binary"
		if !m.has(us) {
			out = append(out, Finding{
				Rule: "meta", Severity: SevError, File: "-",
				Message: "META-INF/com/google/android/updater-script is missing; the magisk installer skips the module entirely",
			})
		} else {
			text, _ := m.text(us)
			if !strings.Contains(text, "#MAGISK") {
				out = append(out, Finding{
					Rule: "meta", Severity: SevError, File: us, Line: 1,
					Message: `updater-script lacks "#MAGISK"; the magisk stub will not run and nothing installs`,
				})
			}
		}
		if !m.has(ub) {
			out = append(out, Finding{
				Rule: "meta", Severity: SevError, File: ub,
				Message: "update-binary is missing; magisk executes this file to install the module, so a zip without it fails to flash",
			})
		}
		return out
	},
}

// zipLayoutRule flags everything nested under one top-level folder. This is
// the single most common packaging mistake: the zip installs fine and the
// module does nothing at all.
var zipLayoutRule = Rule{
	ID: "layout",
	Run: func(m *Module, ctx *context) []Finding {
		if m.has("module.prop") {
			return nil
		}
		if prefix := nestedPrefix(m); prefix != "" {
			return []Finding{{
				Rule: "layout", Severity: SevError, File: "-",
				Message: fmt.Sprintf("every file sits under %q in the zip; magisk looks for module.prop at the root and installs nothing", prefix),
			}}
		}
		if len(m.Files) > 0 {
			return []Finding{{
				Rule: "layout", Severity: SevError, File: "-",
				Message: "module.prop not found at the root of the module",
			}}
		}
		return nil
	},
}

// crlfRule reports CRLF line endings in any text script file. Android's sh
// treats the trailing \r as part of the command or value and the resulting
// error points nowhere near the cause.
var crlfRule = Rule{
	ID: "crlf",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		for _, name := range m.names() {
			if !isScriptLike(name) {
				continue
			}
			b := m.Files[name]
			for i, line := range strings.Split(string(b), "\n") {
				if strings.HasSuffix(line, "\r") {
					out = append(out, Finding{
						Rule: "crlf", Severity: SevError, File: name, Line: i + 1,
						Message: "CRLF line ending; android sh runs the \\r as part of the command and fails with a misleading error",
					})
					break // one report per file is enough to fix it
				}
			}
		}
		return out
	},
}

// isScriptLike reports whether a file is text that ends up executed or parsed
// on the device. Anything else (images, binaries) is skipped by the line-level
// rules.
func isScriptLike(name string) bool {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "module.prop", "customize.sh", "post-fs-data.sh", "service.sh",
		"uninstall.sh", "boot-completed.sh", "action.sh", "post-fs.sh",
		"sepolicy.rule", "system.prop":
		return true
	}
	if name == "META-INF/com/google/android/updater-script" {
		return true
	}
	return strings.HasSuffix(name, ".sh")
}

// register is called once from run.go with every rule in reporting order.
func register(rules ...Rule) { registry = append(registry, rules...) }

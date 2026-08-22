package main

import (
	"fmt"
	"sort"
)

// symlinkRule reports symlinks the module ships. Magisk preserves them on
// extraction, so a link pointing outside the module writes through to
// wherever it lands on the device — worth knowing, since in a zip listing a
// link looks like an ordinary file.
var symlinkRule = Rule{
	ID: "symlink",
	Run: func(m *Module, ctx *context) []Finding {
		names := make([]string, 0, len(m.Symlinks))
		for name := range m.Symlinks {
			names = append(names, name)
		}
		sort.Strings(names)

		var out []Finding
		for _, name := range names {
			target := m.Symlinks[name]
			sev := SevNote
			msg := fmt.Sprintf("symlink to %s; magisk preserves links on extract, so on the device this points wherever the target lives", target)
			if len(target) > 0 && target[0] == '/' {
				sev = SevWarning
				msg = fmt.Sprintf("symlink to absolute path %s; it resolves against the phone's filesystem and replaces whatever is there", target)
			}
			out = append(out, Finding{
				Rule: "symlink", Severity: sev, File: name,
				Message: msg,
			})
		}
		return out
	},
}

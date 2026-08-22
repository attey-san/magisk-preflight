package main

import (
	"regexp"
	"strings"
)

// shellScripts lists the module's executable shell files. customize.sh is
// sourced by Magisk's installer (busybox ash), so a shebang there is inert
// but bashisms still matter.
func shellScripts(m *Module) []string {
	var out []string
	for _, name := range m.names() {
		if !isScriptLike(name) {
			continue
		}
		base := name
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		switch base {
		case "module.prop", "sepolicy.rule", "system.prop":
			continue
		}
		if strings.HasSuffix(name, ".sh") || base == "customize.sh" {
			out = append(out, name)
		}
	}
	return out
}

var bashismPatterns = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`\[\[`), "mksh has no [[ ]]; the test fails or the line errors under /system/bin/sh"},
	{regexp.MustCompile(`\[\s[^]]*==[^]]*\]`), "posix [ ] has no == operator; use = or the comparison breaks on android"},
	{regexp.MustCompile(`[[:alnum:]_]+\+?=\(`), "arrays are a bash feature; mksh on android cannot run them"},
	{regexp.MustCompile(`\$\{[[:alnum:]_]+\[`), "array indexing does not exist in mksh; the expansion is treated as a pattern"},
	{regexp.MustCompile(`\bfunction\s+[[:alnum:]_]+`), "the function keyword is bash-only; write foo() { ... } instead"},
	{regexp.MustCompile(`\bsource\s`), "mksh has no source builtin; use . (dot) instead"},
	{regexp.MustCompile(`\$'`), "ANSI-C quoting $'...' is bash-only; mksh takes the bytes literally"},
	{regexp.MustCompile(`[<>]\(`), "process substitution is bash-only; mksh reports a syntax error"},
	{regexp.MustCompile(`<<<`), "herestrings are a bash feature; mksh cannot parse them"},
	{regexp.MustCompile(`\$\{[[:alnum:]_]+(,,|\^\^)`), "case-modifying expansions like ${var,,} are bash-only"},
	{regexp.MustCompile(`\$\{[[:alnum:]_]+(/|//)`), "${var/pat/repl} substitution is bash-only and mksh leaves it unexpanded"},
}

var suPattern = regexp.MustCompile(`(^|[;&|]\s*|\bsudo\s+)su\b|\bsu\s+-\b|\bsu\s+-c\b`)

var shebangRule = Rule{
	ID: "shebang",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		for _, name := range shellScripts(m) {
			if strings.HasSuffix(name, "customize.sh") {
				continue // sourced by the installer, shebang never read
			}
			text, _ := m.text(name)
			line := strings.SplitN(text, "\n", 2)[0]
			switch {
			case strings.HasPrefix(line, "#!/bin/bash"), strings.HasPrefix(line, "#!/usr/bin/env bash"),
				strings.HasPrefix(line, "#!/usr/bin/env bash"):
				out = append(out, Finding{
					Rule: "shebang", Severity: SevError, File: name, Line: 1,
					Message: "bash does not exist at this path on android; the script dies with 'no such file or directory' when run directly",
				})
			case strings.HasPrefix(line, "#!"):
				interpreter := strings.TrimPrefix(line, "#!")
				interpreter = strings.TrimSpace(interpreter)
				if interpreter == "/bin/sh" || interpreter == "/system/bin/sh" {
					continue
				}
				out = append(out, Finding{
					Rule: "shebang", Severity: SevError, File: name, Line: 1,
					Message: "interpreter " + interpreter + " is not present on android; use #!/system/bin/sh",
				})
			}
		}
		return out
	},
}

var bashismRule = Rule{
	ID: "bashism",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		for _, name := range shellScripts(m) {
			text, _ := m.text(name)
			for i, line := range strings.Split(text, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" || strings.HasPrefix(trimmed, "#") {
					continue
				}
				for _, p := range bashismPatterns {
					if p.re.MatchString(line) {
						out = append(out, Finding{
							Rule: "bashism", Severity: SevError, File: name, Line: i + 1,
							Message: p.msg,
						})
						break // one bashism per line keeps the report readable
					}
				}
			}
		}
		return out
	},
}

var suRule = Rule{
	ID: "su",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		for _, name := range shellScripts(m) {
			text, _ := m.text(name)
			for i, line := range strings.Split(text, "\n") {
				if suPattern.MatchString(line) {
					out = append(out, Finding{
						Rule: "su", Severity: SevError, File: name, Line: i + 1,
						Message: "module scripts already run as root; calling su re-prompts through the magisk shell and can deadlock the script",
					})
				}
			}
		}
		return out
	},
}

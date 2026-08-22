package main

import (
	"regexp"
	"strconv"
	"strings"
)

var apiLine = regexp.MustCompile(`(?i)\b(?:api|sdk)[ _-]?(?:level)?[ _-]*=?[ _-]*(2[0-5]|1[0-9])\b`)

// tlsRule flags https fetches when the module targets Android 7.0 (API 24) or
// older, where the system trust store has no ISRG Root X1 and every Let's
// Encrypt certificate fails with a bare certificate error.
var tlsRule = Rule{
	ID: "tls",
	Run: func(m *Module, ctx *context) []Finding {
		if !ctx.apiKnown() || ctx.minAPI > 24 {
			return nil
		}
		var out []Finding
		for _, name := range shellScripts(m) {
			text, _ := m.text(name)
			for i, line := range strings.Split(text, "\n") {
				if netCmdPattern.MatchString(line) && strings.Contains(line, "https://") {
					out = append(out, Finding{
						Rule: "tls", Severity: SevError, File: name, Line: i + 1,
						Message: "on android 7.0 and earlier the trust store lacks ISRG Root X1; this https fetch to a Let's Encrypt host fails with a certificate error",
					})
				}
			}
		}
		return out
	},
}

// iptablesWaitRule catches `iptables -w N`. The -w flag takes no argument on
// older kernels; the whole command fails with "option requires an argument".
var iptablesWaitRule = Rule{
	ID: "iptablesw",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		pat := regexp.MustCompile(`\bi(p6|p)?tables?\s+(-w|--wait)\s+\d`)
		for _, name := range shellScripts(m) {
			text, _ := m.text(name)
			for i, line := range strings.Split(text, "\n") {
				if pat.MatchString(line) {
					out = append(out, Finding{
						Rule: "iptablesw", Severity: SevError, File: name, Line: i + 1,
						Message: "-w takes no argument on older kernels; use a plain -w or retry in a loop instead of -w 5",
					})
				}
			}
		}
		return out
	},
}

// safeModeRule checks that risky modules document the safe-mode escape.
// Magisk disables all modules after a safe-mode boot; if the README never
// says so, users discover it only after their phone misbehaves.
var safeModeRule = Rule{
	ID: "safemode",
	Run: func(m *Module, ctx *context) []Finding {
		risky := m.has("post-fs-data.sh") && len(postFsDataRule.Run(m, ctx)) > 0 ||
			len(partitionWriteRule.Run(m, ctx)) > 0 ||
			len(iptablesWaitRule.Run(m, ctx)) > 0

		readme, hasReadme := readmeText(m)
		if !hasReadme {
			if risky {
				return []Finding{{
					Rule: "safemode", Severity: SevWarning, File: "-",
					Message: "module does risky work but has no README documenting that booting android safe mode disables it",
				}}
			}
			return nil
		}
		if risky && !mentionsSafeMode(readme) {
			return []Finding{{
				Rule: "safemode", Severity: SevWarning, File: readmeName(m),
				Message: "no mention of magisk's safe mode; document that booting into android safe mode disables this module so users can recover",
			}}
		}
		return nil
	},
}

func readmeName(m *Module) string {
	for _, name := range m.names() {
		base := strings.ToLower(name)
		if base == "readme.md" || base == "readme.txt" || base == "readme" {
			return name
		}
	}
	return ""
}

func readmeText(m *Module) (string, bool) {
	n := readmeName(m)
	if n == "" {
		return "", false
	}
	t, ok := m.text(n)
	return t, ok
}

func mentionsSafeMode(text string) bool {
	return strings.Contains(strings.ToLower(text), "safe mode") ||
		strings.Contains(strings.ToLower(text), "safe-mode")
}

// parseAPIMention extracts an API level from customize.sh text like
// "# Only tested up to API 25" — modules often state support there.
func parseAPIMention(text string) int {
	m := apiLine.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

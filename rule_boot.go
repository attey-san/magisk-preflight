package main

import (
	"regexp"
	"strings"
)

// stripQuoted removes double-quoted and single-quoted spans from a shell
// line, so words inside strings (echo "do not sleep here") do not trip
// command-oriented rules. Backslashes inside double quotes are left as-is;
// this is deliberately conservative and only needs to be good enough to
// avoid false positives.
func stripQuoted(line string) string {
	var b strings.Builder
	var quote rune
	for _, r := range line {
		switch {
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	sleepPattern  = regexp.MustCompile(`\bsleep\b`)
	waitPattern   = regexp.MustCompile(`(^|[\s;&])wait\b`)
	whilePattern  = regexp.MustCompile(`(^|[\s;&])while\b`)
	netCmdPattern = regexp.MustCompile(`\b(curl|wget|ping\d?|nc|netcat)\b`)
	appLaunchHint = regexp.MustCompile(`\b(am\s+start|monkey\s+-p)\b`)
)

// commandText returns the line with quoted spans removed and comments
// stripped, for rules that look at commands rather than prose.
func commandText(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if idx := indexUnquotedHash(trimmed); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return stripQuoted(trimmed), true
}

// indexUnquotedHash finds a # that starts a comment outside quotes.
func indexUnquotedHash(line string) int {
	var quote rune
	for i, r := range line {
		switch {
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && r == '#':
			return i
		}
	}
	return -1
}

// postFsDataRule flags anything slow, blocking or networked in
// post-fs-data.sh. That stage runs synchronously before zygote; every second
// there is a second of boot time, and enough of it is a bootloop.
var postFsDataRule = Rule{
	ID: "postfsdata",
	Run: func(m *Module, ctx *context) []Finding {
		text, ok := m.text("post-fs-data.sh")
		if !ok {
			return nil
		}
		var out []Finding
		for i, line := range strings.Split(text, "\n") {
			cmd, ok := commandText(line)
			if !ok {
				continue
			}
			report := func(msg string) {
				out = append(out, Finding{
					Rule: "postfsdata", Severity: SevError, File: "post-fs-data.sh", Line: i + 1,
					Message: msg,
				})
			}
			if sleepPattern.MatchString(cmd) {
				report("sleep in post-fs-data blocks the entire boot; move it to service.sh which runs non-blocking after late_start")
			}
			if waitPattern.MatchString(cmd) {
				report("wait blocks post-fs-data until children exit, stalling zygote; run background work from service.sh instead")
			}
			if whilePattern.MatchString(cmd) {
				report("an unbounded while loop here delays zygote for as long as it runs; if it must loop, background it in service.sh")
			}
			if netCmdPattern.MatchString(cmd) {
				report("there is no network in post-fs-data (and often no netd yet); this call fails every boot — move it to service.sh")
			}
		}
		return out
	},
}

// serviceShRule catches the opposite mistake: work that needs the system fully
// up (launching apps, reading user settings) sitting in a stage where those
// services do not exist yet.
var serviceShRule = Rule{
	ID: "servicestage",
	Run: func(m *Module, ctx *context) []Finding {
		text, ok := m.text("service.sh")
		if !ok {
			return nil
		}
		var out []Finding
		for i, line := range strings.Split(text, "\n") {
			cmd, ok := commandText(line)
			if !ok {
				continue
			}
			if appLaunchHint.MatchString(cmd) && !strings.Contains(cmd, "until") &&
				!strings.Contains(cmd, "boot_completed") {
				out = append(out, Finding{
					Rule: "servicestage", Severity: SevNote, File: "service.sh", Line: i + 1,
					Message: "apps are not launchable until well after service.sh starts on many devices; wrap this in a short wait-until-booted loop",
				})
			}
		}
		return out
	},
}

// partitionWriteRule is the highest-severity check: writes that escape the
// systemless overlay and touch real partitions. They survive module removal
// and brick devices with immutable or dm-verity-protected partitions.
var partitionWriteRule = Rule{
	ID: "partition",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		writeCmd := regexp.MustCompile(`^\s*(sudo\s+)?(rm|cp|mv|dd|cat|tee|mkdir|touch|ln|chmod|chown|truncate|setfattr|patch|sed)\b`)
		// Remounts appear with the flags in either order; writes may name a
		// partition path anywhere on the line, including inside quotes from
		// `su -c "rm ... /system/x"`, which gets flagged separately by suRule.
		remount := regexp.MustCompile(`\bremount\b[^\n]*\brw\b|\bmount\s+[^;\n]*-o[^;\n]*rw[^;\n]*remount`)
		anySystemPath := regexp.MustCompile(`(^|[^[:alnum:]_])/(system|vendor|product|system_ext)(/|[[:alnum:]_]|$)`)

		for _, name := range shellScripts(m) {
			text, _ := m.text(name)
			for i, line := range strings.Split(text, "\n") {
				cmd, ok := commandText(line)
				if !ok {
					continue
				}
				lower := strings.ToLower(cmd)

				if anySystemPath.MatchString(lower) {
					if remount.MatchString(lower) {
						out = append(out, Finding{
							Rule: "partition", Severity: SevError, File: name, Line: i + 1,
							Message: "remounting a partition read-write defeats the systemless design, survives module removal and bricks devices with immutable partitions",
						})
						continue
					}
					// Writes may name a partition path anywhere on the line,
					// including inside quotes from `su -c "rm ... /system/x"`,
					// so the raw (unstripped) line is checked for the path but
					// the command verb comes from the stripped text.
					raw := strings.ToLower(strings.TrimSpace(stripQuoted(line)))
					if writeCmd.MatchString(cmd) || writeCmd.MatchString(raw) ||
						strings.HasPrefix(cmd, "su ") || strings.HasPrefix(cmd, "su\t") {
						out = append(out, Finding{
							Rule: "partition", Severity: SevError, File: name, Line: i + 1,
							Message: "writes outside the module's own system/ overlay persist after uninstall and can hard-brick devices under dm-verity; put the file in the overlay instead",
						})
					}
				}
			}
		}
		return out
	},
}

// vendorDirRule reports a bare top-level vendor/ directory. Magisk only mounts
// <module>/system over /system; vendor overlays belong in system/vendor.
var vendorDirRule = Rule{
	ID: "vendordir",
	Run: func(m *Module, ctx *context) []Finding {
		var out []Finding
		for _, name := range m.names() {
			if name == "vendor" || strings.HasPrefix(name, "vendor/") {
				out = append(out, Finding{
					Rule: "vendordir", Severity: SevError, File: "-",
					Message: `top-level "vendor/" is never mounted by magisk; move it to "system/vendor/" so the files actually appear on /vendor`,
				})
				break
			}
		}
		return out
	},
}

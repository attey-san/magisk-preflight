package main

import "fmt"

// Severity of a finding, ordered by how badly the module will misbehave.
type Severity int

const (
	// SevNote is information the user should know but no action is required.
	SevNote Severity = iota
	// SevWarning flags something likely to misbehave on some devices.
	SevWarning
	// SevError flags something that will break the module or the boot.
	SevError
)

func (s Severity) String() string {
	switch s {
	case SevNote:
		return "note"
	case SevWarning:
		return "warning"
	default:
		return "error"
	}
}

// Finding is one rule violation at a specific place in the module.
type Finding struct {
	Rule     string // short rule id, e.g. "crlf"
	Severity Severity
	File     string // path relative to the module root; "-" for whole-module
	Line     int    // 1-based; 0 when not tied to a line
	Message  string // the consequence for the user, not a restatement of the rule
}

func (f Finding) String() string {
	loc := f.File
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	if loc == "" || loc == "-" {
		return fmt.Sprintf("%-8s %s", f.Severity, f.Message)
	}
	return fmt.Sprintf("%-8s %s: %s", f.Severity, loc, f.Message)
}

// worst returns the highest severity in a set of findings.
func worst(fs []Finding) Severity {
	w := SevNote
	for _, f := range fs {
		if f.Severity > w {
			w = f.Severity
		}
	}
	return w
}

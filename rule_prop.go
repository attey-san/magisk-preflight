package main

import (
	"fmt"
	"strconv"
	"strings"
)

// requiredPropKeys are the keys Magisk's own parser needs; a module missing
// any of them is rejected at install time.
var requiredPropKeys = []string{
	"id", "name", "version", "versionCode", "author", "description",
}

// parseModuleProp parses module.prop the way Magisk does: one key=value per
// line, everything after the first '=' belonging to the value.
func parseModuleProp(text string) (map[string]string, bool) {
	props := make(map[string]string)
	ok := true
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			ok = false
			continue
		}
		props[strings.TrimSpace(k)] = v
	}
	return props, ok
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	c := id[0]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

var propRule = Rule{
	ID: "prop",
	Run: func(m *Module, ctx *context) []Finding {
		if !m.has("module.prop") {
			return []Finding{{
				Rule: "prop", Severity: SevError, File: "-",
				Message: "module.prop is missing; magisk cannot identify or install this module",
			}}
		}
		var out []Finding
		if !ctx.propOK {
			out = append(out, Finding{
				Rule: "prop", Severity: SevError, File: "module.prop",
				Message: "a line without key=value aborts magisk's parser and the install fails",
			})
		}
		for _, k := range requiredPropKeys {
			if _, ok := ctx.prop[k]; !ok {
				out = append(out, Finding{
					Rule: "prop", Severity: SevError, File: "module.prop",
					Message: fmt.Sprintf("missing required key %q; magisk rejects the module at install", k),
				})
			}
		}
		if id, ok := ctx.prop["id"]; ok && !validID(id) {
			out = append(out, Finding{
				Rule: "prop", Severity: SevError, File: "module.prop",
				Message: fmt.Sprintf("id %q must match ^[a-zA-Z][a-zA-Z0-9._-]+$ or magisk ignores the module", id),
			})
		}
		if vc, ok := ctx.prop["versionCode"]; ok {
			if _, err := strconv.Atoi(strings.TrimSpace(vc)); err != nil {
				out = append(out, Finding{
					Rule: "prop", Severity: SevError, File: "module.prop",
					Message: fmt.Sprintf("versionCode %q is not an integer; magisk compares versions numerically and will fail", vc),
				})
			}
		}
		return out
	},
}

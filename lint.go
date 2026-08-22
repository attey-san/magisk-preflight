package main

import (
	"fmt"
	"sort"
)

// lintContext builds the shared per-run state from a loaded module.
func lintContext(m *Module) *context {
	ctx := &context{}
	if t, ok := m.text("module.prop"); ok {
		ctx.prop, ctx.propOK = parseModuleProp(t)
		ctx.minAPI = propInt(ctx.prop, "minApi")
	}
	if t, ok := m.text("customize.sh"); ok {
		ctx.customSh = t
		if ctx.minAPI == 0 {
			ctx.minAPI = parseAPIMention(t)
		}
	}
	return ctx
}

func propInt(props map[string]string, key string) int {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok {
		return 0
	}
	var out int
	if _, err := fmt.Sscanf(v, "%d", &out); err != nil {
		return 0
	}
	return out
}

// runRules applies every registered rule and returns findings sorted by file,
// then line, then rule id — so output is stable regardless of rule order.
func runRules(m *Module) []Finding {
	ctx := lintContext(m)
	var out []Finding
	for _, r := range registry {
		out = append(out, r.Run(m, ctx)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			if a.File == "-" {
				return false
			}
			if b.File == "-" {
				return true
			}
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	return out
}

// registerAll wires the registry. Adding a rule means adding it here.
func registerAll() {
	register(
		updaterRule,
		zipLayoutRule,
		crlfRule,
		propRule,
		shebangRule,
		bashismRule,
		suRule,
		postFsDataRule,
		serviceShRule,
		partitionWriteRule,
		vendorDirRule,
		iptablesWaitRule,
		tlsRule,
		safeModeRule,
		symlinkRule,
	)
}

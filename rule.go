package main

// Rule is one check. The registry exists so each failure mode is a single
// self-contained unit with its own test; adding rule N+1 means appending a
// value here and writing its function.
type Rule struct {
	ID  string
	Run func(m *Module, ctx *context) []Finding
}

var registry []Rule

// context carries per-run state that rules need to share without talking to
// each other: parsed module.prop and the detected minimum API level.
type context struct {
	prop   map[string]string // parsed module.prop, nil if absent/unparsable
	propOK bool              // module.prop present and every line parseable
	minAPI int               // from minApi=, or 0 when unknown
}

func (c *context) apiKnown() bool { return c.minAPI > 0 }

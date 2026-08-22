package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdLint analyses a module and prints findings. Exit 1 when anything
// error-severity turned up, 0 otherwise; 2 means the input could not be read.
func cmdLint(args []string) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON")
	paths := parseTrailing(fs, args, 1)
	if paths == nil {
		return 2
	}
	m, code := loadOrDie(paths[0])
	if m == nil {
		return code
	}
	registerAll()
	findings := runRules(m)

	if *jsonOut {
		b, err := toJSON(findings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "preflight: %v\n", err)
			return 2
		}
		os.Stdout.Write(b)
	} else {
		for _, f := range findings {
			fmt.Println(f.String())
		}
		if len(findings) == 0 {
			fmt.Printf("%s: no problems found\n", m.Root)
		}
	}
	if worst(findings) == SevError {
		return 1
	}
	return 0
}

// parseTrailing parses flags that may appear before, between or after
// positional arguments: flag's default loop stops at the first non-flag word,
// so "lint mod --json" would otherwise treat --json as a second path.
//
// Everything after a bare "--" is positional by definition, so the arguments
// are split there first; the remainder is parsed repeatedly, holding out one
// positional per pass until no flags are left. Exactly want positional
// arguments are required, else a usage error is printed and nil returned.
func parseTrailing(fs *flag.FlagSet, args []string, want int) []string {
	head, tail := args, []string(nil)
	for i, a := range args {
		if a == "--" {
			head, tail = args[:i], args[i+1:]
			break
		}
	}

	var positional []string
	rest := head
	for {
		if err := fs.Parse(rest); err != nil {
			return nil
		}
		next := fs.Args()
		switch {
		case len(next) == 0:
			positional = append(positional, tail...)
			if len(positional) != want {
				fmt.Fprintf(os.Stderr, "preflight %s: exactly %d path argument%s required\n",
					fs.Name(), want, plural(want))
				return nil
			}
			return positional
		case len(next) == len(rest):
			// Parsing stopped on this token; hold it out and look past it.
			positional = append(positional, next[0])
			rest = next[1:]
		default:
			rest = next
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cmdSimulate prints the resolved plan: what gets mounted where, and which
// scripts run in which stage.
func cmdSimulate(args []string) int {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	paths := parseTrailing(fs, args, 1)
	if paths == nil {
		return 2
	}
	m, code := loadOrDie(paths[0])
	if m == nil {
		return code
	}
	p := resolvePlan(m)

	if len(p.Overlays) == 0 && len(p.Scripts) == 0 {
		fmt.Printf("%s: nothing to simulate — no system/ tree and no scripts\n", m.Root)
		return 0
	}
	if len(p.Overlays) > 0 {
		fmt.Println("system overlay:")
		for _, o := range p.Overlays {
			if o.VendorBare {
				fmt.Printf("  %-14s %s\n", o.Target, "IGNORED — top-level vendor/ is never mounted; move under system/vendor/")
				continue
			}
			if o.Skipped {
				fmt.Printf("  %-14s skipped (.skip_mount present)\n", o.Target)
				continue
			}
			fmt.Printf("  %-14s %s\n", o.Target, o.Mode)
			for _, f := range o.Files {
				fmt.Printf("      %s\n", f)
			}
			if o.Mode == "replace" {
				fmt.Printf("      (every stock file in %s not listed above is hidden)\n", o.Target)
			}
		}
	}
	if len(p.Scripts) > 0 {
		fmt.Println("scripts, in run order:")
		for _, s := range p.Scripts {
			marker := ""
			if s.Block {
				marker = "  [blocks boot]"
			}
			fmt.Printf("  %-16s %s%s\n", s.Path, s.Stage, marker)
		}
	}
	return 0
}

// cmdNew scaffolds a module skeleton with correct structure from the start.
func cmdNew(args []string) int {
	if len(args) != 1 || args[0] == "" || args[0] == "." || args[0] == ".." ||
		filepath.Base(args[0]) != args[0] {
		fmt.Fprintln(os.Stderr, "preflight new: a single directory name is required")
		return 2
	}
	name := args[0]
	dir := filepath.Join(".", name)
	if _, err := os.Stat(dir); err == nil {
		fmt.Fprintf(os.Stderr, "preflight new: %s already exists\n", dir)
		return 2
	}
	files := scaffold(name)
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "preflight: %v\n", err)
			return 2
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "preflight: %v\n", err)
			return 2
		}
	}
	fmt.Printf("created %s — edit module.prop, then zip the contents (not the folder) to flash\n", dir)
	return 0
}

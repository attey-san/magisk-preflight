package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// run dispatches a subcommand. It returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "new":
		return cmdNew(rest)
	case "lint":
		return cmdLint(rest)
	case "simulate":
		return cmdSimulate(rest)
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "preflight: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: preflight <command> [flags] <path>

  preflight new <name>     scaffold a module skeleton in ./<name>
  preflight lint <path>    analyse a module directory or zip
  preflight simulate <path>  print what the module would do on flash and boot

lint flags:
  --json   emit findings as JSON instead of text

Exit status is 1 when lint finds any error-severity problem, so it can gate CI.
`)
}

// jsonFinding is the wire format for --json. Severity is a lowercase string,
// not an int, because consumers are shell scripts and jq one-liners.
type jsonFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

func toJSON(fs []Finding) ([]byte, error) {
	out := make([]jsonFinding, len(fs))
	for i, f := range fs {
		out[i] = jsonFinding{
			Rule:     f.Rule,
			Severity: f.Severity.String(),
			File:     f.File,
			Line:     f.Line,
			Message:  strings.TrimSpace(f.Message),
		}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func loadOrDie(path string) (*Module, int) {
	m, err := load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preflight: %v\n", err)
		return nil, 2
	}
	return m, 0
}

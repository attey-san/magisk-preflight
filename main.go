// Package preflight analyses Magisk modules statically and reports what will
// break before anything reaches a phone.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "preflight:", err)
		os.Exit(2)
	}
}

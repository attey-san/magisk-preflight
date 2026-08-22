package main

import "os"

func main() {
	if code := run(os.Args[1:]); code != 0 {
		os.Exit(code)
	}
}

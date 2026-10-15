package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	code, err := run(args, stdin, stdout)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "md: %v\n", err)
	}
	return code
}

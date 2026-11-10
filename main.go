package main

import (
	"fmt"
	"os"

	"github.com/Pippit-dev/pippit-cli/internal/trae"

	"github.com/Pippit-dev/pippit-cli/cmd"
)

func main() {
	// Installers reuse the native executable lookup before a login or network call.
	if len(os.Args) == 2 && os.Args[1] == "__trae_caller" {
		if trae.Ancestor() {
			_, _ = os.Stdout.WriteString("trae\n")
		}
		return
	}
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

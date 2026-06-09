package main

import (
	"fmt"
	"os"

	"github.com/alex99y/matching-engine/cli/internal/command"
)

func main() {
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

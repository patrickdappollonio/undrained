package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/patrickdappollonio/undrained/cmd"
)

var version = "development"

func main() {
	if err := cmd.NewRootCommand(version).Execute(); err != nil {
		if errors.Is(err, cmd.ErrIssuesFound) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(2)
	}
}

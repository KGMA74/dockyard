package main

import (
	"fmt"
	"os"

	"dockyard/internal/cli"
	"dockyard/internal/version"
)

func main() {
	if err := cli.Root(version.Version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

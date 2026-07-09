package main

import (
	"fmt"
	"os"

	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "turbo-cache:", err)
		os.Exit(1)
	}
}

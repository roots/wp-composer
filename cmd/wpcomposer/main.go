package main

import (
	"fmt"
	"os"

	wpcomposergo "github.com/roots/wp-composer"
	"github.com/roots/wp-composer/cmd/wpcomposer/cmd"
)

func main() {
	cmd.Migrations = wpcomposergo.Migrations
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

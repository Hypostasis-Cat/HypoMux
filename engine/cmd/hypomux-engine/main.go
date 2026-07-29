package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "serve":
		engineServer := server.New(stdin, stdout, server.Metadata{
			Name:    "hypomux-engine",
			Version: version,
			Commit:  commit,
		})
		if err := engineServer.Run(context.Background()); err != nil {
			fmt.Fprintf(stderr, "hypomux-engine: %v\n", err)
			return 1
		}
		return 0
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "hypomux-engine %s (%s)\n", version, commit)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", command)
		fmt.Fprintln(stderr, "usage: hypomux-engine [serve|version]")
		return 2
	}
}

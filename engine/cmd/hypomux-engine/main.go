package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
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
	case "diagnose", "diagnostic":
		flags := flag.NewFlagSet("diagnose", flag.ContinueOnError)
		flags.SetOutput(stderr)
		sourceIP := flags.String("src-ip", "", "source IPv4 address")
		targetIP := flags.String("target-ip", diagnostic.DefaultTargetIP, "target IPv4 address")
		count := flags.Int("count", diagnostic.DefaultCount, "number of probes")
		timeoutMS := flags.Int("timeout-ms", int(diagnostic.DefaultTimeout/time.Millisecond), "timeout per probe")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		result := diagnostic.Run(context.Background(), diagnostic.Config{
			SourceIP: *sourceIP,
			TargetIP: *targetIP,
			Count:    *count,
			Timeout:  time.Duration(*timeoutMS) * time.Millisecond,
		})
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "encode diagnostic result: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", command)
		fmt.Fprintln(stderr, "usage: hypomux-engine [serve|version|diagnose]")
		return 2
	}
}

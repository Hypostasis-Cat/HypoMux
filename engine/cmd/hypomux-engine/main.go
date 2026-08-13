package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
)

var (
	version = "dev"
	commit  = "unknown"

	recoverTUN = tun.Recover

	installServiceCommand = installWindowsService
	removeServiceCommand  = removeWindowsService
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
		return runServer(stdin, stdout, stderr)
	case "service":
		return runWindowsService(stderr, baseServerMetadata())
	case "install-service":
		if err := installServiceCommand(); err != nil {
			fmt.Fprintf(stderr, "install HypoMux Core Service: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "HypoMux Core Service installed and started")
		return 0
	case "remove-service":
		if err := removeServiceCommand(); err != nil {
			fmt.Fprintf(stderr, "remove HypoMux Core Service: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "HypoMux Core Service removed")
		return 0
	case "serve-pipe":
		flags := flag.NewFlagSet("serve-pipe", flag.ContinueOnError)
		flags.SetOutput(stderr)
		pipeName := flags.String("pipe", "", "authenticated host pipe name")
		sessionToken := flags.String("session-token", "", "one-time host session token")
		hostPID := flags.Int("host-pid", 0, "expected desktop host process ID")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *pipeName == "" || *sessionToken == "" || *hostPID <= 0 {
			fmt.Fprintln(stderr, "serve-pipe requires --pipe, --session-token and --host-pid")
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		connection, err := connectAuthenticatedPipe(ctx, *pipeName, *sessionToken, *hostPID)
		cancel()
		if err != nil {
			fmt.Fprintf(stderr, "connect authenticated host pipe: %v\n", err)
			return 1
		}
		defer connection.Close()
		return runServer(connection, connection, stderr)
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "hypomux-engine %s (%s)\n", version, commit)
		return 0
	case "recover":
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := recoverTUN(ctx); err != nil {
			fmt.Fprintf(stderr, "recover HypoMux network state: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "HypoMux network state recovered")
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
		fmt.Fprintln(stderr, "usage: hypomux-engine [serve|serve-pipe|service|install-service|remove-service|version|recover|diagnose]")
		return 2
	}
}

func runServer(input io.Reader, output, stderr io.Writer) int {
	engineServer := server.New(input, output, runtimeServerMetadata())
	if err := engineServer.Run(context.Background()); err != nil {
		fmt.Fprintf(stderr, "hypomux-engine: %v\n", err)
		return 1
	}
	return 0
}

func baseServerMetadata() server.Metadata {
	return server.Metadata{
		Name:    "hypomux-engine",
		Version: version,
		Commit:  commit,
	}
}

func runtimeServerMetadata() server.Metadata {
	metadata := baseServerMetadata()
	executable, err := os.Executable()
	if err != nil {
		return metadata
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return metadata
	}
	metadata.TunExecutable = filepath.Join(filepath.Dir(executable), "sing-box.exe")
	return metadata
}

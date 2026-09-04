//go:build windows

package services

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func probeTUNDataPathPlatform(parent context.Context, endpoints []string) tunConnectivityCheck {
	check := tunConnectivityCheck{
		Stage:    "tun_data_path",
		Outbound: "windows-tun",
	}
	curlPath, err := resolveWindowsSystemExecutable("curl.exe")
	if err != nil {
		check.Error = err.Error()
		return check
	}
	failures := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 7*time.Second)
		command := exec.CommandContext(
			ctx,
			curlPath,
			"--silent", "--show-error", "--ipv4",
			"--noproxy", "*", "--proxy", "",
			"--connect-timeout", "4", "--max-time", "6",
			"--output", "NUL", "--write-out", "%{http_code}",
			endpoint,
		)
		configureBackgroundCommand(command)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		runErr := command.Run()
		contextErr := ctx.Err()
		cancel()
		if runErr != nil {
			detail := runErr.Error()
			if contextErr != nil {
				detail = contextErr.Error()
			} else if value := strings.TrimSpace(stderr.String()); value != "" {
				detail = value
			}
			failures = append(failures, endpoint+": "+detail)
			continue
		}
		statusCode, parseErr := strconv.Atoi(strings.TrimSpace(stdout.String()))
		if parseErr != nil || statusCode < 200 || statusCode >= 400 {
			failures = append(failures, endpoint+": HTTP "+strings.TrimSpace(stdout.String()))
			continue
		}
		check.Endpoint = endpoint
		check.OK = true
		check.Detail = fmt.Sprintf("via independent curl.exe process -> HTTP %d", statusCode)
		return check
	}
	check.Endpoint = strings.Join(endpoints, ", ")
	if len(failures) == 0 {
		check.Error = "no TUN data-path endpoints configured"
	} else {
		check.Error = strings.Join(failures, "; ")
	}
	return check
}

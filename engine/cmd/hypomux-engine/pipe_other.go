//go:build !windows

package main

import (
	"context"
	"errors"
	"io"
)

func connectAuthenticatedPipe(context.Context, string, string, int) (io.ReadWriteCloser, error) {
	return nil, errors.New("authenticated named-pipe transport is only available on Windows")
}

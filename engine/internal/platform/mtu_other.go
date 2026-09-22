//go:build !windows

package platform

import (
	"context"
	"errors"
)

func SetMTU(context.Context, MTUChange) error { return errors.New("MTU changes require Windows") }

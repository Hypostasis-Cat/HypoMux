//go:build !windows

package setup

import (
	"fmt"
	"io"
)

func Run(_ []string, out io.Writer) int {
	fmt.Fprintln(out, "Windows setup is unavailable on this platform")
	return 2
}

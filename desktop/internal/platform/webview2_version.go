package platform

import (
	"strconv"
	"strings"
)

func usableWebViewVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 4 {
		return false
	}
	nonzero := false
	for _, p := range parts {
		n, e := strconv.ParseUint(p, 10, 32)
		if e != nil {
			return false
		}
		nonzero = nonzero || n > 0
	}
	return nonzero
}

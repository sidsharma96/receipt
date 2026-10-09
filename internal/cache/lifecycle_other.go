//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package cache

import (
	"context"
	"fmt"
)

func lockBlobDirectory(context.Context, string, bool) (func(), error) {
	return nil, fmt.Errorf("shared storage lifecycle requires supported Unix filesystem locks")
}

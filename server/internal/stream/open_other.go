//go:build !windows

package stream

import "os"

// OpenShared is os.Open; POSIX renames never conflict with open readers.
func OpenShared(path string) (*os.File, error) { return os.Open(path) }

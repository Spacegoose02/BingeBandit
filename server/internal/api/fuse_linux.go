package api

import "syscall"

// fuseSuperMagic is the filesystem type statfs reports for FUSE mounts,
// such as Unraid's /mnt/user shares (shfs).
const fuseSuperMagic = 0x65735546

// OnFUSE reports whether path is on a FUSE filesystem.
func OnFUSE(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return uint32(st.Type) == fuseSuperMagic
}

package api

import "golang.org/x/sys/windows"

// diskSpace returns the size of the drive holding path and how much of it
// this process can still write.
func diskSpace(path string) (total, free uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, size, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &size, &totalFree); err != nil {
		return 0, 0, err
	}
	return size, avail, nil
}

package runtime

import "golang.org/x/sys/windows"

func storageCapacity(runtimePath string) (totalBytes, availableBytes uint64, isKnown bool) {
	directory, err := windows.UTF16PtrFromString(runtimePath)
	if err != nil {
		// Invalid paths cannot produce an observation safe to publish.
		return 0, 0, false
	}
	err = windows.GetDiskFreeSpaceEx(directory, &availableBytes, &totalBytes, nil)
	if err != nil {
		// Capacity is unknown; do not expose filesystem paths or platform errors.
		return 0, 0, false
	}
	return totalBytes, availableBytes, true
}

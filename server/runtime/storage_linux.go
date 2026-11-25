package runtime

import "golang.org/x/sys/unix"

func storageCapacity(runtimePath string) (totalBytes, availableBytes uint64, isKnown bool) {
	var capacity unix.Statfs_t
	err := unix.Statfs(runtimePath, &capacity)
	if err != nil {
		// Capacity is unknown; do not expose filesystem paths or platform errors.
		return 0, 0, false
	}
	blockSize := int64(capacity.Frsize)
	if blockSize == 0 {
		blockSize = int64(capacity.Bsize)
	}
	if blockSize <= 0 || capacity.Bavail > capacity.Blocks {
		return 0, 0, false
	}
	blockBytes := uint64(blockSize)
	if capacity.Blocks > ^uint64(0)/blockBytes {
		return 0, 0, false
	}
	return capacity.Blocks * blockBytes, capacity.Bavail * blockBytes, true
}

//go:build !linux && !windows

package runtime

func storageCapacity(runtimePath string) (totalBytes, availableBytes uint64, isKnown bool) {
	// This platform has no runtime filesystem capacity observer.
	return 0, 0, false
}

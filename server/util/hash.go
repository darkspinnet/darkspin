// Package util contains shared compatibility helpers from recap_server.
package util

// HashID is recap_server's case-insensitive 32-bit FNV-1 identifier hash.
func HashID(value string) uint32 {
	hash := uint32(0x811c9dc5)
	for index := 0; index < len(value); index++ {
		hash *= 0x01000193
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		hash ^= uint32(character)
	}
	return hash
}

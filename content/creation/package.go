package creation

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

type creationEntry struct {
	dbpf.Entry
	offset uint64
}

type creationPackage struct {
	r       io.ReaderAt
	size    uint64
	entries []creationEntry
}

// DBBF stores 64-bit payload offsets and different header locations. Keep its
// read-only adapter separate from the game's DBPF writer.
// Format reference: SporeModder-FX DatabasePackedFile and DBPFItem readers.
func openCreationPackage(r io.ReaderAt, size int64) (*creationPackage, error) {
	var header [96]byte
	if size < int64(len(header)) {
		return nil, errors.New("incomplete creation package header")
	}
	count, err := r.ReadAt(header[:], 0)
	if err != nil {
		return nil, fmt.Errorf("headerRead: %w", err)
	}
	if count != len(header) {
		return nil, errors.New("incomplete creation package header")
	}
	resourceCount := binary.LittleEndian.Uint32(header[36:])
	if resourceCount > 1000000 {
		return nil, errors.New("creation package resource count exceeds limit")
	}
	pkg := &creationPackage{r: r, size: uint64(size)}
	if string(header[:4]) == "DBPF" {
		reader, readErr := dbpf.NewReader(r, size)
		if readErr != nil {
			return nil, fmt.Errorf("dbpfIndex: %w", readErr)
		}
		for _, entry := range reader.Entries {
			pkg.entries = append(pkg.entries, creationEntry{Entry: entry, offset: uint64(entry.Offset)})
		}
		return pkg, nil
	}
	if string(header[:4]) != "DBBF" || binary.LittleEndian.Uint32(header[4:]) != 2 || binary.LittleEndian.Uint32(header[52:]) != 3 {
		return nil, errors.New("unsupported creation package header")
	}
	indexOffset := binary.LittleEndian.Uint64(header[56:])
	// The DBBF reader layout uses a 32-bit index size at 0x28. Do not
	// interpret the following header word as its high bits; cache writers
	// need not clear that word. Payload and index offsets remain 64-bit.
	indexSize := uint64(binary.LittleEndian.Uint32(header[40:44]))
	if indexOffset > uint64(size) || indexSize > uint64(size)-indexOffset || indexSize > 64<<20 {
		return nil, errors.New("creation index range exceeds limit")
	}
	index := io.NewSectionReader(r, int64(indexOffset), int64(indexSize))
	var flags uint32
	err = binary.Read(index, binary.LittleEndian, &flags)
	if err != nil {
		return nil, fmt.Errorf("indexFlags: %w", err)
	}
	if flags & ^uint32(7) != 0 {
		return nil, errors.New("unsupported creation index flags")
	}
	var sharedKeys [3]uint32
	for position := range sharedKeys {
		if flags&(1<<position) == 0 {
			continue
		}
		err = binary.Read(index, binary.LittleEndian, &sharedKeys[position])
		if err != nil {
			return nil, fmt.Errorf("indexShared: %w", err)
		}
	}
	for ordinal := uint32(0); ordinal < resourceCount; ordinal++ {
		keys := sharedKeys
		for position := range keys {
			if flags&(1<<position) != 0 {
				continue
			}
			err = binary.Read(index, binary.LittleEndian, &keys[position])
			if err != nil {
				return nil, fmt.Errorf("indexKey[%d]: %w", ordinal, err)
			}
		}
		var instanceLow uint32
		var offset uint64
		var storedSize, decodedSize uint32
		var compression, entryFlags uint16
		for _, field := range []any{&instanceLow, &offset, &storedSize, &decodedSize, &compression, &entryFlags} {
			err = binary.Read(index, binary.LittleEndian, field)
			if err != nil {
				return nil, fmt.Errorf("indexField[%d]: %w", ordinal, err)
			}
		}
		pkg.entries = append(pkg.entries, creationEntry{Entry: dbpf.Entry{Type: keys[0], Group: keys[1], Instance: uint64(keys[2])<<32 | uint64(instanceLow), StoredSize: storedSize & 0x7fffffff, Size: decodedSize, Compression: compression, Flags: entryFlags}, offset: offset})
	}
	position, err := index.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("indexPosition: %w", err)
	}
	if uint64(position) != indexSize {
		return nil, errors.New("creation index size mismatch")
	}
	return pkg, nil
}

func readEntry(pkg *creationPackage, entry creationEntry) ([]byte, error) {
	if entry.Size > 32<<20 || entry.StoredSize > 32<<20 {
		return nil, errors.New("creation resource exceeds 32 MiB")
	}
	payload, err := readPackageResource(pkg, entry)
	if err != nil {
		return nil, fmt.Errorf("entryRead: %w", err)
	}
	return payload, nil
}

func readPackageResource(pkg *creationPackage, entry creationEntry) ([]byte, error) {
	if entry.offset > pkg.size || uint64(entry.StoredSize) > pkg.size-entry.offset {
		return nil, errors.New("creation resource exceeds package")
	}
	storedPayload, err := io.ReadAll(io.NewSectionReader(pkg.r, int64(entry.offset), int64(entry.StoredSize)))
	if err != nil {
		return nil, fmt.Errorf("resourceRead: %w", err)
	}
	payload, err := dbpf.Decode(entry.Entry, storedPayload)
	if err != nil {
		return nil, fmt.Errorf("resourceDecode: %w", err)
	}
	return payload, nil
}

// Package dbpf reads and writes Game DBPF package archives.
package dbpf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	HeaderSize         = 96
	entryCountOffset   = 0x24
	indexSizeOffset    = 0x2c
	indexVersionOffset = 0x3c
	indexOffset        = 0x40
	indexHasType       = 0x01
	indexHasGroup      = 0x02
	indexHasInstanceHi = 0x04
)

var magic = [4]byte{'D', 'B', 'P', 'F'}

// Header describes the DBPF fields needed to locate the resource index.
type Header struct {
	MajorVersion  uint32 `json:"major_version"`
	MinorVersion  uint32 `json:"minor_version"`
	IndexVersion  uint32 `json:"index_version"`
	ResourceCount uint32 `json:"resource_count"`
	IndexSize     uint32 `json:"index_size"`
	IndexOffset   uint64 `json:"index_offset"`
}

// Entry describes one indexed resource and its stored representation.
type Entry struct {
	Type             uint32 `json:"type"`
	Group            uint32 `json:"group"`
	Instance         uint64 `json:"instance"`
	Offset           uint32 `json:"offset"`
	StoredSize       uint32 `json:"stored_size"`
	Size             uint32 `json:"size"`
	Compression      uint16 `json:"compression"`
	Flags            uint16 `json:"flags"`
	isStoredSizeFlag bool
}

// Reader provides random-access navigation without loading payloads into memory.
type Reader struct {
	source       io.ReaderAt
	size         int64
	headerBytes  [HeaderSize]byte
	indexFlags   uint32
	sharedType   uint32
	sharedGroup  uint32
	sharedInstHi uint32
	Header       Header
	Entries      []Entry
}

// NewReader parses package navigation metadata from a random-access source.
func NewReader(r io.ReaderAt, size int64) (*Reader, error) {
	if r == nil {
		return nil, errors.New("nil reader")
	}
	if size < HeaderSize {
		return nil, fmt.Errorf("headerSize: got %d", size)
	}
	reader := &Reader{source: r, size: size}
	_, err := r.ReadAt(reader.headerBytes[:], 0)
	if err != nil {
		return nil, fmt.Errorf("headerRead: %w", err)
	}
	if string(reader.headerBytes[:len(magic)]) != string(magic[:]) {
		return nil, errors.New("headerMagic: not DBPF")
	}
	reader.Header = Header{
		MajorVersion:  binary.LittleEndian.Uint32(reader.headerBytes[4:8]),
		MinorVersion:  binary.LittleEndian.Uint32(reader.headerBytes[8:12]),
		ResourceCount: binary.LittleEndian.Uint32(reader.headerBytes[entryCountOffset : entryCountOffset+4]),
		IndexSize:     binary.LittleEndian.Uint32(reader.headerBytes[indexSizeOffset : indexSizeOffset+4]),
		IndexVersion:  binary.LittleEndian.Uint32(reader.headerBytes[indexVersionOffset : indexVersionOffset+4]),
		IndexOffset:   binary.LittleEndian.Uint64(reader.headerBytes[indexOffset : indexOffset+8]),
	}
	if reader.Header.MajorVersion != 3 {
		return nil, fmt.Errorf("headerVersion: got DBPF %d", reader.Header.MajorVersion)
	}
	indexEnd := reader.Header.IndexOffset + uint64(reader.Header.IndexSize)
	if reader.Header.IndexOffset > uint64(size) || indexEnd > uint64(size) {
		return nil, fmt.Errorf("headerIndex: range %d:%d exceeds %d", reader.Header.IndexOffset, indexEnd, size)
	}
	err = reader.readIndex()
	if err != nil {
		return nil, fmt.Errorf("indexRead: %w", err)
	}
	return reader, nil
}

// OpenRaw returns a bounded stream containing the resource exactly as stored.
func (reader *Reader) OpenRaw(entry Entry) (io.Reader, error) {
	end := uint64(entry.Offset) + uint64(entry.StoredSize)
	if end > uint64(reader.size) {
		return nil, fmt.Errorf("payloadRange: %d:%d exceeds %d", entry.Offset, end, reader.size)
	}
	return io.NewSectionReader(reader.source, int64(entry.Offset), int64(entry.StoredSize)), nil
}

// Open returns a resource in its decoded form. Game uses 0xffff for
// RefPack members and zero for members stored without compression.
func (reader *Reader) Open(entry Entry) (io.Reader, error) {
	raw, err := reader.OpenRaw(entry)
	if err != nil {
		return nil, fmt.Errorf("rawOpen: %w", err)
	}
	if entry.Compression == 0 {
		return raw, nil
	}
	if entry.Compression != 0xffff {
		return nil, fmt.Errorf("compressionUnsupported: 0x%04x", entry.Compression)
	}
	compressed, err := io.ReadAll(raw)
	if err != nil {
		return nil, fmt.Errorf("compressedRead: %w", err)
	}
	decoded, err := decodeRefPack(compressed, entry.Size)
	if err != nil {
		return nil, fmt.Errorf("refPackDecode: %w", err)
	}
	return bytes.NewReader(decoded), nil
}

// Decode expands one stored resource payload without reading it from the
// package again. Callers that retain exact stored bytes can hash and decode the
// same buffer instead of issuing a duplicate file read.
func Decode(entry Entry, storedPayload []byte) ([]byte, error) {
	if len(storedPayload) != int(entry.StoredSize) {
		return nil, fmt.Errorf("storedSize: got %d, want %d", len(storedPayload), entry.StoredSize)
	}
	if entry.Compression == 0 {
		return storedPayload, nil
	}
	if entry.Compression != 0xffff {
		return nil, fmt.Errorf("compressionUnsupported: 0x%04x", entry.Compression)
	}
	decodedPayload, err := decodeRefPack(storedPayload, entry.Size)
	if err != nil {
		return nil, fmt.Errorf("refPackDecode: %w", err)
	}
	return decodedPayload, nil
}

func (reader *Reader) readIndex() error {
	indexContents := make([]byte, reader.Header.IndexSize)
	_, err := reader.source.ReadAt(indexContents, int64(reader.Header.IndexOffset))
	if err != nil {
		return fmt.Errorf("contentsRead: %w", err)
	}
	r := bytes.NewReader(indexContents)
	err = binary.Read(r, binary.LittleEndian, &reader.indexFlags)
	if err != nil {
		return fmt.Errorf("flagsRead: %w", err)
	}
	if reader.indexFlags & ^uint32(indexHasType|indexHasGroup|indexHasInstanceHi) != 0 {
		return fmt.Errorf("flagsUnsupported: 0x%x", reader.indexFlags)
	}
	if reader.indexFlags&indexHasType != 0 {
		err = binary.Read(r, binary.LittleEndian, &reader.sharedType)
		if err != nil {
			return fmt.Errorf("typeRead: %w", err)
		}
	}
	if reader.indexFlags&indexHasGroup != 0 {
		err = binary.Read(r, binary.LittleEndian, &reader.sharedGroup)
		if err != nil {
			return fmt.Errorf("groupRead: %w", err)
		}
	}
	if reader.indexFlags&indexHasInstanceHi != 0 {
		err = binary.Read(r, binary.LittleEndian, &reader.sharedInstHi)
		if err != nil {
			return fmt.Errorf("instanceRead: %w", err)
		}
	}
	reader.Entries = make([]Entry, 0, reader.Header.ResourceCount)
	for ordinal := uint32(0); ordinal < reader.Header.ResourceCount; ordinal++ {
		entry, readErr := reader.readEntry(r)
		if readErr != nil {
			return fmt.Errorf("entryRead[%d]: %w", ordinal, readErr)
		}
		reader.Entries = append(reader.Entries, entry)
	}
	position := int64(len(indexContents) - r.Len())
	if position != int64(reader.Header.IndexSize) {
		return fmt.Errorf("sizeMismatch: parsed %d of %d bytes", position, reader.Header.IndexSize)
	}
	return nil
}

func (reader *Reader) readEntry(r io.Reader) (Entry, error) {
	entry := Entry{Type: reader.sharedType, Group: reader.sharedGroup}
	var instanceHi uint32
	var instanceLo uint32
	var storedSize uint32
	fields := make([]any, 0, 9)
	if reader.indexFlags&indexHasType == 0 {
		fields = append(fields, &entry.Type)
	}
	if reader.indexFlags&indexHasGroup == 0 {
		fields = append(fields, &entry.Group)
	}
	if reader.indexFlags&indexHasInstanceHi == 0 {
		fields = append(fields, &instanceHi)
	} else {
		instanceHi = reader.sharedInstHi
	}
	fields = append(fields, &instanceLo, &entry.Offset, &storedSize, &entry.Size, &entry.Compression, &entry.Flags)
	for _, field := range fields {
		err := binary.Read(r, binary.LittleEndian, field)
		if err != nil {
			return Entry{}, fmt.Errorf("fieldRead: %w", err)
		}
	}
	entry.Instance = uint64(instanceHi)<<32 | uint64(instanceLo)
	entry.isStoredSizeFlag = storedSize&0x80000000 != 0
	entry.StoredSize = storedSize & 0x7fffffff
	return entry, nil
}

package sqlite

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// assetCursor follows sub_9CD2C0's shared tail. Pointer words indicate
// presence only; arrays reserve every element before visiting descendants.
type assetCursor struct {
	payload []byte
	offset  int
}

func (e *assetCursor) reserve(count uint32, stride int) (int, error) {
	if stride <= 0 || e.offset < 0 || e.offset > len(e.payload) ||
		uint64(count) > uint64((len(e.payload)-e.offset)/stride) {
		return 0, fmt.Errorf("tailBounds: offset %d count %d stride %d", e.offset, count, stride)
	}
	start := e.offset
	e.offset += int(count) * stride
	return start, nil
}

func (e *assetCursor) reference(fieldOffset int) (*string, error) {
	if fieldOffset < 0 || fieldOffset > len(e.payload)-4 {
		return nil, fmt.Errorf("referenceBounds: %d", fieldOffset)
	}
	if binary.LittleEndian.Uint32(e.payload[fieldOffset:]) == 0 {
		return nil, nil
	}
	end := bytes.IndexByte(e.payload[e.offset:], 0)
	if end < 0 {
		return nil, fmt.Errorf("referenceTerminator: %d", e.offset)
	}
	text := string(e.payload[e.offset : e.offset+end])
	e.offset += end + 1
	return &text, nil
}

func (e *assetCursor) finish() error {
	if e.offset != len(e.payload) {
		return fmt.Errorf("tailRemaining: %d", len(e.payload)-e.offset)
	}
	return nil
}

func (e *assetCursor) optionalBody(fieldOffset, size int) (int, error) {
	if fieldOffset < 0 || fieldOffset > len(e.payload)-4 {
		return -1, fmt.Errorf("optionalBounds: %d", fieldOffset)
	}
	if binary.LittleEndian.Uint32(e.payload[fieldOffset:]) == 0 {
		return -1, nil
	}
	base, err := e.reserve(1, size)
	if err != nil {
		return -1, fmt.Errorf("optionalBody: %w", err)
	}
	return base, nil
}

func (e *assetCursor) references(offsets ...int) error {
	for _, offset := range offsets {
		reference, err := e.reference(offset)
		if err != nil {
			return fmt.Errorf("tailReference[%d]: %w", offset, err)
		}
		_ = reference // These fields are traversed without projecting their strings.
	}
	return nil
}

func fixedAssetString(payload []byte, offset, size int) string {
	text := payload[offset : offset+size]
	end := bytes.IndexByte(text, 0)
	if end >= 0 {
		text = text[:end]
	}
	return string(text)
}

package content

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Build-103 reflected pointers are presence words; descendants follow the
// complete fixed array and strings do not introduce alignment padding.
type lootAffixCursor struct {
	payload []byte
	offset  int
}

func (e *lootAffixCursor) reference(fieldOffset int) (string, error) {
	if fieldOffset < 0 || fieldOffset > len(e.payload)-4 {
		return "", fmt.Errorf("referenceBounds: %d", fieldOffset)
	}
	if binary.LittleEndian.Uint32(e.payload[fieldOffset:]) == 0 {
		return "", nil
	}
	end := bytes.IndexByte(e.payload[e.offset:], 0)
	if end < 0 {
		return "", fmt.Errorf("referenceTerminator: %d", e.offset)
	}
	text := string(e.payload[e.offset : e.offset+end])
	e.offset += end + 1
	return text, nil
}

func (e *lootAffixCursor) strings(fieldOffset int) ([]string, error) {
	presence := binary.LittleEndian.Uint32(e.payload[fieldOffset:])
	count := binary.LittleEndian.Uint32(e.payload[fieldOffset+4:])
	if presence == 0 && count != 0 {
		return nil, fmt.Errorf("arrayPresence: count %d", count)
	}
	if uint64(count) > uint64((len(e.payload)-e.offset)/4) {
		return nil, fmt.Errorf("arrayBounds: count %d", count)
	}
	start := e.offset
	e.offset += int(count) * 4
	strings := make([]string, 0, count)
	for index := uint32(0); index < count; index++ {
		text, err := e.reference(start + int(index)*4)
		if err != nil {
			return nil, fmt.Errorf("arrayString[%d]: %w", index, err)
		}
		strings = append(strings, text)
	}
	return strings, nil
}

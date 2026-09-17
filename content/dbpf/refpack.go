package dbpf

import "fmt"

func decodeRefPack(source []byte, expectedSize uint32) ([]byte, error) {
	if len(source) < 5 || source[1] != 0xfb {
		return nil, fmt.Errorf("headerInvalid: got %x", source[:minimum(len(source), 2)])
	}
	declaredSize := uint32(source[2])<<16 | uint32(source[3])<<8 | uint32(source[4])
	if source[0] != 0x10 && source[0] != 0x50 {
		return nil, fmt.Errorf("headerFlags: got 0x%02x", source[0])
	}
	if declaredSize != expectedSize {
		return nil, fmt.Errorf("sizeHeader: got %d, want %d", declaredSize, expectedSize)
	}
	destination := make([]byte, 0, expectedSize)
	position := 5
	for position < len(source) {
		control := source[position]
		position++
		literalCount := 0
		copyCount := 0
		copyOffset := 0
		switch {
		case control >= 0xfc:
			literalCount = int(control & 0x03)
			err := appendLiterals(&destination, source, &position, literalCount)
			if err != nil {
				return nil, fmt.Errorf("terminalLiteral: %w", err)
			}
			if len(destination) != int(expectedSize) {
				return nil, fmt.Errorf("terminalSize: got %d, want %d", len(destination), expectedSize)
			}
			return destination, nil
		case control >= 0xe0:
			literalCount = int(control&0x1f)<<2 + 4
		case control >= 0xc0:
			if position+3 > len(source) {
				return nil, fmt.Errorf("longCommand: truncated at %d", position)
			}
			first := source[position]
			second := source[position+1]
			third := source[position+2]
			position += 3
			literalCount = int(control & 0x03)
			copyCount = int(control&0x0c)<<6 + int(third) + 5
			copyOffset = int(control&0x10)<<12 + int(first)<<8 + int(second) + 1
		case control >= 0x80:
			if position+2 > len(source) {
				return nil, fmt.Errorf("mediumCommand: truncated at %d", position)
			}
			first := source[position]
			second := source[position+1]
			position += 2
			literalCount = int(first >> 6)
			copyCount = int(control&0x3f) + 4
			copyOffset = int(first&0x3f)<<8 + int(second) + 1
		default:
			if position >= len(source) {
				return nil, fmt.Errorf("shortCommand: truncated at %d", position)
			}
			first := source[position]
			position++
			literalCount = int(control & 0x03)
			copyCount = int(control&0x1c)>>2 + 3
			copyOffset = int(control&0x60)<<3 + int(first) + 1
		}
		err := appendLiterals(&destination, source, &position, literalCount)
		if err != nil {
			return nil, fmt.Errorf("literalCopy: %w", err)
		}
		if copyCount == 0 {
			continue
		}
		if copyOffset <= 0 || copyOffset > len(destination) {
			return nil, fmt.Errorf("copyOffset: got %d at output %d", copyOffset, len(destination))
		}
		if len(destination)+copyCount > int(expectedSize) {
			return nil, fmt.Errorf("copySize: %d exceeds %d", len(destination)+copyCount, expectedSize)
		}
		for count := 0; count < copyCount; count++ {
			destination = append(destination, destination[len(destination)-copyOffset])
		}
	}
	return nil, fmt.Errorf("terminalMissing: decoded %d of %d", len(destination), expectedSize)
}

func appendLiterals(destination *[]byte, source []byte, position *int, count int) error {
	if *position+count > len(source) {
		return fmt.Errorf("literalRange: %d:%d exceeds %d", *position, *position+count, len(source))
	}
	*destination = append(*destination, source[*position:*position+count]...)
	*position += count
	return nil
}

func minimum(left, right int) int {
	if left < right {
		return left
	}
	return right
}

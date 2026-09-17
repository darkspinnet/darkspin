// Package databuffer implements the binary buffer shared by the legacy Blaze,
// RakNet, and game protocols.
package databuffer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Buffer is a growable binary buffer with an independent read/write cursor.
type Buffer struct {
	data     []byte
	position int
}

// New returns an empty buffer.
func New() *Buffer {
	return &Buffer{}
}

// FromBytes returns a buffer containing an independent copy of data.
func FromBytes(data []byte) *Buffer {
	contents := append([]byte(nil), data...)
	return &Buffer{data: contents}
}

// Bytes returns the buffer's used bytes. The returned slice aliases the buffer.
func (b *Buffer) Bytes() []byte {
	return b.data
}

// Position returns the current cursor position.
func (b *Buffer) Position() int {
	return b.position
}

// SetPosition moves the cursor, clamped to the used buffer size.
func (b *Buffer) SetPosition(position int) {
	if position < 0 {
		position = 0
	}
	if position > len(b.data) {
		position = len(b.data)
	}
	b.position = position
}

// Size returns the number of used bytes.
func (b *Buffer) Size() int {
	return len(b.data)
}

// Clear removes all data and resets the cursor.
func (b *Buffer) Clear() {
	b.data = nil
	b.position = 0
}

// EOF reports whether the cursor is at the end of the used data.
func (b *Buffer) EOF() bool {
	return b.position >= len(b.data)
}

// Insert appends the used bytes from another buffer without moving the cursor.
func (b *Buffer) Insert(other *Buffer) {
	if other == nil {
		return
	}
	b.data = append(b.data, other.data...)
}

// PeekBytes returns a copy of the next count bytes without moving the cursor.
func (b *Buffer) PeekBytes(count int) ([]byte, error) {
	if count < 0 {
		return nil, errors.New("peek bytes: negative count")
	}
	end := b.position + count
	if end > len(b.data) {
		return nil, io.ErrUnexpectedEOF
	}
	return append([]byte(nil), b.data[b.position:end]...), nil
}

// ReadBytes returns a copy of the next count bytes and advances the cursor.
func (b *Buffer) ReadBytes(count int) ([]byte, error) {
	contents, err := b.PeekBytes(count)
	if err != nil {
		return nil, fmt.Errorf("readBytes[%d]: %w", count, err)
	}
	b.position += count
	return contents, nil
}

// WriteBytes writes bytes at the cursor, growing the buffer when required.
func (b *Buffer) WriteBytes(contents []byte) {
	end := b.position + len(contents)
	if end > len(b.data) {
		b.data = append(b.data, make([]byte, end-len(b.data))...)
	}
	copy(b.data[b.position:end], contents)
	b.position = end
}

// Skip advances the cursor by count bytes.
func (b *Buffer) Skip(count int) error {
	if count < 0 || b.position+count > len(b.data) {
		return io.ErrUnexpectedEOF
	}
	b.position += count
	return nil
}

// EncodeTDFInteger writes Blaze's variable-length unsigned integer encoding.
func (b *Buffer) EncodeTDFInteger(value uint64) {
	if value < 0x40 {
		b.WriteBytes([]byte{byte(value)})
		return
	}

	b.WriteBytes([]byte{byte(value&0x3f) | 0x80})
	value >>= 6
	for value >= 0x80 {
		b.WriteBytes([]byte{byte(value&0x7f) | 0x80})
		value >>= 7
	}
	b.WriteBytes([]byte{byte(value)})
}

// DecodeTDFInteger reads Blaze's variable-length unsigned integer encoding.
func (b *Buffer) DecodeTDFInteger() (uint64, error) {
	first, err := b.ReadBytes(1)
	if err != nil {
		return 0, fmt.Errorf("tdfFirst: %w", err)
	}
	value := uint64(first[0])
	if value < 0x80 {
		return value, nil
	}

	value &= 0x3f
	for index := uint(1); index < 10; index++ {
		next, readErr := b.ReadBytes(1)
		if readErr != nil {
			return 0, fmt.Errorf("tdfByte[%d]: %w", index+1, readErr)
		}
		shift := (index * 7) - 1
		chunk := uint64(next[0] & 0x7f)
		if shift >= 64 || chunk > math.MaxUint64>>shift {
			return 0, errors.New("decode TDF integer: overflow")
		}
		value |= chunk << shift
		if next[0] < 0x80 {
			return value, nil
		}
	}
	return 0, errors.New("decode TDF integer: too many bytes")
}

func (b *Buffer) PeekUint16LE() (uint16, error)   { return b.peekUint16(binary.LittleEndian) }
func (b *Buffer) PeekUint32LE() (uint32, error)   { return b.peekUint32(binary.LittleEndian) }
func (b *Buffer) PeekUint64LE() (uint64, error)   { return b.peekUint64(binary.LittleEndian) }
func (b *Buffer) PeekFloat32LE() (float32, error) { return b.peekFloat32(binary.LittleEndian) }
func (b *Buffer) PeekFloat64LE() (float64, error) { return b.peekFloat64(binary.LittleEndian) }
func (b *Buffer) PeekUint16BE() (uint16, error)   { return b.peekUint16(binary.BigEndian) }
func (b *Buffer) PeekUint32BE() (uint32, error)   { return b.peekUint32(binary.BigEndian) }
func (b *Buffer) PeekUint64BE() (uint64, error)   { return b.peekUint64(binary.BigEndian) }
func (b *Buffer) PeekFloat32BE() (float32, error) { return b.peekFloat32(binary.BigEndian) }
func (b *Buffer) PeekFloat64BE() (float64, error) { return b.peekFloat64(binary.BigEndian) }

func (b *Buffer) ReadUint16LE() (uint16, error)   { return b.readUint16(binary.LittleEndian) }
func (b *Buffer) ReadUint32LE() (uint32, error)   { return b.readUint32(binary.LittleEndian) }
func (b *Buffer) ReadUint64LE() (uint64, error)   { return b.readUint64(binary.LittleEndian) }
func (b *Buffer) ReadFloat32LE() (float32, error) { return b.readFloat32(binary.LittleEndian) }
func (b *Buffer) ReadFloat64LE() (float64, error) { return b.readFloat64(binary.LittleEndian) }
func (b *Buffer) ReadUint16BE() (uint16, error)   { return b.readUint16(binary.BigEndian) }
func (b *Buffer) ReadUint32BE() (uint32, error)   { return b.readUint32(binary.BigEndian) }
func (b *Buffer) ReadUint64BE() (uint64, error)   { return b.readUint64(binary.BigEndian) }
func (b *Buffer) ReadFloat32BE() (float32, error) { return b.readFloat32(binary.BigEndian) }
func (b *Buffer) ReadFloat64BE() (float64, error) { return b.readFloat64(binary.BigEndian) }

func (b *Buffer) WriteUint16LE(value uint16) { b.writeUint16(binary.LittleEndian, value) }
func (b *Buffer) WriteUint32LE(value uint32) { b.writeUint32(binary.LittleEndian, value) }
func (b *Buffer) WriteUint64LE(value uint64) { b.writeUint64(binary.LittleEndian, value) }
func (b *Buffer) WriteFloat32LE(value float32) {
	b.writeUint32(binary.LittleEndian, math.Float32bits(value))
}
func (b *Buffer) WriteFloat64LE(value float64) {
	b.writeUint64(binary.LittleEndian, math.Float64bits(value))
}
func (b *Buffer) WriteUint16BE(value uint16) { b.writeUint16(binary.BigEndian, value) }
func (b *Buffer) WriteUint32BE(value uint32) { b.writeUint32(binary.BigEndian, value) }
func (b *Buffer) WriteUint64BE(value uint64) { b.writeUint64(binary.BigEndian, value) }
func (b *Buffer) WriteFloat32BE(value float32) {
	b.writeUint32(binary.BigEndian, math.Float32bits(value))
}
func (b *Buffer) WriteFloat64BE(value float64) {
	b.writeUint64(binary.BigEndian, math.Float64bits(value))
}

func (b *Buffer) peekUint16(order binary.ByteOrder) (uint16, error) {
	contents, err := b.PeekBytes(2)
	if err != nil {
		return 0, fmt.Errorf("peekU16: %w", err)
	}
	return order.Uint16(contents), nil
}

func (b *Buffer) peekUint32(order binary.ByteOrder) (uint32, error) {
	contents, err := b.PeekBytes(4)
	if err != nil {
		return 0, fmt.Errorf("peekU32: %w", err)
	}
	return order.Uint32(contents), nil
}

func (b *Buffer) peekUint64(order binary.ByteOrder) (uint64, error) {
	contents, err := b.PeekBytes(8)
	if err != nil {
		return 0, fmt.Errorf("peekU64: %w", err)
	}
	return order.Uint64(contents), nil
}

func (b *Buffer) peekFloat32(order binary.ByteOrder) (float32, error) {
	value, err := b.peekUint32(order)
	if err != nil {
		return 0, fmt.Errorf("peekF32: %w", err)
	}
	return math.Float32frombits(value), nil
}

func (b *Buffer) peekFloat64(order binary.ByteOrder) (float64, error) {
	value, err := b.peekUint64(order)
	if err != nil {
		return 0, fmt.Errorf("peekF64: %w", err)
	}
	return math.Float64frombits(value), nil
}

func (b *Buffer) readUint16(order binary.ByteOrder) (uint16, error) {
	contents, err := b.ReadBytes(2)
	if err != nil {
		return 0, fmt.Errorf("readU16: %w", err)
	}
	return order.Uint16(contents), nil
}

func (b *Buffer) readUint32(order binary.ByteOrder) (uint32, error) {
	contents, err := b.ReadBytes(4)
	if err != nil {
		return 0, fmt.Errorf("readU32: %w", err)
	}
	return order.Uint32(contents), nil
}

func (b *Buffer) readUint64(order binary.ByteOrder) (uint64, error) {
	contents, err := b.ReadBytes(8)
	if err != nil {
		return 0, fmt.Errorf("readU64: %w", err)
	}
	return order.Uint64(contents), nil
}

func (b *Buffer) readFloat32(order binary.ByteOrder) (float32, error) {
	value, err := b.readUint32(order)
	if err != nil {
		return 0, fmt.Errorf("readF32: %w", err)
	}
	return math.Float32frombits(value), nil
}

func (b *Buffer) readFloat64(order binary.ByteOrder) (float64, error) {
	value, err := b.readUint64(order)
	if err != nil {
		return 0, fmt.Errorf("readF64: %w", err)
	}
	return math.Float64frombits(value), nil
}

func (b *Buffer) writeUint16(order binary.ByteOrder, value uint16) {
	contents := make([]byte, 2)
	order.PutUint16(contents, value)
	b.WriteBytes(contents)
}

func (b *Buffer) writeUint32(order binary.ByteOrder, value uint32) {
	contents := make([]byte, 4)
	order.PutUint32(contents, value)
	b.WriteBytes(contents)
}

func (b *Buffer) writeUint64(order binary.ByteOrder, value uint64) {
	contents := make([]byte, 8)
	order.PutUint64(contents, value)
	b.WriteBytes(contents)
}

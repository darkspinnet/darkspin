// Package raknet implements the Game gameplay transport and payload codec.
package raknet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var ErrShortBuffer = errors.New("raknet: short buffer")

// Vector2 and Vector3 are the native gameplay coordinate types.
type Vector2 struct{ X, Y float32 }
type Vector3 struct{ X, Y, Z float32 }
type Quaternion struct{ X, Y, Z, W float32 }

// Reader decodes big-endian RakNet transport fields. Game application
// payloads are little-endian and must not use this transport codec.
type Reader struct {
	data   []byte
	offset int
}

func NewReader(data []byte) *Reader { return &Reader{data: data} }
func (r *Reader) Remaining() int    { return len(r.data) - r.offset }
func (r *Reader) Offset() int       { return r.offset }

func (r *Reader) Bytes(length int) ([]byte, error) {
	if length < 0 || r.Remaining() < length {
		return nil, ErrShortBuffer
	}
	value := r.data[r.offset : r.offset+length]
	r.offset += length
	return value, nil
}

func (r *Reader) Uint8() (uint8, error) {
	value, err := r.Bytes(1)
	if err != nil {
		return 0, fmt.Errorf("u8Read: %w", err)
	}
	return value[0], nil
}

func (r *Reader) Uint16() (uint16, error) {
	value, err := r.Bytes(2)
	if err != nil {
		return 0, fmt.Errorf("u16Read: %w", err)
	}
	return binary.BigEndian.Uint16(value), nil
}

func (r *Reader) Uint32() (uint32, error) {
	value, err := r.Bytes(4)
	if err != nil {
		return 0, fmt.Errorf("u32Read: %w", err)
	}
	return binary.BigEndian.Uint32(value), nil
}

func (r *Reader) Uint64() (uint64, error) {
	value, err := r.Bytes(8)
	if err != nil {
		return 0, fmt.Errorf("u64Read: %w", err)
	}
	return binary.BigEndian.Uint64(value), nil
}

func (r *Reader) Float32() (float32, error) {
	value, err := r.Uint32()
	if err != nil {
		return 0, fmt.Errorf("f32Read: %w", err)
	}
	return math.Float32frombits(value), nil
}

func (r *Reader) String() (string, error) {
	length, err := r.Uint16()
	if err != nil {
		return "", fmt.Errorf("stringLength: %w", err)
	}
	value, err := r.Bytes(int(length))
	if err != nil {
		return "", fmt.Errorf("stringPayload: %w", err)
	}
	return string(value), nil
}

func (r *Reader) Vector3() (Vector3, error) {
	x, err := r.Float32()
	if err != nil {
		return Vector3{}, fmt.Errorf("vectorX: %w", err)
	}
	y, err := r.Float32()
	if err != nil {
		return Vector3{}, fmt.Errorf("vectorY: %w", err)
	}
	z, err := r.Float32()
	if err != nil {
		return Vector3{}, fmt.Errorf("vectorZ: %w", err)
	}
	return Vector3{X: x, Y: y, Z: z}, nil
}

// Writer encodes big-endian RakNet transport fields.
type Writer struct{ data []byte }

func NewWriter(capacity ...int) *Writer {
	size := 0
	if len(capacity) > 0 {
		size = capacity[0]
	}
	return &Writer{data: make([]byte, 0, size)}
}

func (w *Writer) Bytes() []byte           { return append([]byte(nil), w.data...) }
func (w *Writer) WriteBytes(value []byte) { w.data = append(w.data, value...) }
func (w *Writer) Uint8(value uint8)       { w.data = append(w.data, value) }
func (w *Writer) Bool8(isEnabled bool) {
	if isEnabled {
		w.Uint8(1)
		return
	}
	w.Uint8(0)
}

func (w *Writer) Uint16(value uint16) {
	buffer := make([]byte, 2)
	binary.BigEndian.PutUint16(buffer, value)
	w.WriteBytes(buffer)
}

func (w *Writer) Uint32(value uint32) {
	buffer := make([]byte, 4)
	binary.BigEndian.PutUint32(buffer, value)
	w.WriteBytes(buffer)
}

func (w *Writer) Uint64(value uint64) {
	buffer := make([]byte, 8)
	binary.BigEndian.PutUint64(buffer, value)
	w.WriteBytes(buffer)
}

func (w *Writer) Float32(value float32) { w.Uint32(math.Float32bits(value)) }

func (w *Writer) String(value string) {
	length := len(value)
	if length > math.MaxUint16 {
		length = math.MaxUint16
	}
	w.Uint16(uint16(length))
	w.WriteBytes([]byte(value[:length]))
}

func (w *Writer) Vector3(value Vector3) {
	w.Float32(value.X)
	w.Float32(value.Y)
	w.Float32(value.Z)
}

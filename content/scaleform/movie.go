package scaleform

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	MovieResourceType        uint32 = 0x278CF8F2
	ImageResourceType        uint32 = 0x17952E6C
	ImageVariantResourceType uint32 = 0xB8444447
)

type Rect struct {
	XMin int32
	XMax int32
	YMin int32
	YMax int32
}

type Tag struct {
	Code      uint16
	Payload   []byte
	IsLong    bool
	ActionRef string
}

type Movie struct {
	Version      uint8
	Compression  string
	FrameSize    Rect
	FrameRateRaw uint16
	FrameCount   uint16
	Tags         []Tag
}

type Image struct {
	DDS []byte
}

func IsResourceType(resourceType uint32) bool {
	return resourceType == MovieResourceType || resourceType == ImageResourceType || resourceType == ImageVariantResourceType
}

func DecodeMovie(payload []byte) (*Movie, error) {
	if len(payload) < 8 {
		return nil, errors.New("headerShort")
	}
	signature := string(payload[:3])
	if signature != "CFX" && signature != "GFX" {
		return nil, fmt.Errorf("signature: %q", signature)
	}
	declaredSize := binary.LittleEndian.Uint32(payload[4:8])
	body := payload[8:]
	compression := "NONE"
	if signature == "CFX" {
		compression = "ZLIB"
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("zlibOpen: %w", err)
		}
		body, err = io.ReadAll(r)
		if err != nil {
			_ = r.Close()
			return nil, fmt.Errorf("zlibRead: %w", err)
		}
		err = r.Close()
		if err != nil {
			return nil, fmt.Errorf("zlibClose: %w", err)
		}
	}
	if uint64(len(body))+8 != uint64(declaredSize) {
		return nil, fmt.Errorf("declaredSize: got %d, want %d", len(body)+8, declaredSize)
	}
	frameSize, rectBytes, err := decodeRect(body)
	if err != nil {
		return nil, fmt.Errorf("frameSize: %w", err)
	}
	if len(body) < rectBytes+4 {
		return nil, errors.New("frameHeaderShort")
	}
	movie := &Movie{
		Version:      payload[3],
		Compression:  compression,
		FrameSize:    frameSize,
		FrameRateRaw: binary.LittleEndian.Uint16(body[rectBytes : rectBytes+2]),
		FrameCount:   binary.LittleEndian.Uint16(body[rectBytes+2 : rectBytes+4]),
	}
	movie.Tags, err = decodeTags(body[rectBytes+4:])
	if err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	return movie, nil
}

func EncodeMovie(movie *Movie) ([]byte, error) {
	if movie == nil {
		return nil, errors.New("movieMissing")
	}
	rectPayload, err := encodeRect(movie.FrameSize)
	if err != nil {
		return nil, fmt.Errorf("frameSize: %w", err)
	}
	var body bytes.Buffer
	_, err = body.Write(rectPayload)
	if err == nil {
		err = binary.Write(&body, binary.LittleEndian, movie.FrameRateRaw)
	}
	if err == nil {
		err = binary.Write(&body, binary.LittleEndian, movie.FrameCount)
	}
	if err == nil {
		err = encodeTags(&body, movie.Tags)
	}
	if err != nil {
		return nil, fmt.Errorf("bodyWrite: %w", err)
	}
	signature := "GFX"
	encodedBody := body.Bytes()
	if movie.Compression == "ZLIB" {
		signature = "CFX"
		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		_, err = w.Write(encodedBody)
		if err == nil {
			err = w.Close()
		} else {
			_ = w.Close()
		}
		if err != nil {
			return nil, fmt.Errorf("zlibWrite: %w", err)
		}
		encodedBody = compressed.Bytes()
	} else if movie.Compression != "NONE" {
		return nil, fmt.Errorf("compression: %q", movie.Compression)
	}
	payload := make([]byte, 8, 8+len(encodedBody))
	copy(payload, signature)
	payload[3] = movie.Version
	binary.LittleEndian.PutUint32(payload[4:8], uint32(body.Len()+8))
	payload = append(payload, encodedBody...)
	return payload, nil
}

func DecodeImage(payload []byte) (*Image, error) {
	if len(payload) < 4 {
		return nil, errors.New("imageShort")
	}
	if string(payload[:4]) != "DDS " {
		return nil, errors.New("ddsMissing")
	}
	return &Image{DDS: append([]byte(nil), payload...)}, nil
}

func EncodeImage(image *Image) ([]byte, error) {
	if image == nil || len(image.DDS) < 4 || string(image.DDS[:4]) != "DDS " {
		return nil, errors.New("ddsInvalid")
	}
	return append([]byte(nil), image.DDS...), nil
}

func decodeTags(payload []byte) ([]Tag, error) {
	tags := make([]Tag, 0, 32)
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 2 {
			return nil, fmt.Errorf("header[%d]: short", len(tags))
		}
		header := binary.LittleEndian.Uint16(payload[offset : offset+2])
		offset += 2
		code := header >> 6
		length := uint32(header & 0x3F)
		isLong := length == 0x3F
		if isLong {
			if len(payload)-offset < 4 {
				return nil, fmt.Errorf("length[%d]: short", len(tags))
			}
			length = binary.LittleEndian.Uint32(payload[offset : offset+4])
			offset += 4
		}
		if uint64(length) > uint64(len(payload)-offset) {
			return nil, fmt.Errorf("payload[%d]: got %d, remain %d", len(tags), length, len(payload)-offset)
		}
		tagPayload := append([]byte(nil), payload[offset:offset+int(length)]...)
		offset += int(length)
		tags = append(tags, Tag{Code: code, Payload: tagPayload, IsLong: isLong})
	}
	return tags, nil
}

func encodeTags(w io.Writer, tags []Tag) error {
	for tagIndex, tag := range tags {
		length := len(tag.Payload)
		isLong := tag.IsLong || length >= 0x3F
		headerLength := uint16(length)
		if isLong {
			headerLength = 0x3F
		}
		header := tag.Code<<6 | headerLength
		err := binary.Write(w, binary.LittleEndian, header)
		if err == nil && isLong {
			err = binary.Write(w, binary.LittleEndian, uint32(length))
		}
		if err == nil {
			_, err = w.Write(tag.Payload)
		}
		if err != nil {
			return fmt.Errorf("tag[%d]: %w", tagIndex, err)
		}
	}
	return nil
}

func decodeRect(payload []byte) (Rect, int, error) {
	if len(payload) == 0 {
		return Rect{}, 0, errors.New("missing")
	}
	bits := newBitReader(payload)
	bitCount, err := bits.readUnsigned(5)
	if err != nil || bitCount == 0 || bitCount > 31 {
		return Rect{}, 0, fmt.Errorf("bitCount: %d", bitCount)
	}
	coordinates := make([]int32, 4)
	for coordinateIndex := range coordinates {
		coordinate, readErr := bits.readSigned(int(bitCount))
		if readErr != nil {
			return Rect{}, 0, fmt.Errorf("coordinate[%d]: %w", coordinateIndex, readErr)
		}
		coordinates[coordinateIndex] = coordinate
	}
	return Rect{XMin: coordinates[0], XMax: coordinates[1], YMin: coordinates[2], YMax: coordinates[3]}, (bits.offset + 7) / 8, nil
}

func encodeRect(rect Rect) ([]byte, error) {
	coordinates := []int32{rect.XMin, rect.XMax, rect.YMin, rect.YMax}
	bitCount := 1
	for _, coordinate := range coordinates {
		for candidate := 1; candidate <= 31; candidate++ {
			minimum := -int64(1 << (candidate - 1))
			maximum := int64(1<<(candidate-1)) - 1
			if int64(coordinate) >= minimum && int64(coordinate) <= maximum {
				if candidate > bitCount {
					bitCount = candidate
				}
				break
			}
		}
	}
	bits := newBitWriter()
	bits.writeUnsigned(uint32(bitCount), 5)
	for _, coordinate := range coordinates {
		bits.writeUnsigned(uint32(coordinate)&uint32((uint64(1)<<bitCount)-1), bitCount)
	}
	return bits.bytes(), nil
}

type bitReader struct {
	payload []byte
	offset  int
}

func newBitReader(payload []byte) *bitReader { return &bitReader{payload: payload} }

func (e *bitReader) readUnsigned(count int) (uint32, error) {
	if count < 0 || e.offset+count > len(e.payload)*8 {
		return 0, io.ErrUnexpectedEOF
	}
	var number uint32
	for bitIndex := 0; bitIndex < count; bitIndex++ {
		byteIndex := e.offset / 8
		shift := 7 - e.offset%8
		number = number<<1 | uint32(e.payload[byteIndex]>>shift&1)
		e.offset++
	}
	return number, nil
}

func (e *bitReader) readSigned(count int) (int32, error) {
	number, err := e.readUnsigned(count)
	if err != nil {
		return 0, err
	}
	if number&(1<<uint(count-1)) != 0 {
		number |= ^uint32(0) << uint(count)
	}
	return int32(number), nil
}

type bitWriter struct {
	payload []byte
	offset  int
}

func newBitWriter() *bitWriter { return &bitWriter{} }

func (e *bitWriter) writeUnsigned(number uint32, count int) {
	for bitIndex := count - 1; bitIndex >= 0; bitIndex-- {
		if e.offset%8 == 0 {
			e.payload = append(e.payload, 0)
		}
		if number&(1<<uint(bitIndex)) != 0 {
			e.payload[len(e.payload)-1] |= 1 << uint(7-e.offset%8)
		}
		e.offset++
	}
}

func (e *bitWriter) bytes() []byte { return e.payload }

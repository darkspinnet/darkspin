// Package movie reads and writes Electronic Arts VP6 movie resources.
package movie

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

const ResourceType uint32 = 0x376840D7

// KnownNames returns movie identities proven by the package hash function.
func KnownNames() map[uint32]string {
	return map[uint32]string{
		0x4315C729: "intro",
	}
}

const (
	movieHeaderTag = "MVhd"
	keyFrameTag    = "MV0K"
	interFrameTag  = "MV0F"
)

// Header is the fixed 24-byte body of an EA MVhd chunk.
type Header struct {
	Codec        string
	Width        uint16
	Height       uint16
	FrameCount   uint32
	LargestFrame uint32
	TimeBaseDen  uint32
	TimeBaseNum  uint32
}

// Packet is one VP6 frame carried by an MV0K or MV0F chunk.
type Packet struct {
	IsKeyframe bool
	Payload    []byte
}

// Document is the semantic movie header and its VP6 packets. OtherChunks
// retains every non-video EA chunk in source order for exact audio/header
// preservation while the video stream is unchanged.
type Document struct {
	Header  Header
	Audio   *AudioHeader
	Packets []Packet
	Chunks  []Chunk
}

// AudioHeader is the semantic GSTR description carried by SCHl.
type AudioHeader struct {
	Revision    uint32
	Revision2   uint32
	Codec       string
	Channels    uint32
	SampleRate  uint32
	SampleCount uint32
	BlockCount  uint32
}

// Chunk is one complete EA container chunk without its eight-byte preamble.
type Chunk struct {
	Tag     string
	Payload []byte
}

// Decode parses and strictly bounds every EA chunk in a VP6 movie resource.
func Decode(payload []byte) (*Document, error) {
	chunks, err := decodeChunks(payload)
	if err != nil {
		return nil, fmt.Errorf("chunkDecode: %w", err)
	}
	if len(chunks) == 0 || chunks[0].Tag != movieHeaderTag {
		return nil, errors.New("movieHeader: first chunk is not MVhd")
	}
	header, err := decodeHeader(chunks[0].Payload)
	if err != nil {
		return nil, fmt.Errorf("movieHeader: %w", err)
	}
	document := &Document{Header: header, Chunks: chunks}
	audioBlockCount := uint32(0)
	for index, chunk := range chunks {
		if chunk.Tag == "SCHl" {
			if document.Audio != nil {
				return nil, fmt.Errorf("audioHeader[%d]: duplicate", index)
			}
			document.Audio, err = decodeAudioHeader(chunk.Payload)
			if err != nil {
				return nil, fmt.Errorf("audioHeader[%d]: %w", index, err)
			}
			continue
		}
		if chunk.Tag == "SCCl" {
			if len(chunk.Payload) != 4 || document.Audio == nil {
				return nil, fmt.Errorf("audioCount[%d]: invalid", index)
			}
			document.Audio.BlockCount = binary.LittleEndian.Uint32(chunk.Payload)
			continue
		}
		if chunk.Tag == "SCDl" {
			audioBlockCount++
			continue
		}
		if chunk.Tag != keyFrameTag && chunk.Tag != interFrameTag {
			continue
		}
		if len(chunk.Payload) == 0 {
			return nil, fmt.Errorf("videoChunk[%d]: empty", index)
		}
		document.Packets = append(document.Packets, Packet{
			IsKeyframe: chunk.Tag == keyFrameTag,
			Payload:    append([]byte(nil), chunk.Payload...),
		})
	}
	if len(document.Packets) != int(header.FrameCount) {
		return nil, fmt.Errorf("frameCount: got %d, want %d", len(document.Packets), header.FrameCount)
	}
	if document.Audio != nil && document.Audio.BlockCount != audioBlockCount {
		return nil, fmt.Errorf("audioBlockCount: got %d, want %d", audioBlockCount, document.Audio.BlockCount)
	}
	return document, nil
}

func decodeAudioHeader(payload []byte) (*AudioHeader, error) {
	if len(payload) < 12 || string(payload[0:4]) != "GSTR" {
		return nil, errors.New("gstrHeader: missing")
	}
	header := &AudioHeader{}
	compression := uint32(^uint32(0))
	for offset := 8; offset < len(payload); {
		tag := payload[offset]
		offset++
		if tag == 0xFF {
			break
		}
		if tag == 0xFD {
			continue
		}
		field, nextOffset, err := readAudioField(payload, offset)
		if err != nil {
			return nil, fmt.Errorf("field[0x%02X]: %w", tag, err)
		}
		offset = nextOffset
		switch tag {
		case 0x80:
			header.Revision = field
		case 0x82:
			header.Channels = field
		case 0x83:
			compression = field
		case 0x84:
			header.SampleRate = field
		case 0x85:
			header.SampleCount = field
		case 0xA0:
			header.Revision2 = field
		}
	}
	if header.Channels == 0 || header.SampleRate == 0 || header.SampleCount == 0 {
		return nil, fmt.Errorf("format: channels %d, rate %d, samples %d", header.Channels, header.SampleRate, header.SampleCount)
	}
	switch {
	case compression == 0:
		header.Codec = "PCM16LE"
	case header.Revision == 3 && header.Revision2 == 23:
		header.Codec = "EALAYER3"
	case compression == 7:
		header.Codec = "EAADPCM"
	default:
		header.Codec = fmt.Sprintf("UNKNOWN_0x%08X_0x%08X_0x%08X", compression, header.Revision, header.Revision2)
	}
	return header, nil
}

func readAudioField(payload []byte, offset int) (uint32, int, error) {
	if offset >= len(payload) {
		return 0, offset, io.ErrUnexpectedEOF
	}
	size := int(payload[offset])
	offset++
	if size < 1 || size > 4 || len(payload)-offset < size {
		return 0, offset, fmt.Errorf("size: %d", size)
	}
	field := uint32(0)
	for _, octet := range payload[offset : offset+size] {
		field = field<<8 | uint32(octet)
	}
	return field, offset + size, nil
}

func decodeChunks(payload []byte) ([]Chunk, error) {
	chunks := make([]Chunk, 0)
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 8 {
			return nil, fmt.Errorf("chunkHeader[%d]: %d trailing bytes", len(chunks), len(payload)-offset)
		}
		tag := string(payload[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(payload[offset+4 : offset+8])
		if chunkSize < 8 {
			return nil, fmt.Errorf("chunkSize[%d]: got %d, want at least 8", len(chunks), chunkSize)
		}
		chunkEnd := uint64(offset) + uint64(chunkSize)
		if chunkEnd > uint64(len(payload)) {
			return nil, fmt.Errorf("chunkBounds[%d]: end %d exceeds %d", len(chunks), chunkEnd, len(payload))
		}
		chunks = append(chunks, Chunk{
			Tag:     tag,
			Payload: append([]byte(nil), payload[offset+8:int(chunkEnd)]...),
		})
		offset = int(chunkEnd)
	}
	return chunks, nil
}

func decodeHeader(payload []byte) (Header, error) {
	if len(payload) != 24 {
		return Header{}, fmt.Errorf("size: got %d, want 24", len(payload))
	}
	header := Header{
		Codec:        string(payload[0:4]),
		Width:        binary.LittleEndian.Uint16(payload[4:6]),
		Height:       binary.LittleEndian.Uint16(payload[6:8]),
		FrameCount:   binary.LittleEndian.Uint32(payload[8:12]),
		LargestFrame: binary.LittleEndian.Uint32(payload[12:16]),
		TimeBaseDen:  binary.LittleEndian.Uint32(payload[16:20]),
		TimeBaseNum:  binary.LittleEndian.Uint32(payload[20:24]),
	}
	if !strings.EqualFold(header.Codec, "VP60") && !strings.EqualFold(header.Codec, "VP61") && !strings.EqualFold(header.Codec, "VP62") {
		return Header{}, fmt.Errorf("codec: unsupported %q", header.Codec)
	}
	if header.Width == 0 || header.Height == 0 {
		return Header{}, fmt.Errorf("geometry: %dx%d", header.Width, header.Height)
	}
	if header.FrameCount == 0 {
		return Header{}, errors.New("frameCount: zero")
	}
	if header.TimeBaseDen == 0 || header.TimeBaseNum == 0 {
		return Header{}, fmt.Errorf("timeBase: %d/%d", header.TimeBaseNum, header.TimeBaseDen)
	}
	return header, nil
}

// EncodeVideo writes a video-only EA movie using the supplied VP6 packets.
// Audio chunks are added separately once an editable AVI audio stream exists.
func EncodeVideo(header Header, packets []Packet) ([]byte, error) {
	return encode(header, packets, nil)
}

// EncodeVideoPCM writes VP6 video and editable PCM16LE audio in EA SCHl/SCDl
// chunks. Game infers all chunk sizes and sample counts from this stream.
func EncodeVideoPCM(header Header, packets []Packet, audio *AVIAudio) ([]byte, error) {
	if audio == nil {
		return nil, errors.New("audio: nil")
	}
	return encode(header, packets, audio)
}

func encode(header Header, packets []Packet, audio *AVIAudio) ([]byte, error) {
	if len(packets) == 0 {
		return nil, errors.New("packets: empty")
	}
	if !strings.EqualFold(header.Codec, "VP60") && !strings.EqualFold(header.Codec, "VP61") && !strings.EqualFold(header.Codec, "VP62") {
		return nil, fmt.Errorf("codec: unsupported %q", header.Codec)
	}
	if header.Width == 0 || header.Height == 0 {
		return nil, fmt.Errorf("geometry: %dx%d", header.Width, header.Height)
	}
	if header.TimeBaseDen == 0 || header.TimeBaseNum == 0 {
		return nil, fmt.Errorf("timeBase: %d/%d", header.TimeBaseNum, header.TimeBaseDen)
	}
	header.FrameCount = uint32(len(packets))
	header.LargestFrame = 0
	for _, packet := range packets {
		if uint32(len(packet.Payload)) > header.LargestFrame {
			header.LargestFrame = uint32(len(packet.Payload))
		}
	}
	var output bytes.Buffer
	headerPayload := make([]byte, 24)
	copy(headerPayload[0:4], header.Codec)
	binary.LittleEndian.PutUint16(headerPayload[4:6], header.Width)
	binary.LittleEndian.PutUint16(headerPayload[6:8], header.Height)
	binary.LittleEndian.PutUint32(headerPayload[8:12], header.FrameCount)
	binary.LittleEndian.PutUint32(headerPayload[12:16], header.LargestFrame)
	binary.LittleEndian.PutUint32(headerPayload[16:20], header.TimeBaseDen)
	binary.LittleEndian.PutUint32(headerPayload[20:24], header.TimeBaseNum)
	err := writeChunk(&output, movieHeaderTag, headerPayload)
	if err != nil {
		return nil, fmt.Errorf("headerWrite: %w", err)
	}
	audioBlockAlign := uint64(0)
	audioSampleCount := uint64(0)
	if audio != nil {
		if audio.Channels == 0 || audio.SampleRate == 0 {
			return nil, fmt.Errorf("audioFormat: channels %d, rate %d", audio.Channels, audio.SampleRate)
		}
		audioBlockAlign = uint64(audio.Channels) * 2
		if uint64(len(audio.PCM))%audioBlockAlign != 0 {
			return nil, fmt.Errorf("audioAlignment: %d bytes", len(audio.PCM))
		}
		audioSampleCount = uint64(len(audio.PCM)) / audioBlockAlign
		if audioSampleCount > uint64(^uint32(0)) {
			return nil, fmt.Errorf("audioSamples: %d", audioSampleCount)
		}
		audioHeader := encodePCMHeader(audio.Channels, audio.SampleRate, uint32(audioSampleCount))
		err = writeChunk(&output, "SCHl", audioHeader)
		if err != nil {
			return nil, fmt.Errorf("audioHeaderWrite: %w", err)
		}
		blockCount := uint32(len(packets))
		lastFrameSample := uint64(len(packets)) * uint64(audio.SampleRate) * uint64(header.TimeBaseNum) / uint64(header.TimeBaseDen)
		if lastFrameSample < audioSampleCount {
			blockCount++
		}
		countPayload := make([]byte, 4)
		binary.LittleEndian.PutUint32(countPayload, blockCount)
		err = writeChunk(&output, "SCCl", countPayload)
		if err != nil {
			return nil, fmt.Errorf("audioCountWrite: %w", err)
		}
	}
	for index, packet := range packets {
		if len(packet.Payload) == 0 {
			return nil, fmt.Errorf("packet[%d]: empty", index)
		}
		tag := interFrameTag
		if packet.IsKeyframe {
			tag = keyFrameTag
		}
		err = writeChunk(&output, tag, packet.Payload)
		if err != nil {
			return nil, fmt.Errorf("packetWrite[%d]: %w", index, err)
		}
		if audio == nil {
			continue
		}
		startSample := uint64(index) * uint64(audio.SampleRate) * uint64(header.TimeBaseNum) / uint64(header.TimeBaseDen)
		endSample := uint64(index+1) * uint64(audio.SampleRate) * uint64(header.TimeBaseNum) / uint64(header.TimeBaseDen)
		if startSample > audioSampleCount {
			startSample = audioSampleCount
		}
		if endSample > audioSampleCount {
			endSample = audioSampleCount
		}
		if endSample == startSample {
			continue
		}
		err = writePCMAudioChunk(&output, audio.PCM, audioBlockAlign, startSample, endSample)
		if err != nil {
			return nil, fmt.Errorf("audioWrite[%d]: %w", index, err)
		}
	}
	if audio != nil {
		startSample := uint64(len(packets)) * uint64(audio.SampleRate) * uint64(header.TimeBaseNum) / uint64(header.TimeBaseDen)
		if startSample < audioSampleCount {
			err = writePCMAudioChunk(&output, audio.PCM, audioBlockAlign, startSample, audioSampleCount)
			if err != nil {
				return nil, fmt.Errorf("audioTailWrite: %w", err)
			}
		}
	}
	return output.Bytes(), nil
}

func encodePCMHeader(channels uint16, sampleRate, sampleCount uint32) []byte {
	payload := []byte{
		'G', 'S', 'T', 'R', 0x01, 0x00, 0x00, 0x00,
		0x00, 0x04, 0x1E, 0x00, 0x00, 0x00,
		0xFD,
		0x80, 0x04, 0x00, 0x00, 0x00, 0x03,
		0x85, 0x04, 0x00, 0x00, 0x00, 0x00,
		0x82, 0x04, 0x00, 0x00, 0x00, 0x00,
		0x83, 0x04, 0x00, 0x00, 0x00, 0x00,
		0x84, 0x04, 0x00, 0x00, 0x00, 0x00,
		0xFF, 0x00, 0x00,
	}
	binary.BigEndian.PutUint32(payload[23:27], sampleCount)
	binary.BigEndian.PutUint32(payload[29:33], uint32(channels))
	binary.BigEndian.PutUint32(payload[41:45], sampleRate)
	return payload
}

func writePCMAudioChunk(output *bytes.Buffer, pcm []byte, blockAlign, startSample, endSample uint64) error {
	sampleCount := endSample - startSample
	if sampleCount > uint64(^uint32(0)) {
		return fmt.Errorf("sampleCount: %d", sampleCount)
	}
	startByte := startSample * blockAlign
	endByte := endSample * blockAlign
	payload := make([]byte, 4+endByte-startByte)
	binary.BigEndian.PutUint32(payload[0:4], uint32(sampleCount))
	copy(payload[4:], pcm[startByte:endByte])
	err := writeChunk(output, "SCDl", payload)
	if err != nil {
		return fmt.Errorf("chunkWrite: %w", err)
	}
	return nil
}

func writeChunk(output *bytes.Buffer, tag string, payload []byte) error {
	if len(tag) != 4 {
		return fmt.Errorf("tag: %q", tag)
	}
	chunkSize := uint64(len(payload)) + 8
	if chunkSize > uint64(^uint32(0)) {
		return fmt.Errorf("size: %d", chunkSize)
	}
	_, err := output.WriteString(tag)
	if err != nil {
		return fmt.Errorf("tagWrite: %w", err)
	}
	err = binary.Write(output, binary.LittleEndian, uint32(chunkSize))
	if err != nil {
		return fmt.Errorf("sizeWrite: %w", err)
	}
	_, err = output.Write(payload)
	if err != nil {
		return fmt.Errorf("payloadWrite: %w", err)
	}
	return nil
}

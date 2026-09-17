package movie

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	AVIFormatVP60 = "VP60"
	AVIFormatI420 = "I420"
	AVIFormatDIB  = "DIB "
)

// AVI is the editable video boundary used by a MOVIE definition.
type AVI struct {
	Format      string
	Width       uint32
	Height      uint32
	TimeBaseNum uint32
	TimeBaseDen uint32
	Frames      []AVIFrame
	Audio       *AVIAudio
}

// AVIFrame is one compressed VP6 packet or one planar I420 image.
type AVIFrame struct {
	IsKeyframe bool
	Payload    []byte
}

// AVIAudio is interleaved signed PCM16LE carried by the AVI audio stream.
type AVIAudio struct {
	Channels   uint16
	SampleRate uint32
	PCM        []byte
}

type aviIndexEntry struct {
	tag    string
	flags  uint32
	offset uint32
	size   uint32
}

// EncodeAVI writes a single-video-stream RIFF AVI. VP60 preserves package
// packets exactly; I420 is the uncompressed replacement-video profile.
func EncodeAVI(document *AVI) ([]byte, error) {
	if document == nil {
		return nil, errors.New("nil document")
	}
	if document.Format != AVIFormatVP60 && document.Format != AVIFormatI420 && document.Format != AVIFormatDIB {
		return nil, fmt.Errorf("format: unsupported %q", document.Format)
	}
	if document.Width == 0 || document.Height == 0 || document.Width > 0x7FFF || document.Height > 0x7FFF {
		return nil, fmt.Errorf("geometry: %dx%d", document.Width, document.Height)
	}
	if document.TimeBaseNum == 0 || document.TimeBaseDen == 0 {
		return nil, fmt.Errorf("timeBase: %d/%d", document.TimeBaseNum, document.TimeBaseDen)
	}
	if len(document.Frames) == 0 {
		return nil, errors.New("frames: empty")
	}
	frameSize := uint32(0)
	if document.Format == AVIFormatI420 {
		if document.Width%2 != 0 || document.Height%2 != 0 {
			return nil, fmt.Errorf("i420Geometry: %dx%d is not even", document.Width, document.Height)
		}
		frameSize = document.Width * document.Height * 3 / 2
	} else if document.Format == AVIFormatDIB {
		rowSize := (uint64(document.Width)*3 + 3) &^ 3
		dibSize := rowSize * uint64(document.Height)
		if dibSize > uint64(^uint32(0)) {
			return nil, fmt.Errorf("dibSize: %d", dibSize)
		}
		frameSize = uint32(dibSize)
	}
	largestFrame := uint32(0)
	for index, frame := range document.Frames {
		if len(frame.Payload) == 0 || uint64(len(frame.Payload)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("frameSize[%d]: %d", index, len(frame.Payload))
		}
		if frameSize != 0 && len(frame.Payload) != int(frameSize) {
			return nil, fmt.Errorf("i420Size[%d]: got %d, want %d", index, len(frame.Payload), frameSize)
		}
		if uint32(len(frame.Payload)) > largestFrame {
			largestFrame = uint32(len(frame.Payload))
		}
	}
	blockAlign := uint16(0)
	if document.Audio != nil {
		if document.Audio.Channels == 0 || document.Audio.SampleRate == 0 {
			return nil, fmt.Errorf("audioFormat: channels %d, rate %d", document.Audio.Channels, document.Audio.SampleRate)
		}
		blockAlign = document.Audio.Channels * 2
		if len(document.Audio.PCM)%int(blockAlign) != 0 {
			return nil, fmt.Errorf("audioAlignment: %d bytes for %d channels", len(document.Audio.PCM), document.Audio.Channels)
		}
	}

	var headerList bytes.Buffer
	mainHeader := make([]byte, 56)
	microseconds := uint64(document.TimeBaseNum) * 1_000_000 / uint64(document.TimeBaseDen)
	if microseconds > uint64(^uint32(0)) {
		return nil, fmt.Errorf("frameDuration: %d", microseconds)
	}
	binary.LittleEndian.PutUint32(mainHeader[0:4], uint32(microseconds))
	binary.LittleEndian.PutUint32(mainHeader[4:8], largestFrame)
	binary.LittleEndian.PutUint32(mainHeader[12:16], 0x10)
	binary.LittleEndian.PutUint32(mainHeader[16:20], uint32(len(document.Frames)))
	streamCount := uint32(1)
	if document.Audio != nil {
		streamCount++
	}
	binary.LittleEndian.PutUint32(mainHeader[24:28], streamCount)
	binary.LittleEndian.PutUint32(mainHeader[28:32], largestFrame)
	binary.LittleEndian.PutUint32(mainHeader[32:36], document.Width)
	binary.LittleEndian.PutUint32(mainHeader[36:40], document.Height)
	err := writeRIFFChunk(&headerList, "avih", mainHeader)
	if err != nil {
		return nil, fmt.Errorf("mainHeader: %w", err)
	}

	var streamList bytes.Buffer
	streamHeader := make([]byte, 56)
	copy(streamHeader[0:4], "vids")
	copy(streamHeader[4:8], document.Format)
	binary.LittleEndian.PutUint32(streamHeader[20:24], document.TimeBaseNum)
	binary.LittleEndian.PutUint32(streamHeader[24:28], document.TimeBaseDen)
	binary.LittleEndian.PutUint32(streamHeader[32:36], uint32(len(document.Frames)))
	binary.LittleEndian.PutUint32(streamHeader[36:40], largestFrame)
	binary.LittleEndian.PutUint32(streamHeader[40:44], ^uint32(0))
	binary.LittleEndian.PutUint16(streamHeader[48:50], uint16(document.Width))
	binary.LittleEndian.PutUint16(streamHeader[50:52], uint16(document.Height))
	err = writeRIFFChunk(&streamList, "strh", streamHeader)
	if err != nil {
		return nil, fmt.Errorf("streamHeader: %w", err)
	}
	bitmapHeader := make([]byte, 40)
	binary.LittleEndian.PutUint32(bitmapHeader[0:4], 40)
	binary.LittleEndian.PutUint32(bitmapHeader[4:8], document.Width)
	binary.LittleEndian.PutUint32(bitmapHeader[8:12], document.Height)
	binary.LittleEndian.PutUint16(bitmapHeader[12:14], 1)
	if document.Format == AVIFormatI420 {
		binary.LittleEndian.PutUint16(bitmapHeader[14:16], 12)
		copy(bitmapHeader[16:20], document.Format)
	} else if document.Format == AVIFormatDIB {
		binary.LittleEndian.PutUint16(bitmapHeader[14:16], 24)
	} else {
		binary.LittleEndian.PutUint16(bitmapHeader[14:16], 24)
		copy(bitmapHeader[16:20], document.Format)
	}
	binary.LittleEndian.PutUint32(bitmapHeader[20:24], frameSize)
	err = writeRIFFChunk(&streamList, "strf", bitmapHeader)
	if err != nil {
		return nil, fmt.Errorf("streamFormat: %w", err)
	}
	err = writeRIFFList(&headerList, "strl", streamList.Bytes())
	if err != nil {
		return nil, fmt.Errorf("streamList: %w", err)
	}
	if document.Audio != nil {
		var audioList bytes.Buffer
		audioHeader := make([]byte, 56)
		copy(audioHeader[0:4], "auds")
		binary.LittleEndian.PutUint32(audioHeader[20:24], uint32(blockAlign))
		binary.LittleEndian.PutUint32(audioHeader[24:28], document.Audio.SampleRate*uint32(blockAlign))
		binary.LittleEndian.PutUint32(audioHeader[32:36], uint32(len(document.Audio.PCM))/uint32(blockAlign))
		binary.LittleEndian.PutUint32(audioHeader[36:40], uint32(blockAlign)*document.Audio.SampleRate)
		binary.LittleEndian.PutUint32(audioHeader[40:44], ^uint32(0))
		binary.LittleEndian.PutUint32(audioHeader[44:48], uint32(blockAlign))
		err = writeRIFFChunk(&audioList, "strh", audioHeader)
		if err != nil {
			return nil, fmt.Errorf("audioHeader: %w", err)
		}
		waveHeader := make([]byte, 16)
		binary.LittleEndian.PutUint16(waveHeader[0:2], 1)
		binary.LittleEndian.PutUint16(waveHeader[2:4], document.Audio.Channels)
		binary.LittleEndian.PutUint32(waveHeader[4:8], document.Audio.SampleRate)
		binary.LittleEndian.PutUint32(waveHeader[8:12], document.Audio.SampleRate*uint32(blockAlign))
		binary.LittleEndian.PutUint16(waveHeader[12:14], blockAlign)
		binary.LittleEndian.PutUint16(waveHeader[14:16], 16)
		err = writeRIFFChunk(&audioList, "strf", waveHeader)
		if err != nil {
			return nil, fmt.Errorf("audioFormat: %w", err)
		}
		err = writeRIFFList(&headerList, "strl", audioList.Bytes())
		if err != nil {
			return nil, fmt.Errorf("audioList: %w", err)
		}
	}

	var movieList bytes.Buffer
	indexes := make([]aviIndexEntry, 0, len(document.Frames))
	for index, frame := range document.Frames {
		offset := uint64(movieList.Len()) + 4
		if offset > uint64(^uint32(0)) {
			return nil, fmt.Errorf("frameOffset[%d]: %d", index, offset)
		}
		err = writeRIFFChunk(&movieList, "00dc", frame.Payload)
		if err != nil {
			return nil, fmt.Errorf("frameWrite[%d]: %w", index, err)
		}
		flags := uint32(0)
		if frame.IsKeyframe || document.Format == AVIFormatI420 {
			flags = 0x10
		}
		indexes = append(indexes, aviIndexEntry{tag: "00dc", flags: flags, offset: uint32(offset), size: uint32(len(frame.Payload))})
		if document.Audio == nil {
			continue
		}
		startSample := uint64(index) * uint64(document.Audio.SampleRate) * uint64(document.TimeBaseNum) / uint64(document.TimeBaseDen)
		endSample := uint64(index+1) * uint64(document.Audio.SampleRate) * uint64(document.TimeBaseNum) / uint64(document.TimeBaseDen)
		totalSamples := uint64(len(document.Audio.PCM)) / uint64(blockAlign)
		if startSample > totalSamples {
			startSample = totalSamples
		}
		if endSample > totalSamples {
			endSample = totalSamples
		}
		if endSample == startSample {
			continue
		}
		audioOffset := uint64(movieList.Len()) + 4
		if audioOffset > uint64(^uint32(0)) {
			return nil, fmt.Errorf("audioOffset[%d]: %d", index, audioOffset)
		}
		audioPayload := document.Audio.PCM[startSample*uint64(blockAlign) : endSample*uint64(blockAlign)]
		err = writeRIFFChunk(&movieList, "01wb", audioPayload)
		if err != nil {
			return nil, fmt.Errorf("audioWrite[%d]: %w", index, err)
		}
		indexes = append(indexes, aviIndexEntry{tag: "01wb", flags: 0x10, offset: uint32(audioOffset), size: uint32(len(audioPayload))})
	}
	if document.Audio != nil {
		writtenSamples := uint64(len(document.Frames)) * uint64(document.Audio.SampleRate) * uint64(document.TimeBaseNum) / uint64(document.TimeBaseDen)
		totalSamples := uint64(len(document.Audio.PCM)) / uint64(blockAlign)
		if writtenSamples < totalSamples {
			audioOffset := uint64(movieList.Len()) + 4
			audioPayload := document.Audio.PCM[writtenSamples*uint64(blockAlign):]
			err = writeRIFFChunk(&movieList, "01wb", audioPayload)
			if err != nil {
				return nil, fmt.Errorf("audioTail: %w", err)
			}
			indexes = append(indexes, aviIndexEntry{tag: "01wb", flags: 0x10, offset: uint32(audioOffset), size: uint32(len(audioPayload))})
		}
	}
	var indexPayload bytes.Buffer
	for _, index := range indexes {
		_, _ = indexPayload.WriteString(index.tag)
		_ = binary.Write(&indexPayload, binary.LittleEndian, index.flags)
		_ = binary.Write(&indexPayload, binary.LittleEndian, index.offset)
		_ = binary.Write(&indexPayload, binary.LittleEndian, index.size)
	}

	var body bytes.Buffer
	_, _ = body.WriteString("AVI ")
	err = writeRIFFList(&body, "hdrl", headerList.Bytes())
	if err != nil {
		return nil, fmt.Errorf("headerList: %w", err)
	}
	err = writeRIFFList(&body, "movi", movieList.Bytes())
	if err != nil {
		return nil, fmt.Errorf("movieList: %w", err)
	}
	err = writeRIFFChunk(&body, "idx1", indexPayload.Bytes())
	if err != nil {
		return nil, fmt.Errorf("indexWrite: %w", err)
	}
	if uint64(body.Len()) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("riffSize: %d exceeds classic AVI", body.Len())
	}
	var output bytes.Buffer
	_, _ = output.WriteString("RIFF")
	_ = binary.Write(&output, binary.LittleEndian, uint32(body.Len()))
	_, _ = output.Write(body.Bytes())
	return output.Bytes(), nil
}

// DecodeAVI reads the video stream from an ordinary VP60 or I420 AVI.
func DecodeAVI(payload []byte) (*AVI, error) {
	if len(payload) < 12 || string(payload[0:4]) != "RIFF" || string(payload[8:12]) != "AVI " {
		return nil, errors.New("riffHeader: not AVI")
	}
	riffEnd := uint64(binary.LittleEndian.Uint32(payload[4:8])) + 8
	if riffEnd > uint64(len(payload)) {
		return nil, fmt.Errorf("riffBounds: end %d exceeds %d", riffEnd, len(payload))
	}
	document := &AVI{}
	streamIndex := -1
	videoStream := -1
	audioStream := -1
	err := walkRIFF(payload[12:riffEnd], func(tag, listType string, chunk []byte) error {
		if tag == "strh" {
			streamIndex++
			if len(chunk) < 40 {
				return fmt.Errorf("streamHeader[%d]: %d bytes", streamIndex, len(chunk))
			}
			streamType := string(chunk[0:4])
			if streamType == "auds" && audioStream < 0 {
				audioStream = streamIndex
				return nil
			}
			if streamType != "vids" || videoStream >= 0 {
				return nil
			}
			format := string(chunk[4:8])
			if format != AVIFormatVP60 && format != AVIFormatI420 && format != AVIFormatDIB && format != "\x00\x00\x00\x00" {
				return fmt.Errorf("videoFormat: unsupported %q", format)
			}
			if format == "\x00\x00\x00\x00" {
				format = AVIFormatDIB
			}
			document.Format = format
			document.TimeBaseNum = binary.LittleEndian.Uint32(chunk[20:24])
			document.TimeBaseDen = binary.LittleEndian.Uint32(chunk[24:28])
			videoStream = streamIndex
			return nil
		}
		if tag == "strf" && streamIndex == videoStream {
			if len(chunk) < 20 {
				return fmt.Errorf("bitmapHeader: %d bytes", len(chunk))
			}
			document.Width = binary.LittleEndian.Uint32(chunk[4:8])
			document.Height = binary.LittleEndian.Uint32(chunk[8:12])
			return nil
		}
		if tag == "strf" && streamIndex == audioStream {
			if len(chunk) < 16 || binary.LittleEndian.Uint16(chunk[0:2]) != 1 || binary.LittleEndian.Uint16(chunk[14:16]) != 16 {
				return errors.New("audioFormat: only PCM16 is supported")
			}
			document.Audio = &AVIAudio{
				Channels:   binary.LittleEndian.Uint16(chunk[2:4]),
				SampleRate: binary.LittleEndian.Uint32(chunk[4:8]),
			}
			return nil
		}
		if listType != "movi" || len(tag) != 4 {
			return nil
		}
		parsedStream, parseErr := parseAVIStream(tag[0:2])
		if parseErr != nil {
			return nil
		}
		if parsedStream == videoStream && (tag[2:] == "dc" || tag[2:] == "db") {
			document.Frames = append(document.Frames, AVIFrame{Payload: append([]byte(nil), chunk...)})
		}
		if parsedStream == audioStream && tag[2:] == "wb" && document.Audio != nil {
			document.Audio.PCM = append(document.Audio.PCM, chunk...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("riffRead: %w", err)
	}
	if videoStream < 0 || document.Width == 0 || document.Height == 0 || document.TimeBaseNum == 0 || document.TimeBaseDen == 0 {
		return nil, errors.New("videoStream: incomplete")
	}
	if len(document.Frames) == 0 {
		return nil, errors.New("videoFrames: empty")
	}
	if document.Audio != nil {
		blockAlign := int(document.Audio.Channels) * 2
		if blockAlign == 0 || len(document.Audio.PCM)%blockAlign != 0 {
			return nil, fmt.Errorf("audioAlignment: %d bytes", len(document.Audio.PCM))
		}
	}
	if document.Format == AVIFormatI420 {
		frameSize := uint64(document.Width) * uint64(document.Height) * 3 / 2
		for index, frame := range document.Frames {
			if uint64(len(frame.Payload)) != frameSize {
				return nil, fmt.Errorf("i420Size[%d]: got %d, want %d", index, len(frame.Payload), frameSize)
			}
			document.Frames[index].IsKeyframe = true
		}
	} else if document.Format == AVIFormatDIB {
		rowSize := (uint64(document.Width)*3 + 3) &^ 3
		frameSize := rowSize * uint64(document.Height)
		for index, frame := range document.Frames {
			if uint64(len(frame.Payload)) != frameSize {
				return nil, fmt.Errorf("dibSize[%d]: got %d, want %d", index, len(frame.Payload), frameSize)
			}
			document.Frames[index].IsKeyframe = true
		}
	} else {
		for index, frame := range document.Frames {
			document.Frames[index].IsKeyframe = frame.Payload[0]&0x80 == 0
		}
	}
	return document, nil
}

// DIBFramesFromBGR24 converts top-down packed FFmpeg BGR24 frames into
// bottom-up DWORD-aligned AVI DIB frames.
func DIBFramesFromBGR24(payload []byte, width, height uint32, frameCount int) ([]AVIFrame, error) {
	if width == 0 || height == 0 || frameCount <= 0 {
		return nil, fmt.Errorf("geometry: %dx%d frames %d", width, height, frameCount)
	}
	sourceRowSize := uint64(width) * 3
	sourceFrameSize := sourceRowSize * uint64(height)
	expectedSize := sourceFrameSize * uint64(frameCount)
	if uint64(len(payload)) != expectedSize {
		return nil, fmt.Errorf("payloadSize: got %d, want %d", len(payload), expectedSize)
	}
	destinationRowSize := (sourceRowSize + 3) &^ 3
	destinationFrameSize := destinationRowSize * uint64(height)
	if destinationFrameSize > uint64(^uint32(0)) {
		return nil, fmt.Errorf("frameSize: %d", destinationFrameSize)
	}
	frames := make([]AVIFrame, frameCount)
	for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
		frame := make([]byte, int(destinationFrameSize))
		sourceFrameOffset := uint64(frameIndex) * sourceFrameSize
		for row := uint32(0); row < height; row++ {
			sourceOffset := sourceFrameOffset + uint64(row)*sourceRowSize
			destinationOffset := uint64(height-1-row) * destinationRowSize
			copy(frame[destinationOffset:destinationOffset+sourceRowSize], payload[sourceOffset:sourceOffset+sourceRowSize])
		}
		frames[frameIndex] = AVIFrame{IsKeyframe: true, Payload: frame}
	}
	return frames, nil
}

func walkRIFF(payload []byte, visit func(tag, listType string, chunk []byte) error) error {
	return walkRIFFList(payload, "", visit)
}

func walkRIFFList(payload []byte, listType string, visit func(tag, listType string, chunk []byte) error) error {
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 8 {
			return fmt.Errorf("chunkHeader: %d trailing bytes", len(payload)-offset)
		}
		tag := string(payload[offset : offset+4])
		size := binary.LittleEndian.Uint32(payload[offset+4 : offset+8])
		chunkEnd := uint64(offset) + 8 + uint64(size)
		paddedEnd := chunkEnd + uint64(size&1)
		if paddedEnd > uint64(len(payload)) {
			return fmt.Errorf("chunkBounds[%s]: end %d exceeds %d", tag, paddedEnd, len(payload))
		}
		chunk := payload[offset+8 : int(chunkEnd)]
		if tag == "LIST" || tag == "RIFF" {
			if len(chunk) < 4 {
				return fmt.Errorf("listType: %d bytes", len(chunk))
			}
			err := walkRIFFList(chunk[4:], string(chunk[0:4]), visit)
			if err != nil {
				return fmt.Errorf("list[%s]: %w", string(chunk[0:4]), err)
			}
		} else {
			err := visit(tag, listType, chunk)
			if err != nil {
				return err
			}
		}
		offset = int(paddedEnd)
	}
	return nil
}

func parseAVIStream(raw string) (int, error) {
	if len(raw) != 2 || raw[0] < '0' || raw[0] > '9' || raw[1] < '0' || raw[1] > '9' {
		return 0, fmt.Errorf("stream: %q", raw)
	}
	return int(raw[0]-'0')*10 + int(raw[1]-'0'), nil
}

func writeRIFFList(output *bytes.Buffer, listType string, payload []byte) error {
	if len(listType) != 4 {
		return fmt.Errorf("listType: %q", listType)
	}
	contents := make([]byte, 4+len(payload))
	copy(contents, listType)
	copy(contents[4:], payload)
	return writeRIFFChunk(output, "LIST", contents)
}

func writeRIFFChunk(output *bytes.Buffer, tag string, payload []byte) error {
	if len(tag) != 4 {
		return fmt.Errorf("tag: %q", tag)
	}
	if uint64(len(payload)) > uint64(^uint32(0)) {
		return fmt.Errorf("size: %d", len(payload))
	}
	_, err := io.WriteString(output, tag)
	if err != nil {
		return fmt.Errorf("tagWrite: %w", err)
	}
	err = binary.Write(output, binary.LittleEndian, uint32(len(payload)))
	if err != nil {
		return fmt.Errorf("sizeWrite: %w", err)
	}
	_, err = output.Write(payload)
	if err != nil {
		return fmt.Errorf("payloadWrite: %w", err)
	}
	if len(payload)&1 != 0 {
		err = output.WriteByte(0)
		if err != nil {
			return fmt.Errorf("paddingWrite: %w", err)
		}
	}
	return nil
}

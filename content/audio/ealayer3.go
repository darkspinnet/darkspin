package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	mp3 "github.com/darkspinnet/mp3"
)

type eaLayer3Frame struct {
	payload       []byte
	versionIndex  uint32
	sampleIndex   uint32
	channelMode   uint32
	modeExtension uint32
	granuleIndex  uint32
	scfsi         [2]uint32
	mainDataSizes [2]uint32
	others1       [2]uint32
	others2       [2]uint32
	dataOffset    int
	frameSize     int
	pcmSamples    int
	pcmPayload    []byte
	channels      int
	sampleRate    int
	isMPEG1       bool
}

type eaLayer3OutputFrame struct {
	pcmPrefix      []byte
	discardSamples int
	frameSamples   int
	isSegmentStart bool
}

type eaLayer3MPEGSource struct {
	first          eaLayer3Frame
	second         eaLayer3Frame
	isSegmentStart bool
}

func decodeEALayer3V1(blocks []audioBlock, header Header) ([]byte, error) {
	sources := make([]eaLayer3MPEGSource, 0)
	for blockIndex, block := range blocks {
		producedSamples := 0
		if block.isSegmentStart {
			producedSamples -= 529 + 576
		}
		offset := 0
		for frameIndex := 0; producedSamples < int(block.samples); frameIndex++ {
			frame, err := parseEALayer3V1Frame(block.payload[offset:], int(header.Channels))
			if err != nil {
				return nil, fmt.Errorf("frame[%d:%d]@%d: %w", blockIndex, frameIndex, offset, err)
			}
			offset += frame.frameSize
			source := eaLayer3MPEGSource{first: frame, isSegmentStart: block.isSegmentStart && frameIndex == 0}
			frameSamples := 576
			if frame.isMPEG1 {
				second, secondErr := parseEALayer3V1Frame(block.payload[offset:], int(header.Channels))
				if secondErr != nil {
					return nil, fmt.Errorf("frame[%d:%d]@%d: %w", blockIndex, frameIndex, offset, secondErr)
				}
				if frame.granuleIndex == second.granuleIndex || frame.channels != second.channels || frame.sampleRate != second.sampleRate {
					return nil, fmt.Errorf("frame[%d:%d]: granule mismatch", blockIndex, frameIndex)
				}
				source.second = second
				offset += second.frameSize
				frameSamples = 1152
			}
			sources = append(sources, source)
			producedSamples += frameSamples
		}
	}
	if len(sources) == 0 {
		return nil, errors.New("framesMissing")
	}
	var mpeg bytes.Buffer
	outputs := make([]eaLayer3OutputFrame, 0, len(sources))
	for sourceIndex, source := range sources {
		mpegFrame, err := rebuildEALayer3MPEGFrame(source.first, source.second)
		if err != nil {
			return nil, fmt.Errorf("frame[%d]: %w", sourceIndex, err)
		}
		_, err = mpeg.Write(mpegFrame)
		if err != nil {
			return nil, fmt.Errorf("mpegWrite[%d]: %w", sourceIndex, err)
		}
		prefix := append([]byte(nil), source.first.pcmPayload...)
		prefix = append(prefix, source.second.pcmPayload...)
		frameSamples := 576
		if source.first.isMPEG1 {
			frameSamples = 1152
		}
		outputs = append(outputs, eaLayer3OutputFrame{
			pcmPrefix:      prefix,
			discardSamples: source.first.pcmSamples + source.second.pcmSamples,
			frameSamples:   frameSamples,
			isSegmentStart: source.isSegmentStart,
		})
	}
	decoder, err := mp3.NewDecoder(bytes.NewReader(mpeg.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("mpegOpen: %w", err)
	}
	decoded, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("mpegDecode: %w", err)
	}
	expectedSize := 0
	for _, output := range outputs {
		expectedSize += output.frameSamples * 4
	}
	if len(decoded) < expectedSize {
		return nil, fmt.Errorf("mpegSize: got %d, want at least %d", len(decoded), expectedSize)
	}
	pcm := make([]byte, 0, len(decoded))
	decodedOffset := 0
	segmentDiscard := 0
	for _, output := range outputs {
		pcm = append(pcm, output.pcmPrefix...)
		if output.isSegmentStart {
			segmentDiscard = 529 + 576
		}
		discardSamples := output.discardSamples + segmentDiscard
		if discardSamples > output.frameSamples {
			segmentDiscard = discardSamples - output.frameSamples
			discardSamples = output.frameSamples
		} else {
			segmentDiscard = 0
		}
		frameSize := output.frameSamples * 4
		framePCM := decoded[decodedOffset : decodedOffset+frameSize]
		decodedOffset += frameSize
		framePCM = framePCM[discardSamples*4:]
		pcm = appendEALayer3PCM(pcm, framePCM, int(header.Channels))
	}
	return pcm, nil
}

func appendEALayer3PCM(destination, stereoPCM []byte, channels int) []byte {
	if channels == 2 {
		return append(destination, stereoPCM...)
	}
	for offset := 0; offset+4 <= len(stereoPCM); offset += 4 {
		destination = append(destination, stereoPCM[offset:offset+2]...)
	}
	return destination
}

func parseEALayer3V1Frame(payload []byte, channels int) (eaLayer3Frame, error) {
	if len(payload) < 2 {
		return eaLayer3Frame{}, io.ErrUnexpectedEOF
	}
	frame := eaLayer3Frame{payload: payload, channels: channels}
	pcmFlag := payload[0]
	if pcmFlag != 0x00 && pcmFlag != 0xEE {
		return eaLayer3Frame{}, fmt.Errorf("flag: 0x%02X", pcmFlag)
	}
	reader := eaBitReader{payload: payload, position: 8}
	err := parseEALayer3Common(&reader, &frame)
	if err != nil {
		return eaLayer3Frame{}, fmt.Errorf("common: %w", err)
	}
	preSize := 1
	if pcmFlag == 0xEE {
		_, err = reader.read(16)
		if err != nil {
			return eaLayer3Frame{}, fmt.Errorf("pcmOffset: %w", err)
		}
		pcmSamples, readErr := reader.read(16)
		if readErr != nil {
			return eaLayer3Frame{}, fmt.Errorf("pcmSamples: %w", readErr)
		}
		_, readErr = reader.read(32)
		if readErr != nil {
			return eaLayer3Frame{}, fmt.Errorf("pcmUnknown: %w", readErr)
		}
		preSize += 8
		frame.pcmSamples = int(pcmSamples)
	}
	// The common portion starts after the one-byte V1 flag and ends before the
	// optional PCM metadata. Its byte size is recovered from the parsed data end.
	commonEnd := reader.position / 8
	if pcmFlag == 0xEE {
		commonEnd -= 8
	}
	commonSize := commonEnd - 1
	frame.frameSize = preSize + commonSize + frame.pcmSamples*channels*2
	if frame.frameSize <= 0 || frame.frameSize > len(payload) {
		return eaLayer3Frame{}, fmt.Errorf("size: got %d, remaining %d", frame.frameSize, len(payload))
	}
	if frame.pcmSamples > 0 {
		pcmOffset := preSize + commonSize
		pcmSize := frame.pcmSamples * channels * 2
		planar := payload[pcmOffset : pcmOffset+pcmSize]
		frame.pcmPayload = interleaveEALayer3PCM(planar, channels, frame.pcmSamples)
	}
	frame.payload = payload[:frame.frameSize]
	return frame, nil
}

func parseEALayer3Common(reader *eaBitReader, frame *eaLayer3Frame) error {
	start := reader.position
	var err error
	frame.versionIndex, err = reader.read(2)
	if err != nil {
		return err
	}
	frame.sampleIndex, err = reader.read(2)
	if err != nil {
		return err
	}
	frame.channelMode, err = reader.read(2)
	if err != nil {
		return err
	}
	frame.modeExtension, err = reader.read(2)
	if err != nil {
		return err
	}
	versionTable := [4]int{3, -1, 2, 1}
	sampleRates := [4][4]int{{11025, 12000, 8000, -1}, {-1, -1, -1, -1}, {22050, 24000, 16000, -1}, {44100, 48000, 32000, -1}}
	channelCounts := [4]int{2, 2, 2, 1}
	version := versionTable[frame.versionIndex]
	frame.sampleRate = sampleRates[frame.versionIndex][frame.sampleIndex]
	frame.channels = channelCounts[frame.channelMode]
	frame.isMPEG1 = version == 1
	if version < 0 || frame.sampleRate < 0 {
		return errors.New("headerUnsupported")
	}
	frame.granuleIndex, err = reader.read(1)
	if err != nil {
		return err
	}
	if frame.isMPEG1 && frame.granuleIndex == 1 {
		for channel := 0; channel < frame.channels; channel++ {
			frame.scfsi[channel], err = reader.read(4)
			if err != nil {
				return err
			}
		}
	}
	otherBits := 19
	if frame.isMPEG1 {
		otherBits = 15
	}
	dataBits := 0
	for channel := 0; channel < frame.channels; channel++ {
		frame.mainDataSizes[channel], err = reader.read(12)
		if err != nil {
			return err
		}
		frame.others1[channel], err = reader.read(32)
		if err != nil {
			return err
		}
		frame.others2[channel], err = reader.read(otherBits)
		if err != nil {
			return err
		}
		dataBits += int(frame.mainDataSizes[channel])
	}
	frame.dataOffset = reader.position
	baseBits := reader.position - start
	paddingBits := (8 - (baseBits+dataBits)%8) % 8
	err = reader.skip(dataBits + paddingBits)
	if err != nil {
		return err
	}
	return nil
}

func interleaveEALayer3PCM(planar []byte, channels, samples int) []byte {
	if channels == 1 {
		pcm := append([]byte(nil), planar...)
		for offset := 0; offset+2 <= len(pcm); offset += 2 {
			pcm[offset], pcm[offset+1] = pcm[offset+1], pcm[offset]
		}
		return pcm
	}
	pcm := make([]byte, 0, len(planar))
	for sample := 0; sample < samples; sample++ {
		for channel := 0; channel < channels; channel++ {
			offset := (channel*samples + sample) * 2
			pcm = append(pcm, planar[offset+1], planar[offset])
		}
	}
	return pcm
}

func rebuildEALayer3MPEGFrame(first, second eaLayer3Frame) ([]byte, error) {
	frameSize := 72 * 320000 / first.sampleRate
	if first.isMPEG1 {
		frameSize = 144 * 640000 / first.sampleRate
	}
	writer := eaBitWriter{payload: make([]byte, frameSize)}
	writes := [][2]uint32{
		{11, 0x7FF}, {2, first.versionIndex}, {2, 1}, {1, 1}, {4, 0},
		{2, first.sampleIndex}, {1, 0}, {1, 0}, {2, first.channelMode},
		{2, first.modeExtension}, {1, 1}, {1, 1}, {2, 0},
	}
	for _, field := range writes {
		if err := writer.write(int(field[0]), field[1]); err != nil {
			return nil, err
		}
	}
	if first.isMPEG1 {
		privateBits := 3
		if first.channels == 1 {
			privateBits = 5
		}
		if err := writer.write(9, 0); err != nil {
			return nil, err
		}
		if err := writer.write(privateBits, 0); err != nil {
			return nil, err
		}
		for channel := 0; channel < first.channels; channel++ {
			if err := writer.write(4, second.scfsi[channel]); err != nil {
				return nil, err
			}
		}
		for _, frame := range []eaLayer3Frame{first, second} {
			for channel := 0; channel < frame.channels; channel++ {
				if err := writer.write(12, frame.mainDataSizes[channel]); err != nil {
					return nil, err
				}
				if err := writer.write(32, frame.others1[channel]); err != nil {
					return nil, err
				}
				if err := writer.write(15, frame.others2[channel]); err != nil {
					return nil, err
				}
			}
		}
	} else {
		privateBits := 2
		if first.channels == 1 {
			privateBits = 1
		}
		if err := writer.write(8, 0); err != nil {
			return nil, err
		}
		if err := writer.write(privateBits, 0); err != nil {
			return nil, err
		}
		for channel := 0; channel < first.channels; channel++ {
			if err := writer.write(12, first.mainDataSizes[channel]); err != nil {
				return nil, err
			}
			if err := writer.write(32, first.others1[channel]); err != nil {
				return nil, err
			}
			if err := writer.write(19, first.others2[channel]); err != nil {
				return nil, err
			}
		}
	}
	frames := []eaLayer3Frame{first}
	if first.isMPEG1 {
		frames = append(frames, second)
	}
	for _, frame := range frames {
		reader := eaBitReader{payload: frame.payload, position: frame.dataOffset}
		for channel := 0; channel < frame.channels; channel++ {
			for bit := uint32(0); bit < frame.mainDataSizes[channel]; bit++ {
				current, err := reader.read(1)
				if err != nil {
					return nil, err
				}
				if err := writer.write(1, current); err != nil {
					return nil, err
				}
			}
		}
	}
	return writer.payload, nil
}

type eaBitReader struct {
	payload  []byte
	position int
}

func (e *eaBitReader) read(count int) (uint32, error) {
	if count < 0 || count > 32 || e.position+count > len(e.payload)*8 {
		return 0, io.ErrUnexpectedEOF
	}
	var result uint32
	for bit := 0; bit < count; bit++ {
		byteIndex := e.position / 8
		bitIndex := 7 - e.position%8
		result = result<<1 | uint32((e.payload[byteIndex]>>bitIndex)&1)
		e.position++
	}
	return result, nil
}

func (e *eaBitReader) skip(count int) error {
	if count < 0 || e.position+count > len(e.payload)*8 {
		return io.ErrUnexpectedEOF
	}
	e.position += count
	return nil
}

type eaBitWriter struct {
	payload  []byte
	position int
}

func (e *eaBitWriter) write(count int, field uint32) error {
	if count < 0 || count > 32 || e.position+count > len(e.payload)*8 {
		return fmt.Errorf("frameOverflow: bit %d + %d exceeds %d", e.position, count, len(e.payload)*8)
	}
	for bit := count - 1; bit >= 0; bit-- {
		if field&(uint32(1)<<bit) != 0 {
			e.payload[e.position/8] |= 1 << (7 - e.position%8)
		}
		e.position++
	}
	return nil
}

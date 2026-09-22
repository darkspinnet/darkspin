package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DecodeSNR converts the EA AudioCore codecs used by Spore and Darkspore to
// canonical interleaved PCM16LE. snsPayload is required for streamed assets.
func DecodeSNR(snrPayload, snsPayload []byte) (WAV, error) {
	header, err := DecodeResolvedHeader(snrPayload)
	if err != nil {
		return WAV{}, fmt.Errorf("headerDecode: %w", err)
	}
	headerSize, err := audioHeaderSize(snrPayload, header)
	if err != nil {
		return WAV{}, fmt.Errorf("headerSize: %w", err)
	}
	streamPayload := snrPayload[headerSize:]
	if header.Storage == "STREAM" {
		if len(snsPayload) == 0 {
			return WAV{}, errors.New("streamMissing")
		}
		streamPayload = snsPayload
	}
	if header.Storage == "GIGASAMPLE" {
		if len(snsPayload) == 0 {
			return WAV{}, errors.New("streamMissing")
		}
		streamPayload = append(append([]byte(nil), streamPayload...), snsPayload...)
	}
	blocks, err := decodeAudioBlocks(streamPayload, header.Version)
	if err != nil {
		return WAV{}, fmt.Errorf("blocksDecode: %w", err)
	}
	var pcm []byte
	switch header.Codec {
	case "PCM16BE":
		pcm, err = decodePCM16BEBlocks(blocks, header.Channels)
	case "XAS0":
		pcm, err = decodeXAS0Blocks(blocks, header.Channels)
	case "XAS1":
		pcm, err = decodeXAS1Blocks(blocks, header.Channels)
	case "EALAYER3_V1":
		pcm, err = decodeEALayer3V1(blocks, header)
	default:
		return WAV{}, fmt.Errorf("codecUnsupported: %q", header.Codec)
	}
	if err != nil {
		return WAV{}, fmt.Errorf("%sDecode: %w", header.Codec, err)
	}
	expectedSize := uint64(header.SampleCount) * uint64(header.Channels) * 2
	if uint64(len(pcm)) < expectedSize {
		return WAV{}, fmt.Errorf("sampleCount: decoded %d bytes, want %d", len(pcm), expectedSize)
	}
	pcm = pcm[:expectedSize]
	return WAV{Channels: uint16(header.Channels), SampleRate: header.SampleRate, PCM: pcm}, nil
}

// DecodeResolvedHeader reads an SNR header and recognizes Spore's XAS0
// resources, which use codec nibble zero and channel-interleaved frames.
// Unknown NONE and RESERVED payloads remain raw.
func DecodeResolvedHeader(payload []byte) (Header, error) {
	header, err := DecodeHeader(payload)
	if err != nil {
		return Header{}, fmt.Errorf("headerDecode: %w", err)
	}
	if header.Codec != "NONE" || header.Storage != "RAM" {
		return header, nil
	}
	headerSize, sizeErr := audioHeaderSize(payload, header)
	if sizeErr != nil {
		return header, nil
	}
	blocks, blockErr := decodeAudioBlocks(payload[headerSize:], header.Version)
	if blockErr != nil {
		return header, nil
	}
	if areXAS0Blocks(blocks, header.Channels) {
		header.Codec = "XAS0"
	}
	return header, nil
}

func areXAS0Blocks(blocks []audioBlock, channels uint8) bool {
	const frameSize = 0x13
	const samplesPerFrame = 32
	if channels == 0 {
		return false
	}
	for _, block := range blocks {
		if block.samples == 0 {
			return false
		}
		frameCount := (uint64(block.samples) + samplesPerFrame - 1) / samplesPerFrame
		minimumSize := (frameCount - 1) * frameSize * uint64(channels)
		expectedSize := frameCount * frameSize * uint64(channels)
		payloadSize := uint64(len(block.payload))
		if payloadSize <= minimumSize || payloadSize > expectedSize {
			return false
		}
		frameGroupSize := frameSize * int(channels)
		for frameOffset := 0; frameOffset < len(block.payload); frameOffset += frameGroupSize {
			for channel := 0; channel < int(channels); channel++ {
				headerOffset := frameOffset + channel*2
				if headerOffset >= len(block.payload) {
					continue
				}
				if block.payload[headerOffset]&0x0F > 3 {
					return false
				}
			}
		}
	}
	return len(blocks) != 0
}

type audioBlock struct {
	samples        uint32
	payload        []byte
	isSegmentStart bool
}

func audioHeaderSize(payload []byte, header Header) (int, error) {
	headerSize := 8
	if header.IsLooped {
		headerSize += 4
		if header.Storage == "STREAM" {
			headerSize += 4
		}
	}
	if header.Storage == "GIGASAMPLE" {
		prefetchOffset := 8
		loopStart := uint32(0)
		if header.IsLooped {
			if len(payload) < 12 {
				return 0, fmt.Errorf("got %d bytes, want at least 12", len(payload))
			}
			loopStart = binary.BigEndian.Uint32(payload[8:12])
			prefetchOffset = 12
		}
		if len(payload) < prefetchOffset+4 {
			return 0, fmt.Errorf("got %d bytes, want at least %d", len(payload), prefetchOffset+4)
		}
		prefetchSamples := binary.BigEndian.Uint32(payload[prefetchOffset : prefetchOffset+4])
		headerSize += 4
		if header.IsLooped && loopStart >= prefetchSamples {
			headerSize += 4
		}
	}
	if headerSize > len(payload) {
		return 0, fmt.Errorf("got %d bytes, want at least %d", len(payload), headerSize)
	}
	return headerSize, nil
}

func decodeAudioBlocks(payload []byte, version uint8) ([]audioBlock, error) {
	blocks := make([]audioBlock, 0, 1)
	isSegmentStart := true
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 4 {
			return nil, fmt.Errorf("header[%d]: %d trailing bytes", len(blocks), len(payload)-offset)
		}
		word := binary.BigEndian.Uint32(payload[offset : offset+4])
		blockID := byte(word >> 24)
		blockSize := int(word & 0x00FFFFFF)
		if blockSize < 4 || blockSize > len(payload)-offset {
			return nil, fmt.Errorf("size[%d]: %d at %d", len(blocks), blockSize, offset)
		}
		if version == 1 && blockID == 0x48 {
			offset += blockSize
			continue
		}
		if version == 1 && blockID == 0x45 {
			break
		}
		isData := version == 0 && (blockID == 0x00 || blockID == 0x80) || version == 1 && blockID == 0x44
		if !isData || blockSize < 8 {
			return nil, fmt.Errorf("type[%d]: 0x%02X", len(blocks), blockID)
		}
		blocks = append(blocks, audioBlock{
			samples:        binary.BigEndian.Uint32(payload[offset+4 : offset+8]),
			payload:        payload[offset+8 : offset+blockSize],
			isSegmentStart: isSegmentStart,
		})
		offset += blockSize
		isSegmentStart = version == 0 && blockID == 0x80
	}
	if len(blocks) == 0 {
		return nil, errors.New("blocksMissing")
	}
	return blocks, nil
}

func decodePCM16BEBlocks(blocks []audioBlock, channels uint8) ([]byte, error) {
	pcm := make([]byte, 0)
	for blockIndex, block := range blocks {
		expectedSize := uint64(block.samples) * uint64(channels) * 2
		if uint64(len(block.payload)) < expectedSize {
			return nil, fmt.Errorf("block[%d]: got %d bytes, want %d", blockIndex, len(block.payload), expectedSize)
		}
		for offset := 0; offset < int(expectedSize); offset += 2 {
			pcm = append(pcm, block.payload[offset+1], block.payload[offset])
		}
	}
	return pcm, nil
}

func decodeXAS0Blocks(blocks []audioBlock, channels uint8) ([]byte, error) {
	const frameSize = 0x13
	const samplesPerFrame = 32
	pcm := make([]byte, 0)
	for blockIndex, block := range blocks {
		if block.samples == 0 {
			return nil, fmt.Errorf("block[%d]: samples missing", blockIndex)
		}
		frameGroupSize := frameSize * int(channels)
		frameCount := (int(block.samples) + samplesPerFrame - 1) / samplesPerFrame
		minimumSize := (frameCount - 1) * frameGroupSize
		if len(block.payload) <= minimumSize {
			return nil, fmt.Errorf("block[%d]: got %d bytes, want more than %d", blockIndex, len(block.payload), minimumSize)
		}
		blockPCM := make([][]int16, channels)
		for channel := range blockPCM {
			blockPCM[channel] = make([]int16, 0, frameCount*samplesPerFrame)
		}
		for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
			frameOffset := frameIndex * frameGroupSize
			for channel := 0; channel < int(channels); channel++ {
				frame := make([]byte, frameSize)
				copyInterleavedXAS0Frame(frame, block.payload, frameOffset, channel, int(channels))
				framePCM, frameErr := decodeXAS0Frame(frame)
				if frameErr != nil {
					return nil, fmt.Errorf("frame[%d:%d]: %w", blockIndex, frameIndex, frameErr)
				}
				blockPCM[channel] = append(blockPCM[channel], framePCM...)
			}
		}
		for sampleIndex := 0; sampleIndex < int(block.samples); sampleIndex++ {
			for channel := 0; channel < int(channels); channel++ {
				sample := blockPCM[channel][sampleIndex]
				pcm = binary.LittleEndian.AppendUint16(pcm, uint16(sample))
			}
		}
	}
	return pcm, nil
}

func copyInterleavedXAS0Frame(frame, payload []byte, frameOffset, channel, channels int) {
	history2Offset := frameOffset + channel*2
	history1Offset := frameOffset + channels*2 + channel*2
	if history2Offset+2 <= len(payload) {
		copy(frame[0:2], payload[history2Offset:history2Offset+2])
	}
	if history1Offset+2 <= len(payload) {
		copy(frame[2:4], payload[history1Offset:history1Offset+2])
	}
	dataOffset := frameOffset + channels*4
	for row := 0; row < 15; row++ {
		sourceOffset := dataOffset + row*channels + channel
		if sourceOffset < len(payload) {
			frame[4+row] = payload[sourceOffset]
		}
	}
}

func decodeXAS0Frame(frame []byte) ([]int16, error) {
	if len(frame) != 0x13 {
		return nil, fmt.Errorf("size: %d", len(frame))
	}
	coefficients := [4][2]float32{{0, 0}, {0.9375, 0}, {1.796875, -0.8125}, {1.53125, -0.859375}}
	header := binary.LittleEndian.Uint32(frame[0:4])
	coefficientIndex := int(header & 0x0F)
	if coefficientIndex >= len(coefficients) {
		return nil, fmt.Errorf("coefficient: %d", coefficientIndex)
	}
	history2 := int16(header & 0xFFF0)
	history1 := int16((header >> 16) & 0xFFF0)
	shift := uint((header >> 16) & 0x0F)
	pcm := make([]int16, 0, 32)
	pcm = append(pcm, history2, history1)
	for nibbleIndex := 0; nibbleIndex < 30; nibbleIndex++ {
		nibbles := frame[4+nibbleIndex/2]
		nibble := nibbles >> 4
		if nibbleIndex&1 != 0 {
			nibble = nibbles & 0x0F
		}
		scaled := int(int16(uint16(nibble)<<12) >> shift)
		sample := scaled + int(float32(history1)*coefficients[coefficientIndex][0]+float32(history2)*coefficients[coefficientIndex][1])
		if sample > 32767 {
			sample = 32767
		}
		if sample < -32768 {
			sample = -32768
		}
		history2 = history1
		history1 = int16(sample)
		pcm = append(pcm, history1)
	}
	return pcm, nil
}

func decodeXAS1Blocks(blocks []audioBlock, channels uint8) ([]byte, error) {
	const frameSize = 0x4C
	const samplesPerFrame = 128
	pcm := make([]byte, 0)
	for blockIndex, block := range blocks {
		if block.samples == 0 {
			return nil, fmt.Errorf("block[%d]: samples missing", blockIndex)
		}
		frameGroupSize := frameSize * int(channels)
		frameCount := (int(block.samples) + samplesPerFrame - 1) / samplesPerFrame
		minimumSize := (frameCount - 1) * frameGroupSize
		if len(block.payload) <= minimumSize {
			return nil, fmt.Errorf("block[%d]: got %d bytes, want more than %d", blockIndex, len(block.payload), minimumSize)
		}
		blockPCM := make([][]int16, channels)
		for channel := range blockPCM {
			blockPCM[channel] = make([]int16, 0, frameCount*samplesPerFrame)
		}
		for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
			for channel := 0; channel < int(channels); channel++ {
				frameOffset := frameIndex*frameGroupSize + channel*frameSize
				frame := make([]byte, frameSize)
				if frameOffset < len(block.payload) {
					copy(frame, block.payload[frameOffset:])
				}
				framePCM, err := decodeXAS1Frame(frame)
				if err != nil {
					return nil, fmt.Errorf("frame[%d:%d]: %w", blockIndex, frameIndex, err)
				}
				blockPCM[channel] = append(blockPCM[channel], framePCM...)
			}
		}
		for sampleIndex := 0; sampleIndex < int(block.samples); sampleIndex++ {
			for channel := 0; channel < int(channels); channel++ {
				sample := blockPCM[channel][sampleIndex]
				pcm = binary.LittleEndian.AppendUint16(pcm, uint16(sample))
			}
		}
	}
	return pcm, nil
}

func decodeXAS1Frame(frame []byte) ([]int16, error) {
	if len(frame) != 0x4C {
		return nil, fmt.Errorf("size: %d", len(frame))
	}
	coefficients := [4][2]float32{{0, 0}, {0.9375, 0}, {1.796875, -0.8125}, {1.53125, -0.859375}}
	pcm := make([]int16, 0, 128)
	for group := 0; group < 4; group++ {
		header := binary.LittleEndian.Uint32(frame[group*4 : group*4+4])
		coefficientIndex := int(header & 0x0F)
		if coefficientIndex >= len(coefficients) {
			coefficientIndex = 0
		}
		history2 := int16(header & 0xFFF0)
		history1 := int16((header >> 16) & 0xFFF0)
		shift := uint((header >> 16) & 0x0F)
		pcm = append(pcm, history2, history1)
		for row := 0; row < 15; row++ {
			nibbles := frame[16+row*4+group]
			for nibbleIndex := 0; nibbleIndex < 2; nibbleIndex++ {
				nibble := nibbles >> 4
				if nibbleIndex == 1 {
					nibble = nibbles & 0x0F
				}
				scaled := int(int16(uint16(nibble)<<12) >> shift)
				sample := scaled + int(float32(history1)*coefficients[coefficientIndex][0]+float32(history2)*coefficients[coefficientIndex][1])
				if sample > 32767 {
					sample = 32767
				}
				if sample < -32768 {
					sample = -32768
				}
				history2 = history1
				history1 = int16(sample)
				pcm = append(pcm, history1)
			}
		}
	}
	return pcm, nil
}

// EncodeWAV serializes canonical PCM16LE samples as a RIFF/WAVE file.
func EncodeWAV(wav WAV) ([]byte, error) {
	if wav.Channels == 0 || wav.SampleRate == 0 || len(wav.PCM)%int(wav.Channels*2) != 0 {
		return nil, fmt.Errorf("waveFormat: channels %d, rate %d, bytes %d", wav.Channels, wav.SampleRate, len(wav.PCM))
	}
	if uint64(len(wav.PCM))+36 > uint64(^uint32(0)) {
		return nil, fmt.Errorf("waveSize: %d", len(wav.PCM))
	}
	output := make([]byte, 44+len(wav.PCM))
	copy(output[0:4], "RIFF")
	binary.LittleEndian.PutUint32(output[4:8], uint32(36+len(wav.PCM)))
	copy(output[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(output[16:20], 16)
	binary.LittleEndian.PutUint16(output[20:22], 1)
	binary.LittleEndian.PutUint16(output[22:24], wav.Channels)
	binary.LittleEndian.PutUint32(output[24:28], wav.SampleRate)
	byteRate := wav.SampleRate * uint32(wav.Channels) * 2
	binary.LittleEndian.PutUint32(output[28:32], byteRate)
	binary.LittleEndian.PutUint16(output[32:34], wav.Channels*2)
	binary.LittleEndian.PutUint16(output[34:36], 16)
	copy(output[36:40], "data")
	binary.LittleEndian.PutUint32(output[40:44], uint32(len(wav.PCM)))
	copy(output[44:], wav.PCM)
	return output, nil
}

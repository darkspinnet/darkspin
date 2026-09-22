package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// WAV is canonical interleaved 16-bit PCM audio used by editable DS assets.
type WAV struct {
	Channels   uint16
	SampleRate uint32
	PCM        []byte
}

// DecodeWAV reads a RIFF/WAVE PCM16 file and rejects unsupported encodings.
func DecodeWAV(payload []byte) (WAV, error) {
	if len(payload) < 12 || !bytes.Equal(payload[0:4], []byte("RIFF")) || !bytes.Equal(payload[8:12], []byte("WAVE")) {
		return WAV{}, errors.New("waveHeader")
	}
	wav := WAV{}
	isFormatFound := false
	isDataFound := false
	for offset := 12; offset+8 <= len(payload); {
		chunkSize := int(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
		dataOffset := offset + 8
		dataEnd := dataOffset + chunkSize
		if chunkSize < 0 || dataEnd < dataOffset || dataEnd > len(payload) {
			return WAV{}, fmt.Errorf("chunkRange[%d]: %d", offset, chunkSize)
		}
		switch string(payload[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 {
				return WAV{}, fmt.Errorf("formatSize: %d", chunkSize)
			}
			format := binary.LittleEndian.Uint16(payload[dataOffset : dataOffset+2])
			if format != 1 {
				return WAV{}, fmt.Errorf("formatUnsupported: %d", format)
			}
			wav.Channels = binary.LittleEndian.Uint16(payload[dataOffset+2 : dataOffset+4])
			wav.SampleRate = binary.LittleEndian.Uint32(payload[dataOffset+4 : dataOffset+8])
			bitsPerSample := binary.LittleEndian.Uint16(payload[dataOffset+14 : dataOffset+16])
			if bitsPerSample != 16 {
				return WAV{}, fmt.Errorf("sampleBitsUnsupported: %d", bitsPerSample)
			}
			isFormatFound = true
		case "data":
			wav.PCM = append([]byte(nil), payload[dataOffset:dataEnd]...)
			isDataFound = true
		}
		offset = dataEnd + chunkSize%2
	}
	if !isFormatFound || !isDataFound {
		return WAV{}, errors.New("waveChunkMissing")
	}
	if wav.Channels == 0 || wav.Channels > 64 || wav.SampleRate == 0 || wav.SampleRate > 200000 {
		return WAV{}, fmt.Errorf("waveFormat: channels %d, rate %d", wav.Channels, wav.SampleRate)
	}
	frameSize := int(wav.Channels) * 2
	if len(wav.PCM)%frameSize != 0 {
		return WAV{}, fmt.Errorf("waveAlignment: %d bytes for %d channels", len(wav.PCM), wav.Channels)
	}
	return wav, nil
}

// EncodeSNRPCM creates a version-zero EA AudioCore PCM16BE header and, for RAM
// assets, embeds the WAV samples in the SNR itself.
func EncodeSNRPCM(wav WAV, storage string, isLooped bool) ([]byte, error) {
	sampleCount := len(wav.PCM) / (int(wav.Channels) * 2)
	if sampleCount > 0x1FFFFFFF {
		return nil, fmt.Errorf("sampleCountRange: %d", sampleCount)
	}
	header := Header{Version: 0, Codec: "PCM16BE", Channels: uint8(wav.Channels), SampleRate: wav.SampleRate, SampleCount: uint32(sampleCount), IsLooped: isLooped, Storage: storage}
	headerSize := 8
	if isLooped {
		headerSize += 4
		if storage == "STREAM" {
			headerSize += 4
		}
	}
	payload := make([]byte, headerSize)
	payload, err := EncodeHeader(payload, header)
	if err != nil {
		return nil, fmt.Errorf("headerEncode: %w", err)
	}
	if storage != "RAM" {
		return payload, nil
	}
	block, err := encodePCMBlocks(wav)
	if err != nil {
		return nil, fmt.Errorf("ramBlocks: %w", err)
	}
	return append(payload, block...), nil
}

// EncodeSNSPCM creates the blocked PCM16BE body paired with a streamed SNR.
func EncodeSNSPCM(wav WAV) ([]byte, error) {
	return encodePCMBlocks(wav)
}

func encodePCMBlocks(wav WAV) ([]byte, error) {
	const maxBlockData = 0x00F00000
	frameSize := int(wav.Channels) * 2
	chunkSize := maxBlockData - maxBlockData%frameSize
	blocks := make([]byte, 0, len(wav.PCM)+8)
	for offset := 0; offset < len(wav.PCM) || offset == 0; {
		remaining := len(wav.PCM) - offset
		dataSize := remaining
		if dataSize > chunkSize {
			dataSize = chunkSize
		}
		blockSize := dataSize + 8
		block := make([]byte, blockSize)
		blockID := byte(0)
		if offset+dataSize == len(wav.PCM) {
			blockID = 0x80
		}
		binary.BigEndian.PutUint32(block[0:4], uint32(blockID)<<24|uint32(blockSize))
		binary.BigEndian.PutUint32(block[4:8], uint32(dataSize/frameSize))
		for sampleOffset := 0; sampleOffset < dataSize; sampleOffset += 2 {
			block[8+sampleOffset] = wav.PCM[offset+sampleOffset+1]
			block[8+sampleOffset+1] = wav.PCM[offset+sampleOffset]
		}
		blocks = append(blocks, block...)
		offset += dataSize
		if dataSize == 0 {
			break
		}
	}
	return blocks, nil
}

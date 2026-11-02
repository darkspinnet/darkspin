package creation

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// The 65,535 carrier bits hold 8,191 complete bytes: an eight-byte header,
// compressed data, and a twelve-byte signature/checksum footer.
const appearanceCapacity = 8171
const appearanceSignature = 0x02450d16

// Build-103 retail template PNGs carry a zlib-compressed spore0007 header
// and format-21 model XML in BGRA pixel low bits. Keep the carrier pixels
// unchanged apart from those bits; no game resource is embedded in source.
// Carrier reference: https://github.com/Spore-Community/SporeTools/blob/main/SporePngDecoder.ts
// Independently decoded the installed build-103 template to spore0007 / XML
// format 21 before implementing model edits; older Spore model schemas are refused.
type appearanceCarrier struct {
	pixels   []byte
	position uint32
	hash     uint32
}

func (e *appearanceCarrier) byte(encoded byte, isWriting bool) byte {
	decoded := byte(0)
	for bit := uint(0); bit < 8; bit++ {
		position := e.position
		channel := e.pixels[position]
		e.hash = e.hash*0x01000193 ^ ((position & 7) | uint32(channel&0xf8))
		mask := byte((e.hash >> 15) & 1)
		if isWriting {
			channel = channel&0xfe | (((encoded >> bit) & 1) ^ mask)
			e.pixels[position] = channel
		}
		decoded |= ((channel & 1) ^ mask) << bit
		e.position = position >> 1
		if position&1 != 0 {
			e.position ^= 0xb400
		}
	}
	return decoded
}

// VaryAppearance makes a private model variation from a native template or
// saved appearance. The XML mutator only accepts build-103 format-21 models.
func VaryAppearance(source []byte, seed uint64) ([]byte, error) {
	config, err := png.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("appearanceConfig: %w", err)
	}
	if config.Width != 128 || config.Height != 128 {
		return nil, errors.New("appearance carrier must be 128x128")
	}
	decoded, err := png.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("appearancePNG: %w", err)
	}
	pixels := make([]byte, 128*128*4)
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			index := (y*128 + x) * 4
			pixels[index], pixels[index+1], pixels[index+2], pixels[index+3] = pixel.B, pixel.G, pixel.R, pixel.A
		}
	}
	carrier := appearanceCarrier{pixels: pixels, position: 0xb400, hash: 0x811c9dc5}
	header := make([]byte, 8)
	for index := range header {
		header[index] = carrier.byte(0, false)
	}
	length := binary.LittleEndian.Uint32(header[4:])
	if binary.LittleEndian.Uint32(header) != appearanceSignature || length == 0 || length > appearanceCapacity {
		return nil, errors.New("appearance carrier header unsupported")
	}
	compressed := make([]byte, int(length))
	for index := range compressed {
		compressed[index] = carrier.byte(0, false)
	}
	footer := make([]byte, 12)
	for index := range footer {
		footer[index] = carrier.byte(0, false)
	}
	if binary.LittleEndian.Uint32(footer) != appearanceSignature || binary.LittleEndian.Uint64(footer[4:]) != appearanceChecksum(compressed) {
		return nil, errors.New("appearance carrier checksum invalid")
	}
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("appearanceZlib: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, 2<<20))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("appearanceRead: %w", errors.Join(readErr, closeErr))
	}
	if len(payload) >= 2<<20 || !bytes.HasPrefix(payload, []byte("spore0007")) {
		return nil, errors.New("appearance metadata unsupported")
	}
	modelOffset := bytes.Index(payload, []byte("<?xml"))
	if modelOffset < 0 {
		return nil, errors.New("appearance model missing")
	}
	model, err := varyAppearanceXML(payload[modelOffset:], seed)
	if err != nil {
		return nil, fmt.Errorf("appearanceModel: %w", err)
	}
	payload = append(append([]byte(nil), payload[:modelOffset]...), model...)
	var compressedOutput bytes.Buffer
	w, err := zlib.NewWriterLevel(&compressedOutput, zlib.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("appearanceCompressor: %w", err)
	}
	count, writeErr := w.Write(payload)
	closeErr = w.Close()
	if writeErr != nil || closeErr != nil {
		return nil, fmt.Errorf("appearanceCompress: %w", errors.Join(writeErr, closeErr))
	}
	if count != len(payload) || compressedOutput.Len() > appearanceCapacity {
		return nil, errors.New("varied appearance exceeds carrier capacity")
	}
	binary.LittleEndian.PutUint32(header[4:], uint32(compressedOutput.Len()))
	binary.LittleEndian.PutUint32(footer, appearanceSignature)
	binary.LittleEndian.PutUint64(footer[4:], appearanceChecksum(compressedOutput.Bytes()))
	carrier.position, carrier.hash = 0xb400, 0x811c9dc5
	for _, encoded := range append(header, compressedOutput.Bytes()...) {
		carrier.byte(encoded, true)
	}
	for _, encoded := range footer {
		carrier.byte(encoded, true)
	}
	output := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for index := 0; index < len(pixels); index += 4 {
		output.Pix[index], output.Pix[index+1], output.Pix[index+2], output.Pix[index+3] = pixels[index+2], pixels[index+1], pixels[index], pixels[index+3]
	}
	var pngOutput bytes.Buffer
	err = png.Encode(&pngOutput, output)
	if err != nil {
		return nil, fmt.Errorf("appearanceEncode: %w", err)
	}
	return pngOutput.Bytes(), nil
}

// Build 103's PNG finalizer (0x7E2730) appends the signature and this CRC;
// its reader (0x7E2880) rejects either mismatch. 0xAFD2B0 uses the forward
// ECMA polynomial from table 0x1189608, an all-one seed, and no final XOR.
func appearanceChecksum(compressed []byte) uint64 {
	checksum := ^uint64(0)
	for _, octet := range compressed {
		checksum ^= uint64(octet) << 56
		for bit := 0; bit < 8; bit++ {
			isHighBitSet := checksum>>63 != 0
			checksum <<= 1
			if isHighBitSet {
				checksum ^= 0x42f0e1eba9ea3693
			}
		}
	}
	return checksum
}

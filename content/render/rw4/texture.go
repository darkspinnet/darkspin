package rw4

import (
	"encoding/binary"
	"fmt"
)

const (
	formatR8G8B8   = 20
	formatA8R8G8B8 = 21
	formatA8       = 28
	formatDXT1     = 0x31545844
	formatDXT3     = 0x33545844
	formatDXT5     = 0x35545844
)

// TextureImage is one decoded top-level raster image in RGBA byte order.
type TextureImage struct {
	Name   string
	Width  int
	Height int
	RGBA   []byte
}

func (e *Document) rasterImage(ordinal int) (*TextureImage, error) {
	raster, isFound := e.Rasters[ordinal]
	if !isFound {
		return nil, fmt.Errorf("section %d is not a raster", ordinal)
	}
	dataOrdinal, err := e.directOrdinal(raster.DataRef)
	if err != nil {
		return nil, fmt.Errorf("dataRef: %w", err)
	}
	payload, err := e.baseResource(dataOrdinal)
	if err != nil {
		return nil, fmt.Errorf("dataResource: %w", err)
	}
	width := int(raster.Width)
	height := int(raster.Height)
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("dimensions: %dx%d", width, height)
	}
	image := &TextureImage{
		Name:   fmt.Sprintf("rw4_raster_%d", ordinal),
		Width:  width,
		Height: height,
		RGBA:   make([]byte, width*height*4),
	}
	switch raster.TextureFormat {
	case formatR8G8B8:
		err = decodeBGR(image, payload)
	case formatA8R8G8B8:
		err = decodeBGRA(image, payload)
	case formatA8:
		err = decodeAlpha(image, payload)
	case formatDXT1:
		err = decodeBC(image, payload, 1)
	case formatDXT3:
		err = decodeBC(image, payload, 2)
	case formatDXT5:
		err = decodeBC(image, payload, 3)
	default:
		return nil, fmt.Errorf("format: unsupported 0x%08X", raster.TextureFormat)
	}
	if err != nil {
		return nil, fmt.Errorf("pixels: %w", err)
	}
	return image, nil
}

func decodeBGR(image *TextureImage, payload []byte) error {
	expectedSize := image.Width * image.Height * 3
	if len(payload) < expectedSize {
		return fmt.Errorf("size: got %d, want at least %d", len(payload), expectedSize)
	}
	for pixelIndex := 0; pixelIndex < image.Width*image.Height; pixelIndex++ {
		sourceOffset := pixelIndex * 3
		destinationOffset := pixelIndex * 4
		image.RGBA[destinationOffset] = payload[sourceOffset+2]
		image.RGBA[destinationOffset+1] = payload[sourceOffset+1]
		image.RGBA[destinationOffset+2] = payload[sourceOffset]
		image.RGBA[destinationOffset+3] = 0xFF
	}
	return nil
}

func decodeBGRA(image *TextureImage, payload []byte) error {
	expectedSize := image.Width * image.Height * 4
	if len(payload) < expectedSize {
		return fmt.Errorf("size: got %d, want at least %d", len(payload), expectedSize)
	}
	for pixelIndex := 0; pixelIndex < image.Width*image.Height; pixelIndex++ {
		offset := pixelIndex * 4
		image.RGBA[offset] = payload[offset+2]
		image.RGBA[offset+1] = payload[offset+1]
		image.RGBA[offset+2] = payload[offset]
		image.RGBA[offset+3] = payload[offset+3]
	}
	return nil
}

func decodeAlpha(image *TextureImage, payload []byte) error {
	expectedSize := image.Width * image.Height
	if len(payload) < expectedSize {
		return fmt.Errorf("size: got %d, want at least %d", len(payload), expectedSize)
	}
	for pixelIndex := 0; pixelIndex < expectedSize; pixelIndex++ {
		offset := pixelIndex * 4
		image.RGBA[offset] = 0xFF
		image.RGBA[offset+1] = 0xFF
		image.RGBA[offset+2] = 0xFF
		image.RGBA[offset+3] = payload[pixelIndex]
	}
	return nil
}

func decodeBC(image *TextureImage, payload []byte, version int) error {
	blockSize := 8
	if version != 1 {
		blockSize = 16
	}
	blockWidths := (image.Width + 3) / 4
	blockHeights := (image.Height + 3) / 4
	expectedSize := blockWidths * blockHeights * blockSize
	if len(payload) < expectedSize {
		return fmt.Errorf("size: got %d, want at least %d", len(payload), expectedSize)
	}
	for blockY := 0; blockY < blockHeights; blockY++ {
		for blockX := 0; blockX < blockWidths; blockX++ {
			offset := (blockY*blockWidths + blockX) * blockSize
			block := payload[offset : offset+blockSize]
			decodeBCBlock(image, blockX, blockY, block, version)
		}
	}
	return nil
}

func decodeBCBlock(image *TextureImage, blockX, blockY int, block []byte, version int) {
	colorOffset := 0
	if version != 1 {
		colorOffset = 8
	}
	color0 := binary.LittleEndian.Uint16(block[colorOffset : colorOffset+2])
	color1 := binary.LittleEndian.Uint16(block[colorOffset+2 : colorOffset+4])
	colors := bcColors(color0, color1, version == 1)
	colorIndexes := binary.LittleEndian.Uint32(block[colorOffset+4 : colorOffset+8])
	alphaPixels := bcAlpha(block, version)
	for pixelIndex := 0; pixelIndex < 16; pixelIndex++ {
		x := blockX*4 + pixelIndex%4
		y := blockY*4 + pixelIndex/4
		if x >= image.Width || y >= image.Height {
			continue
		}
		colorIndex := (colorIndexes >> (pixelIndex * 2)) & 0x3
		color := colors[colorIndex]
		alpha := color[3]
		if version != 1 {
			alpha = alphaPixels[pixelIndex]
		}
		offset := (y*image.Width + x) * 4
		image.RGBA[offset] = color[0]
		image.RGBA[offset+1] = color[1]
		image.RGBA[offset+2] = color[2]
		image.RGBA[offset+3] = alpha
	}
}

func bcColors(color0, color1 uint16, isDXT1 bool) [4][4]uint8 {
	colors := [4][4]uint8{rgb565(color0), rgb565(color1)}
	colors[0][3] = 0xFF
	colors[1][3] = 0xFF
	if isDXT1 && color0 <= color1 {
		for component := 0; component < 3; component++ {
			colors[2][component] = uint8((uint16(colors[0][component]) + uint16(colors[1][component])) / 2)
		}
		colors[2][3] = 0xFF
		colors[3] = [4]uint8{}
		return colors
	}
	for component := 0; component < 3; component++ {
		colors[2][component] = uint8((2*uint16(colors[0][component]) + uint16(colors[1][component])) / 3)
		colors[3][component] = uint8((uint16(colors[0][component]) + 2*uint16(colors[1][component])) / 3)
	}
	colors[2][3] = 0xFF
	colors[3][3] = 0xFF
	return colors
}

func rgb565(color uint16) [4]uint8 {
	red := uint8((color >> 11) & 0x1F)
	green := uint8((color >> 5) & 0x3F)
	blue := uint8(color & 0x1F)
	return [4]uint8{
		(red << 3) | (red >> 2),
		(green << 2) | (green >> 4),
		(blue << 3) | (blue >> 2),
		0xFF,
	}
}

func bcAlpha(block []byte, version int) [16]uint8 {
	alphaPixels := [16]uint8{}
	if version == 2 {
		bits := binary.LittleEndian.Uint64(block[:8])
		for pixelIndex := range alphaPixels {
			alphaPixels[pixelIndex] = uint8((bits>>(pixelIndex*4))&0xF) * 17
		}
		return alphaPixels
	}
	if version != 3 {
		return alphaPixels
	}
	alphaTable := [8]uint8{block[0], block[1]}
	if block[0] > block[1] {
		for index := 1; index <= 6; index++ {
			alphaTable[index+1] = uint8(((7-index)*int(block[0]) + index*int(block[1])) / 7)
		}
	} else {
		for index := 1; index <= 4; index++ {
			alphaTable[index+1] = uint8(((5-index)*int(block[0]) + index*int(block[1])) / 5)
		}
		alphaTable[6] = 0
		alphaTable[7] = 0xFF
	}
	bits := uint64(0)
	for index := 0; index < 6; index++ {
		bits |= uint64(block[index+2]) << (index * 8)
	}
	for pixelIndex := range alphaPixels {
		alphaPixels[pixelIndex] = alphaTable[(bits>>(pixelIndex*3))&0x7]
	}
	return alphaPixels
}

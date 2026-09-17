// Package rw4 decodes exact bounded structures from RenderWare 4 resources.
package rw4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

func float32At(payload []byte, offset int) float32 {
	return math.Float32frombits(readUint32(payload, offset))
}

const (
	ResourceType          = 0x2F4E681B
	TypeBaseResource      = 0x00010030
	TypeRaster            = 0x00020003
	TypeVertexDescription = 0x00020004
	TypeVertexBuffer      = 0x00020005
	TypeIndexBuffer       = 0x00020007
	TypeMesh              = 0x00020009
	TypeCompiledState     = 0x0002000B
	TypeMeshStateLink     = 0x0002001A
	TypeKeyframeAnimation = 0x00070001
	TypeSkeleton          = 0x00070002
	TypeAnimationSkin     = 0x00070003
	TypeSkeletonBinding   = 0x0007000B
	TypeSkinsInK          = 0x0007000C
	TypeSkinMatrixBuffer  = 0x0007000F
	TypeBoundingBox       = 0x00080005
	TypeMorphHandle       = 0x00FF0000
	TypeAnimations        = 0x00FF0001

	PrimitiveTriangleList = 4
)

var magic = []byte{
	0x89, 0x52, 0x57, 0x34, 0x77, 0x33, 0x32, 0x00,
	0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x20, 0x04, 0x00,
	0x34, 0x35, 0x34, 0x00, 0x30, 0x30, 0x30, 0x00,
	0x00, 0x00, 0x00, 0x00,
}

// Document is the exact sectioned representation of one decoded RW4 resource.
type Document struct {
	Payload            []byte
	Type               uint32
	BufferOffset       uint32
	SectionOffset      uint32
	Sections           []Section
	VertexFormats      map[int]VertexDescription
	VertexBuffers      map[int]VertexBuffer
	IndexBuffers       map[int]IndexBuffer
	Meshes             map[int]Mesh
	Rasters            map[int]Raster
	CompiledStates     map[int]CompiledState
	MeshStateLinks     map[int]MeshStateLink
	Skeletons          map[int]Skeleton
	AnimationSkins     map[int]AnimationSkin
	SkinMatrices       map[int]SkinMatrixBuffer
	KeyframeAnimations map[int]KeyframeAnimation
	AnimationMaps      map[int]AnimationMap
}

// Section describes one authoritatively bounded RW4 object.
type Section struct {
	Ordinal       int
	Offset        uint32
	Field04       uint32
	Size          uint32
	Alignment     uint32
	TypeCodeIndex uint32
	TypeCode      uint32
	Data          []byte
}

// VertexDescription describes the encoded layout of a vertex stream.
type VertexDescription struct {
	Field00      uint32
	Field04      uint32
	Declaration  uint32
	Field0E      uint8
	VertexSize   uint8
	ElementFlags uint32
	Field14      uint32
	Elements     []VertexElement
}

// VertexElement describes one Direct3D vertex element.
type VertexElement struct {
	Stream     uint16
	Offset     uint16
	DataType   uint8
	Method     uint8
	Usage      uint8
	UsageIndex uint8
	TypeCode   uint32
}

// VertexBuffer connects a vertex declaration to its bounded base resource.
type VertexBuffer struct {
	DescriptionRef uint32
	Field04        uint32
	BaseVertex     uint32
	VertexCount    uint32
	Field10        uint32
	VertexSize     uint32
	DataRef        uint32
}

// IndexBuffer connects primitive indexes to their bounded base resource.
type IndexBuffer struct {
	Declaration    uint32
	StartIndex     uint32
	PrimitiveCount uint32
	Usage          uint32
	Format         uint32
	PrimitiveType  uint32
	DataRef        uint32
}

// Mesh selects ranges from one index buffer and one or more vertex buffers.
type Mesh struct {
	Field00          uint32
	PrimitiveType    uint32
	IndexBufferRef   uint32
	TriangleCount    uint32
	FirstIndex       uint32
	PrimitiveCount   uint32
	FirstVertex      uint32
	VertexCount      uint32
	VertexBufferRefs []uint32
}

// Raster describes one texture and the base-resource section containing its
// encoded mip data.
type Raster struct {
	TextureFormat uint32
	TextureFlags  uint16
	VolumeDepth   uint16
	DXTexture     uint32
	Width         uint16
	Height        uint16
	Field10       uint8
	MipmapLevels  uint8
	Field12       uint16
	Field14       uint32
	Field18       uint32
	DataRef       uint32
}

// CompiledState is the decoded material state needed by portable projections.
// Raw retains the complete bounded state for lossless DSE serialization.
type CompiledState struct {
	Raw          []byte
	TextureSlots []TextureSlot
}

// TextureSlot connects one material sampler to a raster-like RW4 object.
type TextureSlot struct {
	SamplerIndex uint32
	RasterRef    uint32
}

// MeshStateLink associates a mesh with its ordered compiled material states.
type MeshStateLink struct {
	MeshRef           uint32
	CompiledStateRefs []uint32
}

type Skeleton struct {
	ID      uint32
	Names   []uint32
	Flags   []uint32
	Parents []int32
}

type AnimationSkin struct {
	Field8 uint32
	FieldC uint32
	Poses  []BonePose
}

type BonePose struct {
	Rotation    [9]float32
	Translation [3]float32
}

type SkinMatrixBuffer struct {
	Field8   uint32
	FieldC   uint32
	Matrices [][12]float32
}

type AnimationMapEntry struct {
	ID          uint32
	ResourceRef uint32
}

type AnimationMap struct {
	SelfReference uint32
	Entries       []AnimationMapEntry
}

type KeyframeAnimation struct {
	SkeletonID uint32
	FieldC     uint32
	Field1C    uint32
	Length     float32
	Field24    uint32
	Flags      uint32
	Channels   []AnimationChannel
}

type AnimationChannel struct {
	NameID     uint32
	Components uint32
	PoseSize   uint32
	Keyframes  []AnimationKeyframe
}

type AnimationKeyframe struct {
	Rotation    [4]float32
	Translation [3]float32
	Scale       [3]float32
	Factor      float32
	Field28     uint32
	Time        float32
}

// Decode reads and strictly validates the RW4 header and every known bounded
// section. Unknown bounded section types remain exact opaque section data.
func Decode(payload []byte) (*Document, error) {
	if len(payload) < 0x6c {
		return nil, fmt.Errorf("headerSize: got %d", len(payload))
	}
	if !bytes.Equal(payload[:len(magic)], magic) {
		return nil, errors.New("headerMagic: not RW4w32")
	}
	document := &Document{
		Payload:            append([]byte(nil), payload...),
		Type:               readUint32(payload, 0x1c),
		SectionOffset:      readUint32(payload, 0x30),
		BufferOffset:       readUint32(payload, 0x44),
		VertexFormats:      make(map[int]VertexDescription),
		VertexBuffers:      make(map[int]VertexBuffer),
		IndexBuffers:       make(map[int]IndexBuffer),
		Meshes:             make(map[int]Mesh),
		Rasters:            make(map[int]Raster),
		CompiledStates:     make(map[int]CompiledState),
		MeshStateLinks:     make(map[int]MeshStateLink),
		Skeletons:          make(map[int]Skeleton),
		AnimationSkins:     make(map[int]AnimationSkin),
		SkinMatrices:       make(map[int]SkinMatrixBuffer),
		KeyframeAnimations: make(map[int]KeyframeAnimation),
		AnimationMaps:      make(map[int]AnimationMap),
	}
	sectionCount := readUint32(payload, 0x24)
	if uint64(sectionCount) > uint64(len(payload))/24 {
		return nil, fmt.Errorf("sectionCount: %d exceeds payload", sectionCount)
	}
	tableEnd := uint64(document.SectionOffset) + uint64(sectionCount)*24
	if uint64(document.SectionOffset) > uint64(len(payload)) || tableEnd > uint64(len(payload)) {
		return nil, fmt.Errorf("sectionTable: %d:%d exceeds %d", document.SectionOffset, tableEnd, len(payload))
	}
	document.Sections = make([]Section, 0, sectionCount)
	for ordinal := 0; ordinal < int(sectionCount); ordinal++ {
		offset := int(document.SectionOffset) + ordinal*24
		section := Section{
			Ordinal:       ordinal,
			Offset:        readUint32(payload, offset),
			Field04:       readUint32(payload, offset+4),
			Size:          readUint32(payload, offset+8),
			Alignment:     readUint32(payload, offset+12),
			TypeCodeIndex: readUint32(payload, offset+16),
			TypeCode:      readUint32(payload, offset+20),
		}
		if section.TypeCode == TypeBaseResource {
			if uint64(section.Offset)+uint64(document.BufferOffset) > uint64(^uint32(0)) {
				return nil, fmt.Errorf("sectionOffset[%d]: base resource overflow", ordinal)
			}
			section.Offset += document.BufferOffset
		}
		sectionEnd := uint64(section.Offset) + uint64(section.Size)
		if uint64(section.Offset) > uint64(len(payload)) || sectionEnd > uint64(len(payload)) {
			return nil, fmt.Errorf("sectionRange[%d]: %d:%d exceeds %d", ordinal, section.Offset, sectionEnd, len(payload))
		}
		if section.Alignment == 0 || section.Offset%section.Alignment != 0 {
			return nil, fmt.Errorf("sectionAlignment[%d]: offset %d alignment %d", ordinal, section.Offset, section.Alignment)
		}
		section.Data = payload[section.Offset:sectionEnd]
		document.Sections = append(document.Sections, section)
	}
	for _, section := range document.Sections {
		err := document.decodeSection(section)
		if err != nil {
			return nil, fmt.Errorf("sectionDecode[%d]: %w", section.Ordinal, err)
		}
	}
	return document, nil
}

func (e *Document) decodeSection(section Section) error {
	switch section.TypeCode {
	case TypeBaseResource:
		return nil
	case TypeRaster:
		raster, err := decodeRaster(section.Data)
		if err != nil {
			return fmt.Errorf("raster: %w", err)
		}
		e.Rasters[section.Ordinal] = raster
	case TypeVertexDescription:
		vertexDescription, err := decodeVertexDescription(section.Data)
		if err != nil {
			return fmt.Errorf("vertexDescription: %w", err)
		}
		e.VertexFormats[section.Ordinal] = vertexDescription
	case TypeVertexBuffer:
		vertexBuffer, err := decodeVertexBuffer(section.Data)
		if err != nil {
			return fmt.Errorf("vertexBuffer: %w", err)
		}
		e.VertexBuffers[section.Ordinal] = vertexBuffer
	case TypeIndexBuffer:
		indexBuffer, err := decodeIndexBuffer(section.Data)
		if err != nil {
			return fmt.Errorf("indexBuffer: %w", err)
		}
		e.IndexBuffers[section.Ordinal] = indexBuffer
	case TypeMesh:
		mesh, err := decodeMesh(section.Data)
		if err != nil {
			return fmt.Errorf("mesh: %w", err)
		}
		e.Meshes[section.Ordinal] = mesh
	case TypeCompiledState:
		compiledState, err := decodeCompiledState(section.Data)
		if err != nil {
			return fmt.Errorf("compiledState: %w", err)
		}
		e.CompiledStates[section.Ordinal] = compiledState
	case TypeMeshStateLink:
		meshStateLink, err := decodeMeshStateLink(section.Data)
		if err != nil {
			return fmt.Errorf("meshStateLink: %w", err)
		}
		e.MeshStateLinks[section.Ordinal] = meshStateLink
	case TypeKeyframeAnimation:
		animation, err := decodeKeyframeAnimation(section)
		if err != nil {
			return fmt.Errorf("keyframeAnimation: %w", err)
		}
		e.KeyframeAnimations[section.Ordinal] = animation
	case TypeSkeleton:
		skeleton, err := decodeSkeleton(section)
		if err != nil {
			return fmt.Errorf("skeleton: %w", err)
		}
		e.Skeletons[section.Ordinal] = skeleton
	case TypeAnimationSkin:
		animationSkin, err := decodeAnimationSkin(section.Data)
		if err != nil {
			return fmt.Errorf("animationSkin: %w", err)
		}
		e.AnimationSkins[section.Ordinal] = animationSkin
	case TypeSkinMatrixBuffer:
		skinMatrix, err := decodeSkinMatrixBuffer(section.Data)
		if err != nil {
			return fmt.Errorf("skinMatrix: %w", err)
		}
		e.SkinMatrices[section.Ordinal] = skinMatrix
	case TypeAnimations:
		animationMap, err := decodeAnimationMap(section.Data)
		if err != nil {
			return fmt.Errorf("animationMap: %w", err)
		}
		e.AnimationMaps[section.Ordinal] = animationMap
	}
	return nil
}

func decodeSkeleton(section Section) (Skeleton, error) {
	payload := section.Data
	if len(payload) < 24 {
		return Skeleton{}, fmt.Errorf("size: got %d, want at least 24", len(payload))
	}
	count := readUint32(payload, 12)
	if readUint32(payload, 20) != count {
		return Skeleton{}, fmt.Errorf("repeatedCount: got %d, want %d", readUint32(payload, 20), count)
	}
	if len(payload) != 24+int(count)*12 {
		return Skeleton{}, fmt.Errorf("size: got %d, want %d", len(payload), 24+int(count)*12)
	}
	localOffset := func(pointer uint32, name string) (int, error) {
		if pointer < section.Offset {
			return 0, fmt.Errorf("%sPointer: %d precedes %d", name, pointer, section.Offset)
		}
		offset := int(pointer - section.Offset)
		if offset < 24 || uint64(offset)+uint64(count)*4 > uint64(len(payload)) {
			return 0, fmt.Errorf("%sRange: %d count %d", name, offset, count)
		}
		return offset, nil
	}
	flagOffset, err := localOffset(readUint32(payload, 0), "flags")
	if err != nil {
		return Skeleton{}, err
	}
	parentOffset, err := localOffset(readUint32(payload, 4), "parents")
	if err != nil {
		return Skeleton{}, err
	}
	nameOffset, err := localOffset(readUint32(payload, 8), "names")
	if err != nil {
		return Skeleton{}, err
	}
	skeleton := Skeleton{ID: readUint32(payload, 16), Names: make([]uint32, count), Flags: make([]uint32, count), Parents: make([]int32, count)}
	for index := range skeleton.Names {
		skeleton.Names[index] = readUint32(payload, nameOffset+index*4)
		skeleton.Flags[index] = readUint32(payload, flagOffset+index*4)
		skeleton.Parents[index] = int32(readUint32(payload, parentOffset+index*4))
		if skeleton.Parents[index] < -1 || skeleton.Parents[index] >= int32(count) {
			return Skeleton{}, fmt.Errorf("parent[%d]: %d", index, skeleton.Parents[index])
		}
	}
	return skeleton, nil
}

func decodeAnimationSkin(payload []byte) (AnimationSkin, error) {
	if len(payload) < 16 {
		return AnimationSkin{}, fmt.Errorf("size: got %d", len(payload))
	}
	count := readUint32(payload, 4)
	if len(payload) != 16+int(count)*64 {
		return AnimationSkin{}, fmt.Errorf("size: got %d, want %d", len(payload), 16+int(count)*64)
	}
	skin := AnimationSkin{Field8: readUint32(payload, 8), FieldC: readUint32(payload, 12), Poses: make([]BonePose, count)}
	for boneIndex := range skin.Poses {
		base := 16 + boneIndex*64
		for row := 0; row < 3; row++ {
			for column := 0; column < 3; column++ {
				skin.Poses[boneIndex].Rotation[row*3+column] = float32At(payload, base+row*16+column*4)
			}
		}
		for coordinate := 0; coordinate < 3; coordinate++ {
			skin.Poses[boneIndex].Translation[coordinate] = float32At(payload, base+48+coordinate*4)
		}
	}
	return skin, nil
}

func decodeSkinMatrixBuffer(payload []byte) (SkinMatrixBuffer, error) {
	if len(payload) < 16 {
		return SkinMatrixBuffer{}, fmt.Errorf("size: got %d", len(payload))
	}
	count := readUint32(payload, 4)
	if len(payload) != 16+int(count)*48 {
		return SkinMatrixBuffer{}, fmt.Errorf("size: got %d, want %d", len(payload), 16+int(count)*48)
	}
	buffer := SkinMatrixBuffer{Field8: readUint32(payload, 8), FieldC: readUint32(payload, 12), Matrices: make([][12]float32, count)}
	for matrixIndex := range buffer.Matrices {
		for elementIndex := range buffer.Matrices[matrixIndex] {
			buffer.Matrices[matrixIndex][elementIndex] = float32At(payload, 16+matrixIndex*48+elementIndex*4)
		}
	}
	return buffer, nil
}

func decodeRaster(payload []byte) (Raster, error) {
	if len(payload) != 32 {
		return Raster{}, fmt.Errorf("size: got %d, want 32", len(payload))
	}
	return Raster{
		TextureFormat: readUint32(payload, 0),
		TextureFlags:  binary.LittleEndian.Uint16(payload[4:6]),
		VolumeDepth:   binary.LittleEndian.Uint16(payload[6:8]),
		DXTexture:     readUint32(payload, 8),
		Width:         binary.LittleEndian.Uint16(payload[12:14]),
		Height:        binary.LittleEndian.Uint16(payload[14:16]),
		Field10:       payload[16],
		MipmapLevels:  payload[17],
		Field12:       binary.LittleEndian.Uint16(payload[18:20]),
		Field14:       readUint32(payload, 20),
		Field18:       readUint32(payload, 24),
		DataRef:       readUint32(payload, 28),
	}, nil
}

func decodeMeshStateLink(payload []byte) (MeshStateLink, error) {
	if len(payload) < 8 {
		return MeshStateLink{}, fmt.Errorf("size: got %d, want at least 8", len(payload))
	}
	count := readUint32(payload, 4)
	if uint64(count) > uint64(len(payload))/4 {
		return MeshStateLink{}, fmt.Errorf("stateCount: %d exceeds payload", count)
	}
	expectedSize := 8 + int(count)*4
	if len(payload) != expectedSize {
		return MeshStateLink{}, fmt.Errorf("size: got %d, want %d", len(payload), expectedSize)
	}
	link := MeshStateLink{MeshRef: readUint32(payload, 0), CompiledStateRefs: make([]uint32, count)}
	for index := range link.CompiledStateRefs {
		link.CompiledStateRefs[index] = readUint32(payload, 8+index*4)
	}
	return link, nil
}

func decodeVertexDescription(payload []byte) (VertexDescription, error) {
	if len(payload) < 24 {
		return VertexDescription{}, fmt.Errorf("size: got %d, want at least 24", len(payload))
	}
	count := int(binary.LittleEndian.Uint16(payload[12:14]))
	expectedSize := 24 + count*12
	if len(payload) != expectedSize {
		return VertexDescription{}, fmt.Errorf("size: got %d, want %d", len(payload), expectedSize)
	}
	description := VertexDescription{
		Field00:      readUint32(payload, 0),
		Field04:      readUint32(payload, 4),
		Declaration:  readUint32(payload, 8),
		Field0E:      payload[14],
		VertexSize:   payload[15],
		ElementFlags: readUint32(payload, 16),
		Field14:      readUint32(payload, 20),
		Elements:     make([]VertexElement, 0, count),
	}
	for index := 0; index < count; index++ {
		offset := 24 + index*12
		description.Elements = append(description.Elements, VertexElement{
			Stream:     binary.LittleEndian.Uint16(payload[offset : offset+2]),
			Offset:     binary.LittleEndian.Uint16(payload[offset+2 : offset+4]),
			DataType:   payload[offset+4],
			Method:     payload[offset+5],
			Usage:      payload[offset+6],
			UsageIndex: payload[offset+7],
			TypeCode:   readUint32(payload, offset+8),
		})
	}
	return description, nil
}

func decodeVertexBuffer(payload []byte) (VertexBuffer, error) {
	if len(payload) != 28 {
		return VertexBuffer{}, fmt.Errorf("size: got %d, want 28", len(payload))
	}
	return VertexBuffer{
		DescriptionRef: readUint32(payload, 0),
		Field04:        readUint32(payload, 4),
		BaseVertex:     readUint32(payload, 8),
		VertexCount:    readUint32(payload, 12),
		Field10:        readUint32(payload, 16),
		VertexSize:     readUint32(payload, 20),
		DataRef:        readUint32(payload, 24),
	}, nil
}

func decodeIndexBuffer(payload []byte) (IndexBuffer, error) {
	if len(payload) != 28 {
		return IndexBuffer{}, fmt.Errorf("size: got %d, want 28", len(payload))
	}
	return IndexBuffer{
		Declaration:    readUint32(payload, 0),
		StartIndex:     readUint32(payload, 4),
		PrimitiveCount: readUint32(payload, 8),
		Usage:          readUint32(payload, 12),
		Format:         readUint32(payload, 16),
		PrimitiveType:  readUint32(payload, 20),
		DataRef:        readUint32(payload, 24),
	}, nil
}

func decodeMesh(payload []byte) (Mesh, error) {
	if len(payload) < 36 {
		return Mesh{}, fmt.Errorf("size: got %d, want at least 36", len(payload))
	}
	bufferCount := readUint32(payload, 16)
	if uint64(bufferCount) > uint64(len(payload))/4 {
		return Mesh{}, fmt.Errorf("bufferCount: %d exceeds payload", bufferCount)
	}
	expectedSize := 36 + int(bufferCount)*4
	if len(payload) != expectedSize {
		return Mesh{}, fmt.Errorf("size: got %d, want %d", len(payload), expectedSize)
	}
	mesh := Mesh{
		Field00:          readUint32(payload, 0),
		PrimitiveType:    readUint32(payload, 4),
		IndexBufferRef:   readUint32(payload, 8),
		TriangleCount:    readUint32(payload, 12),
		FirstIndex:       readUint32(payload, 20),
		PrimitiveCount:   readUint32(payload, 24),
		FirstVertex:      readUint32(payload, 28),
		VertexCount:      readUint32(payload, 32),
		VertexBufferRefs: make([]uint32, 0, bufferCount),
	}
	for index := 0; index < int(bufferCount); index++ {
		mesh.VertexBufferRefs = append(mesh.VertexBufferRefs, readUint32(payload, 36+index*4))
	}
	return mesh, nil
}

func (e *Document) directOrdinal(reference uint32) (int, error) {
	referenceType := reference >> 22
	if referenceType != 0 {
		return 0, fmt.Errorf("referenceType: %d is not direct", referenceType)
	}
	ordinal := int(reference & 0x003fffff)
	if ordinal < 0 || ordinal >= len(e.Sections) {
		return 0, fmt.Errorf("referenceOrdinal: %d exceeds %d", ordinal, len(e.Sections))
	}
	return ordinal, nil
}

func readUint32(payload []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(payload[offset : offset+4])
}

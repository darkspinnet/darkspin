package rw4

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

const (
	usagePosition     = 0
	usageNormal       = 2
	usageTexCoord0    = 6
	typeBlendIndices0 = 14
	typeBlendWeights0 = 15
	typeBlendIndices1 = 22
	typeBlendWeights1 = 23

	dataFloat2  = 1
	dataFloat3  = 2
	dataUByte4  = 5
	dataUByte4N = 8

	formatIndex16 = 101
	formatIndex32 = 102
)

// Primitive is the glTF-projectable portion of one RW4 mesh. It is deliberately
// separate from Document: projection may discard render-only metadata, while
// Document retains every bounded RW4 section.
type Primitive struct {
	Name      string
	Positions [][3]float32
	Normals   [][3]float32
	TexCoords [][2]float32
	Indices   []uint32
	Material  *Material
	Joints0   [][4]uint16
	Weights0  [][4]float32
	Joints1   [][4]uint16
	Weights1  [][4]float32
	Rig       *Rig
}

type Rig struct {
	Skeleton     Skeleton
	BindMatrices [][12]float32
	RestPoses    []BonePose
	Animations   []RigAnimation
}

// Material is the portable portion of one RW4 compiled material state.
type Material struct {
	Name      string
	BaseColor *TextureImage
	Normal    *TextureImage
}

// Primitives projects every ordinary triangle-list mesh into portable streams.
func (e *Document) Primitives() ([]Primitive, error) {
	ordinals := make([]int, 0, len(e.Meshes))
	for ordinal := range e.Meshes {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	primitives := make([]Primitive, 0, len(ordinals))
	for _, ordinal := range ordinals {
		primitive, err := e.primitive(ordinal, e.Meshes[ordinal])
		if err != nil {
			return nil, fmt.Errorf("meshProject[%d]: %w", ordinal, err)
		}
		primitives = append(primitives, primitive)
	}
	if len(primitives) == 0 {
		return nil, fmt.Errorf("meshMissing: no 0x%08X sections", TypeMesh)
	}
	return primitives, nil
}

// ProjectablePrimitives returns every independently projectable mesh and the
// number of mesh sections skipped because their references or stream formats
// are not representable by the current glTF projection.
func (e *Document) ProjectablePrimitives() ([]Primitive, int) {
	ordinals := make([]int, 0, len(e.Meshes))
	for ordinal := range e.Meshes {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	primitives := make([]Primitive, 0, len(ordinals))
	skippedMeshCount := 0
	for _, ordinal := range ordinals {
		primitive, err := e.primitive(ordinal, e.Meshes[ordinal])
		if err != nil {
			skippedMeshCount++
			continue
		}
		primitives = append(primitives, primitive)
	}
	return primitives, skippedMeshCount
}

func (e *Document) primitive(ordinal int, mesh Mesh) (Primitive, error) {
	if mesh.PrimitiveType != PrimitiveTriangleList {
		return Primitive{}, fmt.Errorf("primitiveType: got %d, want triangle list", mesh.PrimitiveType)
	}
	if uint64(mesh.TriangleCount)*3 != uint64(mesh.PrimitiveCount) {
		return Primitive{}, fmt.Errorf("indexCount: primitive %d, triangle %d", mesh.PrimitiveCount, mesh.TriangleCount)
	}
	if len(mesh.VertexBufferRefs) != 1 {
		return Primitive{}, fmt.Errorf("vertexBufferCount: got %d, prototype supports 1", len(mesh.VertexBufferRefs))
	}
	vertexBufferOrdinal, err := e.directOrdinal(mesh.VertexBufferRefs[0])
	if err != nil {
		return Primitive{}, fmt.Errorf("vertexBufferRef: %w", err)
	}
	vertexBuffer, isFound := e.VertexBuffers[vertexBufferOrdinal]
	if !isFound {
		return Primitive{}, fmt.Errorf("vertexBufferType: section %d is not a vertex buffer", vertexBufferOrdinal)
	}
	primitive, err := e.readVertices(mesh, vertexBuffer)
	if err != nil {
		return Primitive{}, fmt.Errorf("vertexRead: %w", err)
	}
	primitive.Name = fmt.Sprintf("rw4_mesh_%d", ordinal)
	primitive.Indices, err = e.readIndices(mesh)
	if err != nil {
		return Primitive{}, fmt.Errorf("indexRead: %w", err)
	}
	primitive.Material, err = e.material(ordinal)
	if err != nil {
		return Primitive{}, fmt.Errorf("materialRead: %w", err)
	}
	primitive.Rig, err = e.rig()
	if err != nil {
		return Primitive{}, fmt.Errorf("rigRead: %w", err)
	}
	return primitive, nil
}

func (e *Document) rig() (*Rig, error) {
	for _, section := range e.Sections {
		if section.TypeCode != TypeSkinsInK {
			continue
		}
		if len(section.Data) != 20 {
			return nil, fmt.Errorf("skinBindingSize: %d", len(section.Data))
		}
		matrixOrdinal, err := e.directOrdinal(readUint32(section.Data, 8))
		if err != nil {
			return nil, fmt.Errorf("matrixRef: %w", err)
		}
		skeletonOrdinal, err := e.directOrdinal(readUint32(section.Data, 12))
		if err != nil {
			return nil, fmt.Errorf("skeletonRef: %w", err)
		}
		animationSkinOrdinal, err := e.directOrdinal(readUint32(section.Data, 16))
		if err != nil {
			return nil, fmt.Errorf("animationSkinRef: %w", err)
		}
		skeleton, isFound := e.Skeletons[skeletonOrdinal]
		if !isFound {
			return nil, fmt.Errorf("skeletonType: section %d", skeletonOrdinal)
		}
		matrices, isFound := e.SkinMatrices[matrixOrdinal]
		if !isFound {
			return nil, fmt.Errorf("matrixType: section %d", matrixOrdinal)
		}
		poses, isFound := e.AnimationSkins[animationSkinOrdinal]
		if !isFound {
			return nil, fmt.Errorf("animationSkinType: section %d", animationSkinOrdinal)
		}
		if len(skeleton.Names) != len(matrices.Matrices) || len(skeleton.Names) != len(poses.Poses) {
			return nil, fmt.Errorf("boneCount: skeleton %d, matrices %d, poses %d", len(skeleton.Names), len(matrices.Matrices), len(poses.Poses))
		}
		rig := &Rig{Skeleton: skeleton, BindMatrices: matrices.Matrices, RestPoses: poses.Poses}
		rig.Animations, err = e.rigAnimations(rig)
		if err != nil {
			return nil, fmt.Errorf("animationRead: %w", err)
		}
		return rig, nil
	}
	return nil, nil
}

func (e *Document) material(meshOrdinal int) (*Material, error) {
	linkOrdinals := make([]int, 0, len(e.MeshStateLinks))
	for ordinal := range e.MeshStateLinks {
		linkOrdinals = append(linkOrdinals, ordinal)
	}
	sort.Ints(linkOrdinals)
	for _, linkOrdinal := range linkOrdinals {
		link := e.MeshStateLinks[linkOrdinal]
		linkedMeshOrdinal, err := e.directOrdinal(link.MeshRef)
		if err != nil {
			return nil, fmt.Errorf("link[%d]Mesh: %w", linkOrdinal, err)
		}
		if linkedMeshOrdinal != meshOrdinal {
			continue
		}
		if len(link.CompiledStateRefs) == 0 {
			return nil, fmt.Errorf("link[%d]: no compiled states", linkOrdinal)
		}
		stateOrdinal, err := e.directOrdinal(link.CompiledStateRefs[0])
		if err != nil {
			return nil, fmt.Errorf("link[%d]State: %w", linkOrdinal, err)
		}
		state, isFound := e.CompiledStates[stateOrdinal]
		if !isFound {
			return nil, fmt.Errorf("stateType: section %d is not a compiled state", stateOrdinal)
		}
		material := &Material{Name: fmt.Sprintf("rw4_material_%d", stateOrdinal)}
		if len(state.TextureSlots) != 0 {
			material.BaseColor, err = e.textureSlotImage(state.TextureSlots[0])
			if err != nil {
				return nil, fmt.Errorf("baseColor: %w", err)
			}
		}
		if len(state.TextureSlots) > 1 {
			material.Normal, err = e.textureSlotImage(state.TextureSlots[1])
			if err != nil {
				return nil, fmt.Errorf("normal: %w", err)
			}
		}
		return material, nil
	}
	return nil, nil
}

func (e *Document) textureSlotImage(slot TextureSlot) (*TextureImage, error) {
	referenceType := slot.RasterRef >> 22
	if referenceType != 0 {
		return nil, nil
	}
	rasterOrdinal, err := e.directOrdinal(slot.RasterRef)
	if err != nil {
		return nil, fmt.Errorf("rasterRef: %w", err)
	}
	image, err := e.rasterImage(rasterOrdinal)
	if err != nil {
		return nil, fmt.Errorf("raster[%d]: %w", rasterOrdinal, err)
	}
	return image, nil
}

func (e *Document) readVertices(mesh Mesh, vertexBuffer VertexBuffer) (Primitive, error) {
	descriptionOrdinal, err := e.directOrdinal(vertexBuffer.DescriptionRef)
	if err != nil {
		return Primitive{}, fmt.Errorf("descriptionRef: %w", err)
	}
	description, isFound := e.VertexFormats[descriptionOrdinal]
	if !isFound {
		return Primitive{}, fmt.Errorf("descriptionType: section %d is not a vertex description", descriptionOrdinal)
	}
	dataOrdinal, err := e.directOrdinal(vertexBuffer.DataRef)
	if err != nil {
		return Primitive{}, fmt.Errorf("dataRef: %w", err)
	}
	data, err := e.baseResource(dataOrdinal)
	if err != nil {
		return Primitive{}, fmt.Errorf("dataResource: %w", err)
	}
	if vertexBuffer.VertexSize == 0 || vertexBuffer.VertexSize != uint32(description.VertexSize) {
		return Primitive{}, fmt.Errorf("vertexSize: buffer %d, description %d", vertexBuffer.VertexSize, description.VertexSize)
	}
	vertexEnd := uint64(mesh.FirstVertex) + uint64(mesh.VertexCount)
	if vertexEnd > uint64(vertexBuffer.VertexCount) {
		return Primitive{}, fmt.Errorf("vertexRange: %d:%d exceeds %d", mesh.FirstVertex, vertexEnd, vertexBuffer.VertexCount)
	}
	byteEnd := vertexEnd * uint64(vertexBuffer.VertexSize)
	if byteEnd > uint64(len(data)) {
		return Primitive{}, fmt.Errorf("vertexData: need %d, have %d", byteEnd, len(data))
	}
	positionElement, isPositionFound := findElement(description.Elements, usagePosition)
	if !isPositionFound {
		return Primitive{}, fmt.Errorf("positionMissing: usage %d", usagePosition)
	}
	normalElement, isNormalFound := findElement(description.Elements, usageNormal)
	texCoordElement, isTexCoordFound := findElement(description.Elements, usageTexCoord0)
	joint0Element, isJoint0Found := findElement(description.Elements, typeBlendIndices0)
	weight0Element, isWeight0Found := findElement(description.Elements, typeBlendWeights0)
	joint1Element, isJoint1Found := findElement(description.Elements, typeBlendIndices1)
	weight1Element, isWeight1Found := findElement(description.Elements, typeBlendWeights1)
	if isJoint0Found != isWeight0Found || isJoint1Found != isWeight1Found {
		return Primitive{}, fmt.Errorf("skinStreams: joint and weight elements differ")
	}
	primitive := Primitive{Positions: make([][3]float32, mesh.VertexCount)}
	if isNormalFound {
		primitive.Normals = make([][3]float32, mesh.VertexCount)
	}
	if isTexCoordFound {
		primitive.TexCoords = make([][2]float32, mesh.VertexCount)
	}
	if isJoint0Found {
		primitive.Joints0 = make([][4]uint16, mesh.VertexCount)
		primitive.Weights0 = make([][4]float32, mesh.VertexCount)
	}
	if isJoint1Found {
		primitive.Joints1 = make([][4]uint16, mesh.VertexCount)
		primitive.Weights1 = make([][4]float32, mesh.VertexCount)
	}
	for vertexIndex := uint32(0); vertexIndex < mesh.VertexCount; vertexIndex++ {
		baseOffset := uint64(mesh.FirstVertex+vertexIndex) * uint64(vertexBuffer.VertexSize)
		primitive.Positions[vertexIndex], err = readFloat3(data, baseOffset, positionElement)
		if err != nil {
			return Primitive{}, fmt.Errorf("position[%d]: %w", vertexIndex, err)
		}
		if isNormalFound {
			primitive.Normals[vertexIndex], err = readNormal(data, baseOffset, normalElement)
			if err != nil {
				return Primitive{}, fmt.Errorf("normal[%d]: %w", vertexIndex, err)
			}
		}
		if isTexCoordFound {
			primitive.TexCoords[vertexIndex], err = readFloat2(data, baseOffset, texCoordElement)
			if err != nil {
				return Primitive{}, fmt.Errorf("texCoord[%d]: %w", vertexIndex, err)
			}
		}
		if isJoint0Found {
			primitive.Joints0[vertexIndex], err = readJoints(data, baseOffset, joint0Element)
			if err != nil {
				return Primitive{}, fmt.Errorf("joints0[%d]: %w", vertexIndex, err)
			}
			primitive.Weights0[vertexIndex], err = readWeights(data, baseOffset, weight0Element)
			if err != nil {
				return Primitive{}, fmt.Errorf("weights0[%d]: %w", vertexIndex, err)
			}
		}
		if isJoint1Found {
			primitive.Joints1[vertexIndex], err = readJoints(data, baseOffset, joint1Element)
			if err != nil {
				return Primitive{}, fmt.Errorf("joints1[%d]: %w", vertexIndex, err)
			}
			primitive.Weights1[vertexIndex], err = readWeights(data, baseOffset, weight1Element)
			if err != nil {
				return Primitive{}, fmt.Errorf("weights1[%d]: %w", vertexIndex, err)
			}
		}
	}
	return primitive, nil
}

func readJoints(data []byte, baseOffset uint64, element VertexElement) ([4]uint16, error) {
	if element.DataType != dataUByte4 {
		return [4]uint16{}, fmt.Errorf("dataType: got %d, want UBYTE4", element.DataType)
	}
	offset := baseOffset + uint64(element.Offset)
	if offset+4 > uint64(len(data)) {
		return [4]uint16{}, fmt.Errorf("range: %d:%d exceeds %d", offset, offset+4, len(data))
	}
	// RenderWare stores byte offsets into a three-row matrix palette, not glTF
	// joint ordinals. Every bone therefore advances the encoded index by three.
	return [4]uint16{uint16(data[offset]) / 3, uint16(data[offset+1]) / 3, uint16(data[offset+2]) / 3, uint16(data[offset+3]) / 3}, nil
}

func readWeights(data []byte, baseOffset uint64, element VertexElement) ([4]float32, error) {
	if element.DataType != dataUByte4N {
		return [4]float32{}, fmt.Errorf("dataType: got %d, want UBYTE4N", element.DataType)
	}
	offset := baseOffset + uint64(element.Offset)
	if offset+4 > uint64(len(data)) {
		return [4]float32{}, fmt.Errorf("range: %d:%d exceeds %d", offset, offset+4, len(data))
	}
	weights := [4]float32{float32(data[offset]) / 255, float32(data[offset+1]) / 255, float32(data[offset+2]) / 255, float32(data[offset+3]) / 255}
	total := weights[0] + weights[1] + weights[2] + weights[3]
	if total > 0 {
		for index := range weights {
			weights[index] /= total
		}
	}
	return weights, nil
}

func (e *Document) readIndices(mesh Mesh) ([]uint32, error) {
	indexBufferOrdinal, err := e.directOrdinal(mesh.IndexBufferRef)
	if err != nil {
		return nil, fmt.Errorf("bufferRef: %w", err)
	}
	indexBuffer, isFound := e.IndexBuffers[indexBufferOrdinal]
	if !isFound {
		return nil, fmt.Errorf("bufferType: section %d is not an index buffer", indexBufferOrdinal)
	}
	if indexBuffer.PrimitiveType != PrimitiveTriangleList {
		return nil, fmt.Errorf("primitiveType: got %d, want triangle list", indexBuffer.PrimitiveType)
	}
	dataOrdinal, err := e.directOrdinal(indexBuffer.DataRef)
	if err != nil {
		return nil, fmt.Errorf("dataRef: %w", err)
	}
	data, err := e.baseResource(dataOrdinal)
	if err != nil {
		return nil, fmt.Errorf("dataResource: %w", err)
	}
	indexSize := uint64(0)
	switch indexBuffer.Format {
	case formatIndex16:
		indexSize = 2
	case formatIndex32:
		indexSize = 4
	default:
		return nil, fmt.Errorf("format: unsupported D3D format %d", indexBuffer.Format)
	}
	indexCount := uint64(mesh.TriangleCount) * 3
	byteStart := uint64(mesh.FirstIndex) * indexSize
	byteEnd := byteStart + indexCount*indexSize
	if byteEnd > uint64(len(data)) {
		return nil, fmt.Errorf("range: %d:%d exceeds %d", byteStart, byteEnd, len(data))
	}
	indices := make([]uint32, indexCount)
	for index := uint64(0); index < indexCount; index++ {
		offset := byteStart + index*indexSize
		if indexSize == 2 {
			indices[index] = uint32(binary.LittleEndian.Uint16(data[offset : offset+2]))
		} else {
			indices[index] = binary.LittleEndian.Uint32(data[offset : offset+4])
		}
		indices[index] += indexBuffer.StartIndex
		if indices[index] < mesh.FirstVertex {
			return nil, fmt.Errorf("index[%d]: %d precedes first vertex %d", index, indices[index], mesh.FirstVertex)
		}
		indices[index] -= mesh.FirstVertex
		if indices[index] >= mesh.VertexCount {
			return nil, fmt.Errorf("index[%d]: %d exceeds vertex count %d", index, indices[index], mesh.VertexCount)
		}
	}
	return indices, nil
}

func (e *Document) baseResource(ordinal int) ([]byte, error) {
	if ordinal < 0 || ordinal >= len(e.Sections) {
		return nil, fmt.Errorf("ordinal: %d exceeds %d", ordinal, len(e.Sections))
	}
	section := e.Sections[ordinal]
	if section.TypeCode != TypeBaseResource {
		return nil, fmt.Errorf("type: section %d is 0x%08X", ordinal, section.TypeCode)
	}
	return section.Data, nil
}

func findElement(elements []VertexElement, typeCode uint32) (VertexElement, bool) {
	for _, element := range elements {
		if element.TypeCode == typeCode {
			return element, true
		}
	}
	return VertexElement{}, false
}

func readFloat3(data []byte, baseOffset uint64, element VertexElement) ([3]float32, error) {
	if element.DataType != dataFloat3 {
		return [3]float32{}, fmt.Errorf("dataType: got %d, want FLOAT3", element.DataType)
	}
	offset := baseOffset + uint64(element.Offset)
	if offset+12 > uint64(len(data)) {
		return [3]float32{}, fmt.Errorf("range: %d:%d exceeds %d", offset, offset+12, len(data))
	}
	return [3]float32{
		math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4])),
		math.Float32frombits(binary.LittleEndian.Uint32(data[offset+4 : offset+8])),
		math.Float32frombits(binary.LittleEndian.Uint32(data[offset+8 : offset+12])),
	}, nil
}

func readFloat2(data []byte, baseOffset uint64, element VertexElement) ([2]float32, error) {
	if element.DataType != dataFloat2 {
		return [2]float32{}, fmt.Errorf("dataType: got %d, want FLOAT2", element.DataType)
	}
	offset := baseOffset + uint64(element.Offset)
	if offset+8 > uint64(len(data)) {
		return [2]float32{}, fmt.Errorf("range: %d:%d exceeds %d", offset, offset+8, len(data))
	}
	return [2]float32{
		math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4])),
		math.Float32frombits(binary.LittleEndian.Uint32(data[offset+4 : offset+8])),
	}, nil
}

func readNormal(data []byte, baseOffset uint64, element VertexElement) ([3]float32, error) {
	if element.DataType == dataFloat3 {
		return readFloat3(data, baseOffset, element)
	}
	if element.DataType != dataUByte4 && element.DataType != dataUByte4N {
		return [3]float32{}, fmt.Errorf("dataType: got %d, want FLOAT3, UBYTE4, or UBYTE4N", element.DataType)
	}
	offset := baseOffset + uint64(element.Offset)
	if offset+4 > uint64(len(data)) {
		return [3]float32{}, fmt.Errorf("range: %d:%d exceeds %d", offset, offset+4, len(data))
	}
	return [3]float32{
		(float32(data[offset]) - 127.5) / 127.5,
		(float32(data[offset+1]) - 127.5) / 127.5,
		(float32(data[offset+2]) - 127.5) / 127.5,
	}, nil
}

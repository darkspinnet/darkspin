package gmsh

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/content/render/rw4"
)

// Primitives projects every supported GMSH draw range into glTF-portable
// geometry while the Document retains the authoritative reconstruction data.
func (e *Document) Primitives() ([]rw4.Primitive, error) {
	if e == nil {
		return nil, fmt.Errorf("documentMissing")
	}
	primitives := make([]rw4.Primitive, 0)
	for meshIndex, mesh := range e.Meshes {
		for drawIndex, draw := range mesh.Draws {
			primitive, err := e.primitive(meshIndex, drawIndex, mesh, draw)
			if err != nil {
				return nil, fmt.Errorf("drawProject[%d:%d]: %w", meshIndex, drawIndex, err)
			}
			primitives = append(primitives, primitive)
		}
	}
	if len(primitives) == 0 {
		return nil, fmt.Errorf("drawMissing")
	}
	return primitives, nil
}

// ProjectablePrimitives returns every independently supported draw and the
// number skipped because its streams or ranges are not representable in glTF.
func (e *Document) ProjectablePrimitives() ([]rw4.Primitive, int) {
	if e == nil {
		return nil, 0
	}
	primitives := make([]rw4.Primitive, 0)
	skippedDrawCount := 0
	for meshIndex, mesh := range e.Meshes {
		for drawIndex, draw := range mesh.Draws {
			primitive, err := e.primitive(meshIndex, drawIndex, mesh, draw)
			if err != nil {
				skippedDrawCount++
				continue
			}
			primitives = append(primitives, primitive)
		}
	}
	return primitives, skippedDrawCount
}

func (e *Document) primitive(meshIndex, drawIndex int, mesh Mesh, draw Draw) (rw4.Primitive, error) {
	if draw.PrimitiveType != 4 {
		return rw4.Primitive{}, fmt.Errorf("primitiveType: got %d, want 4", draw.PrimitiveType)
	}
	if draw.BufferIndex >= uint32(len(mesh.Buffers)) {
		return rw4.Primitive{}, fmt.Errorf("bufferIndex: got %d, buffers %d", draw.BufferIndex, len(mesh.Buffers))
	}
	positionStream, isFound := findStream(mesh.Streams, 1, 0)
	if !isFound || positionStream.Format != 3 || positionStream.Stride != 12 {
		return rw4.Primitive{}, fmt.Errorf("positionMissing")
	}
	primitive := rw4.Primitive{
		Name:      fmt.Sprintf("gmsh_mesh_%d_draw_%d", meshIndex, drawIndex),
		Positions: readFloat3Stream(positionStream),
	}
	normalStream, isFound := findStream(mesh.Streams, 2, 0)
	if isFound {
		if normalStream.Format == 7 && normalStream.Stride == 4 {
			primitive.Normals = readPackedVectorStream(normalStream)
		} else if normalStream.Format == 3 && normalStream.Stride == 12 {
			primitive.Normals = readFloat3Stream(normalStream)
		}
	}
	texCoordStream, isFound := findStream(mesh.Streams, 8, 0)
	if isFound && texCoordStream.Format == 2 && texCoordStream.Stride == 8 {
		primitive.TexCoords = readFloat2Stream(texCoordStream)
	}
	if len(primitive.Normals) != 0 && len(primitive.Normals) != len(primitive.Positions) {
		return rw4.Primitive{}, fmt.Errorf("normalCount: got %d, positions %d", len(primitive.Normals), len(primitive.Positions))
	}
	if len(primitive.TexCoords) != 0 && len(primitive.TexCoords) != len(primitive.Positions) {
		return rw4.Primitive{}, fmt.Errorf("texCoordCount: got %d, positions %d", len(primitive.TexCoords), len(primitive.Positions))
	}
	indices, err := readIndices(mesh.Buffers[draw.BufferIndex])
	if err != nil {
		return rw4.Primitive{}, fmt.Errorf("indices: %w", err)
	}
	if draw.FirstIndex > draw.EndIndex || draw.EndIndex > uint32(len(indices)) {
		return rw4.Primitive{}, fmt.Errorf("indexRange: %d:%d exceeds %d", draw.FirstIndex, draw.EndIndex, len(indices))
	}
	primitive.Indices = append([]uint32(nil), indices[draw.FirstIndex:draw.EndIndex]...)
	if len(primitive.Indices)%3 != 0 {
		return rw4.Primitive{}, fmt.Errorf("triangleCount: %d indices", len(primitive.Indices))
	}
	for indexPosition, vertexIndex := range primitive.Indices {
		if vertexIndex >= uint32(len(primitive.Positions)) {
			return rw4.Primitive{}, fmt.Errorf("vertexIndex[%d]: %d exceeds %d", indexPosition, vertexIndex, len(primitive.Positions))
		}
	}
	shaderIndex, isFound := e.drawShader(meshIndex, mesh, draw)
	if isFound {
		primitive.Material = &rw4.Material{Name: fmt.Sprintf("gmsh_shader_%d", shaderIndex)}
	}
	return primitive, nil
}

func (e *Document) drawShader(meshIndex int, mesh Mesh, draw Draw) (uint16, bool) {
	if meshIndex >= len(e.MaterialSets) {
		return 0, false
	}
	lookupStream, isFound := findStream(mesh.Streams, 21, 0)
	if !isFound || lookupStream.Stride != 4 || draw.MaterialIndex >= lookupStream.Count {
		return 0, false
	}
	offset := int(draw.MaterialIndex) * 4
	materialIndex := binary.LittleEndian.Uint32(lookupStream.Data[offset:])
	materials := e.MaterialSets[meshIndex]
	if materialIndex >= uint32(len(materials)) {
		return 0, false
	}
	return materials[materialIndex].ShaderIndex, true
}

func findStream(streams []Stream, usage, usageIndex uint8) (Stream, bool) {
	for _, stream := range streams {
		if stream.Usage == usage && stream.UsageIndex == usageIndex {
			return stream, true
		}
	}
	return Stream{}, false
}

func readFloat3Stream(stream Stream) [][3]float32 {
	coordinates := make([][3]float32, stream.Count)
	for coordinateIndex := range coordinates {
		offset := coordinateIndex * int(stream.Stride)
		coordinates[coordinateIndex] = [3]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(stream.Data[offset:])),
			math.Float32frombits(binary.LittleEndian.Uint32(stream.Data[offset+4:])),
			math.Float32frombits(binary.LittleEndian.Uint32(stream.Data[offset+8:])),
		}
	}
	return coordinates
}

func readFloat2Stream(stream Stream) [][2]float32 {
	coordinates := make([][2]float32, stream.Count)
	for coordinateIndex := range coordinates {
		offset := coordinateIndex * int(stream.Stride)
		coordinates[coordinateIndex] = [2]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(stream.Data[offset:])),
			math.Float32frombits(binary.LittleEndian.Uint32(stream.Data[offset+4:])),
		}
	}
	return coordinates
}

func readPackedVectorStream(stream Stream) [][3]float32 {
	coordinates := make([][3]float32, stream.Count)
	for coordinateIndex := range coordinates {
		offset := coordinateIndex * int(stream.Stride)
		coordinates[coordinateIndex] = [3]float32{
			float32(stream.Data[offset])/127 - 1,
			float32(stream.Data[offset+1])/127 - 1,
			float32(stream.Data[offset+2])/127 - 1,
		}
	}
	return coordinates
}

func readIndices(buffer Buffer) ([]uint32, error) {
	indices := make([]uint32, buffer.Count)
	switch buffer.Stride {
	case 2:
		for indexPosition := range indices {
			indices[indexPosition] = uint32(binary.LittleEndian.Uint16(buffer.Data[indexPosition*2:]))
		}
	case 4:
		for indexPosition := range indices {
			indices[indexPosition] = binary.LittleEndian.Uint32(buffer.Data[indexPosition*4:])
		}
	default:
		return nil, fmt.Errorf("indexStride: got %d, want 2 or 4", buffer.Stride)
	}
	return indices, nil
}

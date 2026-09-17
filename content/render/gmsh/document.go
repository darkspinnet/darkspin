// Package gmsh decodes Game's game-mesh resource format.
package gmsh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ResourceType is the DBPF type dispatched to Game's GMSH loader.
const ResourceType = 0x01C135DA

// Document is one complete GMSH payload.
type Document struct {
	Version      uint32
	Meshes       []Mesh
	MaterialSets [][]Material
}

// Mesh is one versioned geometry record.
type Mesh struct {
	Version uint16
	Streams []Stream
	Buffers []Buffer
	Draws   []Draw
}

// Draw selects one primitive range and material slot from a mesh buffer.
type Draw struct {
	PrimitiveType uint32
	BufferIndex   uint32
	FirstIndex    uint32
	EndIndex      uint32
	MaterialIndex uint32
}

// Material is the fixed-width lookup record read by Game's graphics
// resource loader after each mesh record.
type Material struct {
	Index       uint32
	ShaderIndex uint16
	ReferenceA  uint32
	ReferenceB  uint32
}

// Stream is a four-byte vertex declaration followed by a counted strided blob.
type Stream struct {
	Usage      uint8
	UsageIndex uint8
	Format     uint8
	Field03    uint8
	Count      uint32
	Stride     uint16
	Data       []byte
}

// Buffer is an index blob and its client-side remap and auxiliary data.
type Buffer struct {
	Count     uint32
	Stride    uint16
	Data      []byte
	Field     uint32
	Remaps    [][2]uint16
	Auxiliary []Blob
}

// Blob is a counted, strided byte region.
type Blob struct {
	Count  uint32
	Stride uint16
	Data   []byte
}

// Decode follows the bounds and byte order used by Game functions
// sub_473DE0, sub_7D4560, and sub_7D2AF0.
func Decode(payload []byte) (*Document, error) {
	decoder := decoder{payload: payload}
	document := &Document{}
	var err error
	document.Version, err = decoder.uint32()
	if err != nil {
		return nil, fmt.Errorf("version: %w", err)
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("versionUnsupported: %d", document.Version)
	}
	meshCount, err := decoder.uint32()
	if err != nil {
		return nil, fmt.Errorf("meshCount: %w", err)
	}
	if uint64(meshCount) > uint64(len(payload))/2 {
		return nil, fmt.Errorf("meshCountInvalid: %d", meshCount)
	}
	document.Meshes = make([]Mesh, meshCount)
	for meshIndex := range document.Meshes {
		mesh, decodeErr := decoder.mesh()
		if decodeErr != nil {
			return nil, fmt.Errorf("mesh[%d]: %w", meshIndex, decodeErr)
		}
		document.Meshes[meshIndex] = mesh
	}
	document.MaterialSets = make([][]Material, len(document.Meshes))
	for meshIndex := range document.MaterialSets {
		materials, decodeErr := decoder.materials()
		if decodeErr != nil {
			return nil, fmt.Errorf("materialSet[%d]: %w", meshIndex, decodeErr)
		}
		document.MaterialSets[meshIndex] = materials
	}
	if decoder.offset != len(payload) {
		return nil, fmt.Errorf("trailingData: %d bytes", len(payload)-decoder.offset)
	}
	return document, nil
}

// Encode rebuilds a GMSH payload without preserving the original DBPF
// compression or resource ordering.
func Encode(document *Document) ([]byte, error) {
	if document == nil {
		return nil, errors.New("nil document")
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("versionUnsupported: %d", document.Version)
	}
	var output bytes.Buffer
	err := binary.Write(&output, binary.BigEndian, document.Version)
	if err != nil {
		return nil, fmt.Errorf("versionWrite: %w", err)
	}
	err = binary.Write(&output, binary.BigEndian, uint32(len(document.Meshes)))
	if err != nil {
		return nil, fmt.Errorf("meshCountWrite: %w", err)
	}
	for meshIndex, mesh := range document.Meshes {
		err = encodeMesh(&output, mesh)
		if err != nil {
			return nil, fmt.Errorf("meshWrite[%d]: %w", meshIndex, err)
		}
	}
	if len(document.MaterialSets) != len(document.Meshes) {
		return nil, fmt.Errorf("materialSetCount: got %d, want %d", len(document.MaterialSets), len(document.Meshes))
	}
	for meshIndex, materials := range document.MaterialSets {
		err = encodeMaterials(&output, materials)
		if err != nil {
			return nil, fmt.Errorf("materialSetWrite[%d]: %w", meshIndex, err)
		}
	}
	return output.Bytes(), nil
}

func encodeMesh(output *bytes.Buffer, mesh Mesh) error {
	if mesh.Version != 3 && mesh.Version != 4 {
		return fmt.Errorf("versionUnsupported: %d", mesh.Version)
	}
	err := binary.Write(output, binary.BigEndian, mesh.Version)
	if err != nil {
		return fmt.Errorf("version: %w", err)
	}
	err = binary.Write(output, binary.BigEndian, uint32(len(mesh.Streams)))
	if err != nil {
		return fmt.Errorf("streamCount: %w", err)
	}
	for streamIndex, stream := range mesh.Streams {
		_, err = output.Write([]byte{stream.Usage, stream.UsageIndex, stream.Format, stream.Field03})
		if err != nil {
			return fmt.Errorf("streamDeclaration[%d]: %w", streamIndex, err)
		}
		err = encodeBlob(output, Blob{Count: stream.Count, Stride: stream.Stride, Data: stream.Data})
		if err != nil {
			return fmt.Errorf("streamData[%d]: %w", streamIndex, err)
		}
	}
	err = binary.Write(output, binary.BigEndian, uint32(len(mesh.Buffers)))
	if err != nil {
		return fmt.Errorf("bufferCount: %w", err)
	}
	for bufferIndex, buffer := range mesh.Buffers {
		err = encodeBlob(output, Blob{Count: buffer.Count, Stride: buffer.Stride, Data: buffer.Data})
		if err != nil {
			return fmt.Errorf("bufferData[%d]: %w", bufferIndex, err)
		}
		err = binary.Write(output, binary.BigEndian, buffer.Field)
		if err != nil {
			return fmt.Errorf("bufferField[%d]: %w", bufferIndex, err)
		}
		if len(buffer.Remaps) > math.MaxUint8 || len(buffer.Auxiliary) > math.MaxUint8 {
			return fmt.Errorf("bufferCount[%d]: remaps %d, auxiliary %d", bufferIndex, len(buffer.Remaps), len(buffer.Auxiliary))
		}
		_ = output.WriteByte(uint8(len(buffer.Remaps)))
		for remapIndex, remap := range buffer.Remaps {
			err = binary.Write(output, binary.BigEndian, remap)
			if err != nil {
				return fmt.Errorf("remap[%d:%d]: %w", bufferIndex, remapIndex, err)
			}
		}
		_ = output.WriteByte(uint8(len(buffer.Auxiliary)))
		for auxiliaryIndex, auxiliary := range buffer.Auxiliary {
			err = encodeBlob(output, auxiliary)
			if err != nil {
				return fmt.Errorf("auxiliary[%d:%d]: %w", bufferIndex, auxiliaryIndex, err)
			}
		}
	}
	err = binary.Write(output, binary.BigEndian, uint32(len(mesh.Draws)))
	if err != nil {
		return fmt.Errorf("drawCount: %w", err)
	}
	for drawIndex, draw := range mesh.Draws {
		err = binary.Write(output, binary.LittleEndian, draw)
		if err != nil {
			return fmt.Errorf("drawData[%d]: %w", drawIndex, err)
		}
	}
	return nil
}

func encodeMaterials(output *bytes.Buffer, materials []Material) error {
	err := binary.Write(output, binary.BigEndian, uint32(len(materials)))
	if err != nil {
		return fmt.Errorf("count: %w", err)
	}
	for materialIndex, material := range materials {
		fields := []any{material.Index, material.ShaderIndex, material.ReferenceA, material.ReferenceB}
		for fieldIndex, field := range fields {
			err = binary.Write(output, binary.BigEndian, field)
			if err != nil {
				return fmt.Errorf("field[%d:%d]: %w", materialIndex, fieldIndex, err)
			}
		}
	}
	return nil
}

func encodeBlob(output *bytes.Buffer, blob Blob) error {
	expectedSize := uint64(blob.Count) * uint64(blob.Stride)
	if expectedSize != uint64(len(blob.Data)) {
		return fmt.Errorf("blobSize: got %d, want %d*%d", len(blob.Data), blob.Count, blob.Stride)
	}
	err := binary.Write(output, binary.BigEndian, blob.Count)
	if err != nil {
		return fmt.Errorf("count: %w", err)
	}
	err = binary.Write(output, binary.BigEndian, blob.Stride)
	if err != nil {
		return fmt.Errorf("stride: %w", err)
	}
	_, err = output.Write(blob.Data)
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

type decoder struct {
	payload []byte
	offset  int
}

func (e *decoder) mesh() (Mesh, error) {
	mesh := Mesh{}
	var err error
	mesh.Version, err = e.uint16()
	if err != nil {
		return mesh, fmt.Errorf("version: %w", err)
	}
	if mesh.Version != 3 && mesh.Version != 4 {
		return mesh, fmt.Errorf("versionUnsupported: %d", mesh.Version)
	}
	streamCount, err := e.uint32()
	if err != nil {
		return mesh, fmt.Errorf("streamCount: %w", err)
	}
	mesh.Streams = make([]Stream, streamCount)
	for streamIndex := range mesh.Streams {
		declaration, readErr := e.bytes(4)
		if readErr != nil {
			return mesh, fmt.Errorf("streamDeclaration[%d]: %w", streamIndex, readErr)
		}
		blob, readErr := e.blob()
		if readErr != nil {
			return mesh, fmt.Errorf("streamData[%d]: %w", streamIndex, readErr)
		}
		mesh.Streams[streamIndex] = Stream{
			Usage: declaration[0], UsageIndex: declaration[1], Format: declaration[2], Field03: declaration[3],
			Count: blob.Count, Stride: blob.Stride, Data: blob.Data,
		}
	}
	bufferCount, err := e.uint32()
	if err != nil {
		return mesh, fmt.Errorf("bufferCount: %w", err)
	}
	mesh.Buffers = make([]Buffer, bufferCount)
	for bufferIndex := range mesh.Buffers {
		blob, readErr := e.blob()
		if readErr != nil {
			return mesh, fmt.Errorf("bufferData[%d]: %w", bufferIndex, readErr)
		}
		buffer := Buffer{Count: blob.Count, Stride: blob.Stride, Data: blob.Data}
		buffer.Field, readErr = e.uint32()
		if readErr != nil {
			return mesh, fmt.Errorf("bufferField[%d]: %w", bufferIndex, readErr)
		}
		remapCount, readErr := e.uint8()
		if readErr != nil {
			return mesh, fmt.Errorf("remapCount[%d]: %w", bufferIndex, readErr)
		}
		buffer.Remaps = make([][2]uint16, remapCount)
		for remapIndex := range buffer.Remaps {
			left, remapErr := e.uint16()
			if remapErr != nil {
				return mesh, fmt.Errorf("remapLeft[%d:%d]: %w", bufferIndex, remapIndex, remapErr)
			}
			right, remapErr := e.uint16()
			if remapErr != nil {
				return mesh, fmt.Errorf("remapRight[%d:%d]: %w", bufferIndex, remapIndex, remapErr)
			}
			buffer.Remaps[remapIndex] = [2]uint16{left, right}
		}
		auxiliaryCount, readErr := e.uint8()
		if readErr != nil {
			return mesh, fmt.Errorf("auxiliaryCount[%d]: %w", bufferIndex, readErr)
		}
		buffer.Auxiliary = make([]Blob, auxiliaryCount)
		for auxiliaryIndex := range buffer.Auxiliary {
			buffer.Auxiliary[auxiliaryIndex], readErr = e.blob()
			if readErr != nil {
				return mesh, fmt.Errorf("auxiliary[%d:%d]: %w", bufferIndex, auxiliaryIndex, readErr)
			}
		}
		mesh.Buffers[bufferIndex] = buffer
	}
	drawCount, err := e.uint32()
	if err != nil {
		return mesh, fmt.Errorf("drawCount: %w", err)
	}
	mesh.Draws = make([]Draw, drawCount)
	for drawIndex := range mesh.Draws {
		data, readErr := e.bytes(20)
		if readErr != nil {
			return mesh, fmt.Errorf("drawData[%d]: %w", drawIndex, readErr)
		}
		mesh.Draws[drawIndex] = Draw{
			PrimitiveType: binary.LittleEndian.Uint32(data[0:4]),
			BufferIndex:   binary.LittleEndian.Uint32(data[4:8]),
			FirstIndex:    binary.LittleEndian.Uint32(data[8:12]),
			EndIndex:      binary.LittleEndian.Uint32(data[12:16]),
			MaterialIndex: binary.LittleEndian.Uint32(data[16:20]),
		}
	}
	return mesh, nil
}

func (e *decoder) materials() ([]Material, error) {
	materialCount, err := e.uint32()
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}
	if uint64(materialCount)*14 > uint64(len(e.payload)-e.offset) {
		return nil, fmt.Errorf("countInvalid: %d", materialCount)
	}
	materials := make([]Material, materialCount)
	for materialIndex := range materials {
		material := Material{}
		material.Index, err = e.uint32()
		if err == nil {
			material.ShaderIndex, err = e.uint16()
		}
		if err == nil {
			material.ReferenceA, err = e.uint32()
		}
		if err == nil {
			material.ReferenceB, err = e.uint32()
		}
		if err != nil {
			return nil, fmt.Errorf("material[%d]: %w", materialIndex, err)
		}
		materials[materialIndex] = material
	}
	return materials, nil
}

func (e *decoder) blob() (Blob, error) {
	count, err := e.uint32()
	if err != nil {
		return Blob{}, fmt.Errorf("count: %w", err)
	}
	stride, err := e.uint16()
	if err != nil {
		return Blob{}, fmt.Errorf("stride: %w", err)
	}
	dataSize := uint64(count) * uint64(stride)
	if dataSize > uint64(math.MaxInt) {
		return Blob{}, fmt.Errorf("sizeOverflow: %d*%d", count, stride)
	}
	data, err := e.bytes(int(dataSize))
	if err != nil {
		return Blob{}, fmt.Errorf("data: %w", err)
	}
	return Blob{Count: count, Stride: stride, Data: data}, nil
}

func (e *decoder) bytes(count int) ([]byte, error) {
	if count < 0 || e.offset > len(e.payload)-count {
		return nil, errors.New("unexpected end of resource")
	}
	data := append([]byte(nil), e.payload[e.offset:e.offset+count]...)
	e.offset += count
	return data, nil
}

func (e *decoder) uint8() (uint8, error) {
	data, err := e.bytes(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (e *decoder) uint16() (uint16, error) {
	data, err := e.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (e *decoder) uint32() (uint32, error) {
	data, err := e.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}

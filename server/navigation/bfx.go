package navigation

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	bfxEnvelopeSize  = uint64(0x18)
	bfxImageOffset   = uint64(0x18)
	bfxLayerOffset   = uint64(0x30)
	bfxLayerHeader   = uint64(0x13c)
	bfxPolygonPrefix = uint64(0x34)
	bfxEdgeSize      = uint64(0x18)
	bfxSpatialPrefix = uint64(0x1c)
	bfxEnvelope      = uint32(2)
	bfxImageVersion  = uint32(0x00010000)
	bfxImageHeader   = uint32(0x1c)
	bfxMaximumLayer  = uint32(32)
	bfxMaximumEdge   = uint32(127)
	bfxUnloadedGraph = uint32(0xffff)
	bfxPlanarEpsilon = float32(0.001)
	bfxRadiusEpsilon = float32(0.00002)
	bfxTurnEpsilon   = float32(0.00000001)
)

type Vec3 struct {
	X float32
	Y float32
	Z float32
}

type Footprint struct {
	PlanLayer  uint8
	Radius     float32
	StepHeight float32
	Height     float32
}

type LayerInfo struct {
	PlanLayer        uint8
	Radius           float32
	StepHeight       float32
	Height           float32
	BoundsMin        Vec3
	BoundsMax        Vec3
	PolygonCount     int
	ComponentCount   uint32
	SpatialCellCount int
}

type Edge struct {
	NeighborOffset uint32
	Vertex         Vec3
	Flags          uint32
	TraversalCost  uint32
}

type Polygon struct {
	Offset      uint32
	Center      Vec3
	Radius      float32
	UserData    uint32
	Meta0       uint32
	Meta1       uint32
	ComponentID uint32
	Edges       []Edge
}

type layer struct {
	info                   LayerInfo
	polygons               []Polygon
	polygonIndexesByOffset map[uint32]int
	spatial                spatialIndex
}

type Mesh struct {
	layers []layer
}

func ParseBFX(data []byte) (*Mesh, error) {
	if len(data) < int(bfxLayerOffset) {
		return nil, errors.New("bfx header truncated")
	}
	size := uint64(len(data))
	if readU32(data, 0x00) != 0 || readU32(data, 0x04) != bfxEnvelope ||
		uint64(readU32(data, 0x08)) != size-bfxEnvelopeSize ||
		readU32(data, 0x10) != 0 || readU32(data, 0x14) != 0 {
		return nil, errors.New("bfx envelope invalid")
	}
	if readU32(data, 0x18) != bfxImageVersion ||
		uint64(readU32(data, 0x1c)) != size-0x24 ||
		readU32(data, 0x20) != 0 || readU32(data, 0x24) != 0 ||
		readU32(data, 0x28) != bfxImageHeader {
		return nil, errors.New("bfx image invalid")
	}
	layerCount := readU32(data, 0x2c)
	if layerCount == 0 || layerCount > bfxMaximumLayer {
		return nil, errors.New("bfx layer count invalid")
	}
	mesh := &Mesh{layers: make([]layer, 0, layerCount)}
	offset := bfxLayerOffset
	for layerIndex := uint32(0); layerIndex < layerCount; layerIndex++ {
		parsed, next, err := parseLayer(data, offset, layerIndex)
		if err != nil {
			return nil, fmt.Errorf("layer[%d]: %w", layerIndex, err)
		}
		mesh.layers = append(mesh.layers, parsed)
		offset = next
	}
	if offset != size {
		return nil, errors.New("bfx trailing data")
	}
	return mesh, nil
}

func parseLayer(data []byte, offset uint64, layerIndex uint32) (layer, uint64, error) {
	size := uint64(len(data))
	if !hasRange(size, offset, bfxLayerHeader) {
		return layer{}, 0, errors.New("header truncated")
	}
	if readU32At(data, offset) != bfxImageHeader || readU32At(data, offset+4) != layerIndex {
		return layer{}, 0, errors.New("header identity invalid")
	}
	arenaSize := uint64(readU32At(data, offset+8))
	layerSize := uint64(readU32At(data, offset+0x0c))
	if layerSize < bfxLayerHeader+bfxSpatialPrefix ||
		!hasRange(size, offset, layerSize) ||
		arenaSize > layerSize-bfxLayerHeader-bfxSpatialPrefix {
		return layer{}, 0, errors.New("size invalid")
	}
	buildScale := readF32At(data, offset+0x10)
	buildQuantum := readF32At(data, offset+0x14)
	radius := readF32At(data, offset+0x18)
	stepHeight := readF32At(data, offset+0x1c)
	height := readF32At(data, offset+0x20)
	boundsMin := readVec3At(data, offset+0x24)
	boundsMax := readVec3At(data, offset+0x30)
	if !isFinitePositive(buildScale) || !isFinitePositive(buildQuantum) ||
		!isFinitePositive(radius) || !isFinitePositive(stepHeight) ||
		!isFinitePositive(height) || !isFinite(boundsMin) || !isFinite(boundsMax) ||
		boundsMin.X > boundsMax.X || boundsMin.Y > boundsMax.Y || boundsMin.Z > boundsMax.Z {
		return layer{}, 0, errors.New("shape invalid")
	}
	for zeroOffset := offset + 0x3c; zeroOffset < offset+0xbc; zeroOffset++ {
		if data[zeroOffset] != 0 {
			return layer{}, 0, errors.New("zero table invalid")
		}
	}
	arenaOffset := offset + bfxLayerHeader
	arenaEnd := arenaOffset + arenaSize
	polygons := make([]Polygon, 0)
	polygonIndexesByOffset := make(map[uint32]int)
	cursor := arenaOffset
	for cursor < arenaEnd {
		polygon, next, err := parsePolygon(data, offset, cursor, arenaEnd, layerIndex)
		if err != nil {
			return layer{}, 0, fmt.Errorf("polygon[%#x]: %w", cursor-offset, err)
		}
		polygonOffset := uint32(cursor - offset)
		polygon.Offset = polygonOffset
		polygonIndexesByOffset[polygonOffset] = len(polygons)
		polygons = append(polygons, polygon)
		cursor = next
	}
	if cursor != arenaEnd || len(polygons) == 0 {
		return layer{}, 0, errors.New("polygon arena invalid")
	}
	err := validateLinks(polygons, polygonIndexesByOffset)
	if err != nil {
		return layer{}, 0, fmt.Errorf("links: %w", err)
	}
	componentCount := assignComponents(polygons, polygonIndexesByOffset)
	spatial, err := buildSpatialIndex(polygons)
	if err != nil {
		return layer{}, 0, fmt.Errorf("spatialIndex: %w", err)
	}
	spatialOffset := arenaEnd
	if !hasRange(size, spatialOffset, bfxSpatialPrefix) {
		return layer{}, 0, errors.New("spatial prefix truncated")
	}
	spatialMin := readVec3At(data, spatialOffset)
	spatialMax := readVec3At(data, spatialOffset+0x0c)
	treeSize := uint64(readU32At(data, spatialOffset+0x18))
	if !isFinite(spatialMin) || !isFinite(spatialMax) ||
		spatialMin.X > spatialMax.X || spatialMin.Y > spatialMax.Y || spatialMin.Z > spatialMax.Z ||
		bfxLayerHeader+arenaSize+bfxSpatialPrefix+treeSize != layerSize {
		return layer{}, 0, errors.New("spatial image invalid")
	}
	return layer{
		info: LayerInfo{
			PlanLayer: uint8(layerIndex), Radius: radius, StepHeight: stepHeight, Height: height,
			BoundsMin: boundsMin, BoundsMax: boundsMax, PolygonCount: len(polygons),
			ComponentCount: componentCount, SpatialCellCount: len(spatial.polygonIndicesByCell),
		},
		polygons: polygons, polygonIndexesByOffset: polygonIndexesByOffset, spatial: spatial,
	}, offset + layerSize, nil
}

func parsePolygon(
	data []byte, layerOffset uint64, offset uint64, arenaEnd uint64, layerIndex uint32,
) (Polygon, uint64, error) {
	if !hasRange(arenaEnd, offset, bfxPolygonPrefix) {
		return Polygon{}, 0, errors.New("prefix truncated")
	}
	for runtimeOffset := uint64(0); runtimeOffset < 0x10; runtimeOffset += 4 {
		if readU32At(data, offset+runtimeOffset) != 0 {
			return Polygon{}, 0, errors.New("runtime prefix nonzero")
		}
	}
	center := readVec3At(data, offset+0x10)
	radius := readF32At(data, offset+0x1c)
	meta0 := readU32At(data, offset+0x28)
	edgeCount := meta0 & 0x7f
	embeddedLayer := (meta0 >> 23) & 0x1f
	isDynamic := (meta0>>30)&1 != 0
	if edgeCount < 3 || edgeCount > bfxMaximumEdge || embeddedLayer != layerIndex || isDynamic ||
		!isFinite(center) || radius < 0 || !isFiniteScalar(radius) {
		return Polygon{}, 0, errors.New("metadata invalid")
	}
	polygonSize := bfxPolygonPrefix + uint64(edgeCount)*bfxEdgeSize
	if !hasRange(arenaEnd, offset, polygonSize) {
		return Polygon{}, 0, errors.New("edges truncated")
	}
	graphIdentity := (meta0 >> 7) & 0xffff
	if graphIdentity != bfxUnloadedGraph {
		return Polygon{}, 0, errors.New("loaded graph identity")
	}
	edges := make([]Edge, 0, edgeCount)
	for edgeIndex := uint32(0); edgeIndex < edgeCount; edgeIndex++ {
		edgeOffset := offset + bfxPolygonPrefix + uint64(edgeIndex)*bfxEdgeSize
		edge := Edge{
			NeighborOffset: readU32At(data, edgeOffset),
			Vertex:         readVec3At(data, edgeOffset+4),
			Flags:          readU32At(data, edgeOffset+0x10),
			TraversalCost:  readU32At(data, edgeOffset+0x14),
		}
		if !isFinite(edge.Vertex) ||
			(edge.NeighborOffset == 0) != (edge.TraversalCost == 0) {
			return Polygon{}, 0, errors.New("edge invalid")
		}
		edges = append(edges, edge)
	}
	polygon := Polygon{
		Center: center, Radius: radius, UserData: readU32At(data, offset+0x20),
		Meta0: meta0, Meta1: readU32At(data, offset+0x2c), Edges: edges,
	}
	err := validatePolygonGeometry(polygon)
	if err != nil {
		return Polygon{}, 0, fmt.Errorf("geometry: %w", err)
	}
	_ = layerOffset
	return polygon, offset + polygonSize, nil
}

func validateLinks(polygons []Polygon, polygonIndexesByOffset map[uint32]int) error {
	for polygonIndex := range polygons {
		polygon := polygons[polygonIndex]
		for edgeIndex := range polygon.Edges {
			edge := polygon.Edges[edgeIndex]
			if edge.NeighborOffset == 0 {
				continue
			}
			neighborIndex, isFound := polygonIndexesByOffset[edge.NeighborOffset]
			if !isFound || neighborIndex == polygonIndex {
				return errors.New("neighbor invalid")
			}
			nextVertex := polygon.Edges[(edgeIndex+1)%len(polygon.Edges)].Vertex
			reciprocalCount := 0
			neighbor := polygons[neighborIndex]
			for neighborEdgeIndex := range neighbor.Edges {
				neighborEdge := neighbor.Edges[neighborEdgeIndex]
				neighborNext := neighbor.Edges[(neighborEdgeIndex+1)%len(neighbor.Edges)].Vertex
				if neighborEdge.NeighborOffset == polygon.Offset &&
					neighborEdge.Vertex == nextVertex && neighborNext == edge.Vertex {
					reciprocalCount++
				}
			}
			if reciprocalCount != 1 {
				return errors.New("neighbor not reciprocal")
			}
		}
	}
	return nil
}

func assignComponents(polygons []Polygon, polygonIndexesByOffset map[uint32]int) uint32 {
	componentID := uint32(0)
	for polygonIndex := range polygons {
		if polygons[polygonIndex].ComponentID != 0 {
			continue
		}
		componentID++
		polygons[polygonIndex].ComponentID = componentID
		queue := []int{polygonIndex}
		for len(queue) > 0 {
			currentIndex := queue[0]
			queue = queue[1:]
			for _, edge := range polygons[currentIndex].Edges {
				if edge.NeighborOffset == 0 {
					continue
				}
				neighborIndex := polygonIndexesByOffset[edge.NeighborOffset]
				if polygons[neighborIndex].ComponentID != 0 {
					continue
				}
				polygons[neighborIndex].ComponentID = componentID
				queue = append(queue, neighborIndex)
			}
		}
	}
	return componentID
}

func (m *Mesh) LayerInfo(planLayer uint8) (LayerInfo, bool) {
	if m == nil || int(planLayer) >= len(m.layers) {
		return LayerInfo{}, false
	}
	return m.layers[planLayer].info, true
}

func (m *Mesh) SelectLayer(radius float32, height float32) (uint8, bool) {
	if m == nil || !isFinitePositive(radius) || !isFinitePositive(height) {
		return 0, false
	}
	for layerIndex := range m.layers {
		info := m.layers[layerIndex].info
		if info.Radius >= radius && info.Height >= height {
			return uint8(layerIndex), true
		}
	}
	return 0, false
}

// SelectLargestLayer returns the widest authored layer that accepts the actor's height.
func (m *Mesh) SelectLargestLayer(height float32) (uint8, bool) {
	if m == nil || !isFinitePositive(height) {
		return 0, false
	}
	selectedLayer := uint8(0)
	selectedRadius := float32(0)
	isSelected := false
	for layerIndex := range m.layers {
		info := m.layers[layerIndex].info
		if info.Height < height || (isSelected && info.Radius <= selectedRadius) {
			continue
		}
		selectedLayer = uint8(layerIndex)
		selectedRadius = info.Radius
		isSelected = true
	}
	return selectedLayer, isSelected
}

func hasRange(size uint64, offset uint64, length uint64) bool {
	return offset <= size && length <= size-offset
}

func readU32(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset : offset+4])
}

func readU32At(data []byte, offset uint64) uint32 {
	return binary.LittleEndian.Uint32(data[offset : offset+4])
}

func readF32At(data []byte, offset uint64) float32 {
	return math.Float32frombits(readU32At(data, offset))
}

func readVec3At(data []byte, offset uint64) Vec3 {
	return Vec3{
		X: readF32At(data, offset),
		Y: readF32At(data, offset+4),
		Z: readF32At(data, offset+8),
	}
}

func isFinite(position Vec3) bool {
	return isFiniteScalar(position.X) && isFiniteScalar(position.Y) && isFiniteScalar(position.Z)
}

func isFinitePositive(scalar float32) bool {
	return scalar > 0 && isFiniteScalar(scalar)
}

func isFiniteScalar(scalar float32) bool {
	return !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}

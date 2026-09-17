package navigation

import (
	"errors"
	"fmt"
	"math"
)

const navigationSpatialCellSize = float32(16)
const navigationMaximumCellsPerPolygon = int64(1 << 20)

type spatialCell struct {
	X int32
	Y int32
}

type spatialIndex struct {
	polygonIndicesByCell map[spatialCell][]int
	minimumCell          spatialCell
	maximumCell          spatialCell
}

func buildSpatialIndex(polygons []Polygon) (spatialIndex, error) {
	if len(polygons) == 0 {
		return spatialIndex{}, errors.New("navigation spatial polygons empty")
	}
	index := spatialIndex{
		polygonIndicesByCell: make(map[spatialCell][]int),
		minimumCell:          spatialCell{X: math.MaxInt32, Y: math.MaxInt32},
		maximumCell:          spatialCell{X: math.MinInt32, Y: math.MinInt32},
	}
	for polygonIndex, polygon := range polygons {
		minimum := polygon.Edges[0].Vertex
		maximum := minimum
		for _, edge := range polygon.Edges[1:] {
			minimum.X = min(minimum.X, edge.Vertex.X)
			minimum.Y = min(minimum.Y, edge.Vertex.Y)
			maximum.X = max(maximum.X, edge.Vertex.X)
			maximum.Y = max(maximum.Y, edge.Vertex.Y)
		}
		minimumCell, err := navigationSpatialCell(minimum.X, minimum.Y)
		if err != nil {
			return spatialIndex{}, fmt.Errorf("spatialMinimum: %w", err)
		}
		maximumCell, err := navigationSpatialCell(maximum.X, maximum.Y)
		if err != nil {
			return spatialIndex{}, fmt.Errorf("spatialMaximum: %w", err)
		}
		cellCountX := int64(maximumCell.X) - int64(minimumCell.X) + 1
		cellCountY := int64(maximumCell.Y) - int64(minimumCell.Y) + 1
		cellCount := cellCountX * cellCountY
		if cellCount <= 0 || cellCount > navigationMaximumCellsPerPolygon {
			return spatialIndex{}, errors.New("navigation spatial span invalid")
		}
		index.minimumCell.X = min(index.minimumCell.X, minimumCell.X)
		index.minimumCell.Y = min(index.minimumCell.Y, minimumCell.Y)
		index.maximumCell.X = max(index.maximumCell.X, maximumCell.X)
		index.maximumCell.Y = max(index.maximumCell.Y, maximumCell.Y)
		for cellX := int64(minimumCell.X); cellX <= int64(maximumCell.X); cellX++ {
			for cellY := int64(minimumCell.Y); cellY <= int64(maximumCell.Y); cellY++ {
				cell := spatialCell{X: int32(cellX), Y: int32(cellY)}
				index.polygonIndicesByCell[cell] = append(
					index.polygonIndicesByCell[cell], polygonIndex,
				)
			}
		}
	}
	return index, nil
}

func navigationSpatialCell(x float32, y float32) (spatialCell, error) {
	cellX := math.Floor(float64(x / navigationSpatialCellSize))
	cellY := math.Floor(float64(y / navigationSpatialCellSize))
	if cellX < math.MinInt32 || cellX > math.MaxInt32 ||
		cellY < math.MinInt32 || cellY > math.MaxInt32 {
		return spatialCell{}, errors.New("navigation spatial coordinate invalid")
	}
	return spatialCell{X: int32(cellX), Y: int32(cellY)}, nil
}

func (s spatialIndex) candidatePolygonIndexes(
	point Vec3, maximumDistance float32, polygonCount int,
) []int {
	minimumCell, err := navigationSpatialCell(
		point.X-maximumDistance, point.Y-maximumDistance,
	)
	if err != nil {
		return nil
	}
	maximumCell, err := navigationSpatialCell(
		point.X+maximumDistance, point.Y+maximumDistance,
	)
	if err != nil {
		return nil
	}
	minimumCell.X = max(minimumCell.X, s.minimumCell.X)
	minimumCell.Y = max(minimumCell.Y, s.minimumCell.Y)
	maximumCell.X = min(maximumCell.X, s.maximumCell.X)
	maximumCell.Y = min(maximumCell.Y, s.maximumCell.Y)
	if minimumCell.X > maximumCell.X || minimumCell.Y > maximumCell.Y {
		return nil
	}
	isPolygonSeen := make([]bool, polygonCount)
	polygonIndexes := make([]int, 0)
	for cellX := int64(minimumCell.X); cellX <= int64(maximumCell.X); cellX++ {
		for cellY := int64(minimumCell.Y); cellY <= int64(maximumCell.Y); cellY++ {
			cell := spatialCell{X: int32(cellX), Y: int32(cellY)}
			for _, polygonIndex := range s.polygonIndicesByCell[cell] {
				if isPolygonSeen[polygonIndex] {
					continue
				}
				isPolygonSeen[polygonIndex] = true
				polygonIndexes = append(polygonIndexes, polygonIndex)
			}
		}
	}
	return polygonIndexes
}

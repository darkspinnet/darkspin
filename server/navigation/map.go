package navigation

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

type MapInfo struct {
	PlanLayer   uint8   `json:"plan_layer"`
	ComponentID uint32  `json:"component_id,omitempty"`
	BoundsMin   Vec3    `json:"bounds_min"`
	BoundsMax   Vec3    `json:"bounds_max"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Padding     int     `json:"padding"`
	Scale       float32 `json:"scale"`
	OffsetX     float32 `json:"offset_x"`
	OffsetY     float32 `json:"offset_y"`
}

type MapSection struct {
	Info  MapInfo
	Image *image.RGBA
}

func (m *Mesh) RenderTopDownMap(
	planLayer uint8, width int, height int, padding int,
) (*image.RGBA, MapInfo, error) {
	if m == nil || int(planLayer) >= len(m.layers) || width <= padding*2 ||
		height <= padding*2 || padding < 0 {
		return nil, MapInfo{}, errors.New("navigation map invalid")
	}
	selected := m.layers[planLayer]
	polygon := selected.polygons
	return renderTopDownPolygons(
		planLayer, 0, polygon, selected.info.BoundsMin, selected.info.BoundsMax,
		width, height, padding,
	)
}

func (m *Mesh) RenderTopDownSections(
	planLayer uint8, width int, height int, padding int,
) ([]MapSection, error) {
	if m == nil || int(planLayer) >= len(m.layers) {
		return nil, errors.New("navigation sections invalid")
	}
	selected := m.layers[planLayer]
	sections := make([]MapSection, 0, selected.info.ComponentCount)
	for componentID := uint32(1); componentID <= selected.info.ComponentCount; componentID++ {
		polygons := make([]Polygon, 0)
		for _, polygon := range selected.polygons {
			if polygon.ComponentID == componentID {
				polygons = append(polygons, polygon)
			}
		}
		boundsMin, boundsMax, err := mapPolygonBounds(polygons)
		if err != nil {
			return nil, fmt.Errorf("sectionBounds[%d]: %w", componentID, err)
		}
		minimap, mapInfo, err := renderTopDownPolygons(
			planLayer, componentID, polygons, boundsMin, boundsMax,
			width, height, padding,
		)
		if err != nil {
			return nil, fmt.Errorf("sectionRender[%d]: %w", componentID, err)
		}
		sections = append(sections, MapSection{Info: mapInfo, Image: minimap})
	}
	return sections, nil
}

func renderTopDownPolygons(
	planLayer uint8, componentID uint32, polygons []Polygon,
	boundsMin Vec3, boundsMax Vec3, width int, height int, padding int,
) (*image.RGBA, MapInfo, error) {
	if len(polygons) == 0 || width <= padding*2 || height <= padding*2 || padding < 0 {
		return nil, MapInfo{}, errors.New("navigation map invalid")
	}
	rangeX := boundsMax.X - boundsMin.X
	rangeY := boundsMax.Y - boundsMin.Y
	if rangeX <= 0 || rangeY <= 0 {
		return nil, MapInfo{}, errors.New("navigation map bounds invalid")
	}
	scaleX := float32(width-padding*2) / rangeX
	scaleY := float32(height-padding*2) / rangeY
	mapScale := min(scaleX, scaleY)
	drawWidth := rangeX * mapScale
	drawHeight := rangeY * mapScale
	offsetX := (float32(width) - drawWidth) / 2
	offsetY := (float32(height) - drawHeight) / 2
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for _, polygon := range polygons {
		points := make([]image.Point, len(polygon.Edges))
		for edgeIndex, edge := range polygon.Edges {
			x := offsetX + (edge.Vertex.X-boundsMin.X)*mapScale
			y := offsetY + (boundsMax.Y-edge.Vertex.Y)*mapScale
			points[edgeIndex] = image.Pt(int(math.Round(float64(x))), int(math.Round(float64(y))))
		}
		fill := navigationMapColor(polygon.Center.Z, boundsMin.Z, boundsMax.Z)
		fillConvexPolygon(canvas, points, fill)
		for pointIndex := range points {
			if polygon.Edges[pointIndex].NeighborOffset != 0 {
				continue
			}
			drawMapLine(
				canvas, points[pointIndex], points[(pointIndex+1)%len(points)],
				color.RGBA{R: 111, G: 223, B: 229, A: 110},
			)
		}
	}
	return canvas, MapInfo{
		PlanLayer: planLayer, ComponentID: componentID,
		BoundsMin: boundsMin, BoundsMax: boundsMax,
		Width: width, Height: height, Padding: padding, Scale: mapScale,
		OffsetX: offsetX, OffsetY: offsetY,
	}, nil
}

func mapPolygonBounds(polygons []Polygon) (Vec3, Vec3, error) {
	if len(polygons) == 0 || len(polygons[0].Edges) == 0 {
		return Vec3{}, Vec3{}, errors.New("navigation section empty")
	}
	boundsMin := polygons[0].Edges[0].Vertex
	boundsMax := boundsMin
	for _, polygon := range polygons {
		for _, edge := range polygon.Edges {
			boundsMin.X = min(boundsMin.X, edge.Vertex.X)
			boundsMin.Y = min(boundsMin.Y, edge.Vertex.Y)
			boundsMin.Z = min(boundsMin.Z, edge.Vertex.Z)
			boundsMax.X = max(boundsMax.X, edge.Vertex.X)
			boundsMax.Y = max(boundsMax.Y, edge.Vertex.Y)
			boundsMax.Z = max(boundsMax.Z, edge.Vertex.Z)
		}
	}
	return boundsMin, boundsMax, nil
}

func (i MapInfo) ProjectTopDown(point Vec3) (image.Point, bool) {
	if i.Width <= 0 || i.Height <= 0 || i.Scale <= 0 ||
		point.X < i.BoundsMin.X || point.X > i.BoundsMax.X ||
		point.Y < i.BoundsMin.Y || point.Y > i.BoundsMax.Y {
		return image.Point{}, false
	}
	x := i.OffsetX + (point.X-i.BoundsMin.X)*i.Scale
	y := i.OffsetY + (i.BoundsMax.Y-point.Y)*i.Scale
	return image.Pt(int(math.Round(float64(x))), int(math.Round(float64(y)))), true
}

func navigationMapColor(z float32, minimum float32, maximum float32) color.RGBA {
	ratio := float32(0)
	if maximum > minimum {
		ratio = (z - minimum) / (maximum - minimum)
	}
	ratio = max(float32(0), min(float32(1), ratio))
	return color.RGBA{
		R: uint8(18 + 24*ratio), G: uint8(92 + 78*ratio),
		B: uint8(105 + 82*ratio), A: 220,
	}
}

func fillConvexPolygon(canvas *image.RGBA, points []image.Point, fill color.RGBA) {
	if len(points) < 3 {
		return
	}
	minimumY := points[0].Y
	maximumY := points[0].Y
	for _, point := range points[1:] {
		minimumY = min(minimumY, point.Y)
		maximumY = max(maximumY, point.Y)
	}
	minimumY = max(minimumY, canvas.Bounds().Min.Y)
	maximumY = min(maximumY, canvas.Bounds().Max.Y-1)
	intersection := make([]int, 0, len(points))
	for y := minimumY; y <= maximumY; y++ {
		intersection = intersection[:0]
		for pointIndex, first := range points {
			second := points[(pointIndex+1)%len(points)]
			if first.Y == second.Y ||
				y < min(first.Y, second.Y) || y >= max(first.Y, second.Y) {
				continue
			}
			x := first.X + (y-first.Y)*(second.X-first.X)/(second.Y-first.Y)
			intersection = append(intersection, x)
		}
		if len(intersection) < 2 {
			continue
		}
		minimumX := intersection[0]
		maximumX := intersection[0]
		for _, x := range intersection[1:] {
			minimumX = min(minimumX, x)
			maximumX = max(maximumX, x)
		}
		minimumX = max(minimumX, canvas.Bounds().Min.X)
		maximumX = min(maximumX, canvas.Bounds().Max.X-1)
		for x := minimumX; x <= maximumX; x++ {
			canvas.SetRGBA(x, y, fill)
		}
	}
}

func drawMapLine(canvas *image.RGBA, start image.Point, end image.Point, stroke color.RGBA) {
	deltaX := int(math.Abs(float64(end.X - start.X)))
	stepX := -1
	if start.X < end.X {
		stepX = 1
	}
	deltaY := -int(math.Abs(float64(end.Y - start.Y)))
	stepY := -1
	if start.Y < end.Y {
		stepY = 1
	}
	lineError := deltaX + deltaY
	for {
		if start.In(canvas.Bounds()) {
			canvas.SetRGBA(start.X, start.Y, stroke)
		}
		if start == end {
			return
		}
		doubleError := lineError * 2
		if doubleError >= deltaY {
			lineError += deltaY
			start.X += stepX
		}
		if doubleError <= deltaX {
			lineError += deltaX
			start.Y += stepY
		}
	}
}

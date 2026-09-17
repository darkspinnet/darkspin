package navigation

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
)

type ProjectionOptions struct {
	PlanLayer              uint8
	MaxDistance            float32
	ComponentID            uint32
	IsComponentConstrained bool
}

type Projection struct {
	Position    Vec3
	PolygonID   uint32
	ComponentID uint32
	Distance    float32
}

type PathOptions struct {
	ProjectionOptions
	MaxVisitedPolygon int
}

type Path struct {
	Start    Projection
	Goal     Projection
	Corridor []uint32
	Points   []Vec3
}

func (m *Mesh) Project(point Vec3, options ProjectionOptions) (Projection, error) {
	if m == nil || !isFinite(point) || !isFiniteScalar(options.MaxDistance) ||
		options.MaxDistance < 0 || int(options.PlanLayer) >= len(m.layers) {
		return Projection{}, errors.New("navigation projection invalid")
	}
	selected := m.layers[options.PlanLayer]
	polygonIndexes := selected.spatial.candidatePolygonIndexes(
		point, options.MaxDistance, len(selected.polygons),
	)
	return projectPolygonIndexes(selected, point, options, polygonIndexes)
}

func projectPolygonIndexes(
	selected layer, point Vec3, options ProjectionOptions, polygonIndexes []int,
) (Projection, error) {
	maximumSquared := options.MaxDistance * options.MaxDistance
	bestSquared := float32(math.Inf(1))
	best := Projection{}
	for _, polygonIndex := range polygonIndexes {
		polygon := selected.polygons[polygonIndex]
		if options.IsComponentConstrained && polygon.ComponentID != options.ComponentID {
			continue
		}
		if squaredDistance(point, polygon.Center) >
			(polygon.Radius+options.MaxDistance)*(polygon.Radius+options.MaxDistance) {
			continue
		}
		candidate := closestPointOnPolygon(point, polygon)
		candidateSquared := squaredDistance(point, candidate)
		if candidateSquared > maximumSquared {
			continue
		}
		if candidateSquared > bestSquared ||
			candidateSquared == bestSquared && polygon.Offset >= best.PolygonID {
			continue
		}
		bestSquared = candidateSquared
		best = Projection{
			Position: candidate, PolygonID: polygon.Offset, ComponentID: polygon.ComponentID,
			Distance: float32(math.Sqrt(float64(candidateSquared))),
		}
	}
	if math.IsInf(float64(bestSquared), 1) {
		return Projection{}, errors.New("navigation projection unavailable")
	}
	return best, nil
}

func (m *Mesh) IsReachable(start Projection, goal Projection, planLayer uint8) bool {
	if m == nil || int(planLayer) >= len(m.layers) || start.ComponentID == 0 ||
		goal.ComponentID == 0 || start.ComponentID != goal.ComponentID {
		return false
	}
	selected := m.layers[planLayer]
	_, isStartFound := selected.polygonIndexesByOffset[start.PolygonID]
	_, isGoalFound := selected.polygonIndexesByOffset[goal.PolygonID]
	return isStartFound && isGoalFound
}

func (m *Mesh) CreatePath(start Vec3, goal Vec3, options PathOptions) (Path, error) {
	if options.MaxVisitedPolygon <= 0 {
		return Path{}, errors.New("navigation path limit invalid")
	}
	startProjection, err := m.Project(start, options.ProjectionOptions)
	if err != nil {
		return Path{}, fmt.Errorf("pathStart: %w", err)
	}
	goalOptions := options.ProjectionOptions
	goalOptions.ComponentID = startProjection.ComponentID
	goalOptions.IsComponentConstrained = true
	goalProjection, err := m.Project(goal, goalOptions)
	if err != nil {
		return Path{}, fmt.Errorf("pathGoal: %w", err)
	}
	selected := m.layers[options.PlanLayer]
	startIndex := selected.polygonIndexesByOffset[startProjection.PolygonID]
	goalIndex := selected.polygonIndexesByOffset[goalProjection.PolygonID]
	previous, err := shortestCorridor(selected, startIndex, goalIndex, options.MaxVisitedPolygon)
	if err != nil {
		return Path{}, fmt.Errorf("pathCorridor: %w", err)
	}
	corridorIndex := []int{goalIndex}
	for corridorIndex[len(corridorIndex)-1] != startIndex {
		corridorIndex = append(corridorIndex, previous[corridorIndex[len(corridorIndex)-1]])
	}
	reverse(corridorIndex)
	corridor := make([]uint32, 0, len(corridorIndex))
	points := make([]Vec3, 0, len(corridorIndex)+1)
	portals := make([]pathPortal, 0, len(corridorIndex)-1)
	points = append(points, startProjection.Position)
	for index := 0; index+1 < len(corridorIndex); index++ {
		current := selected.polygons[corridorIndex[index]]
		nextOffset := selected.polygons[corridorIndex[index+1]].Offset
		for edgeIndex := range current.Edges {
			edge := current.Edges[edgeIndex]
			if edge.NeighborOffset != nextOffset {
				continue
			}
			nextVertex := current.Edges[(edgeIndex+1)%len(current.Edges)].Vertex
			portals = append(portals, pathPortal{start: edge.Vertex, end: nextVertex})
			points = append(points, scale(add(edge.Vertex, nextVertex), 0.5))
			break
		}
	}
	points = append(points, goalProjection.Position)
	points = straightenPath(points, portals)
	for _, polygonIndex := range corridorIndex {
		corridor = append(corridor, selected.polygons[polygonIndex].Offset)
	}
	return Path{
		Start: startProjection, Goal: goalProjection, Corridor: corridor, Points: points,
	}, nil
}

type pathNode struct {
	index int
	cost  uint64
}

type pathQueue []pathNode

func (q pathQueue) Len() int {
	return len(q)
}

func (q pathQueue) Less(i int, j int) bool {
	if q[i].cost == q[j].cost {
		return q[i].index < q[j].index
	}
	return q[i].cost < q[j].cost
}

func (q pathQueue) Swap(i int, j int) {
	q[i], q[j] = q[j], q[i]
}

func (q *pathQueue) Push(entry any) {
	*q = append(*q, entry.(pathNode))
}

func (q *pathQueue) Pop() any {
	previous := *q
	last := previous[len(previous)-1]
	*q = previous[:len(previous)-1]
	return last
}

func shortestCorridor(selected layer, startIndex int, goalIndex int, maximumVisited int) ([]int, error) {
	distanceByIndex := make([]uint64, len(selected.polygons))
	previousByIndex := make([]int, len(selected.polygons))
	for index := range distanceByIndex {
		distanceByIndex[index] = math.MaxUint64
		previousByIndex[index] = -1
	}
	distanceByIndex[startIndex] = 0
	queue := &pathQueue{{index: startIndex}}
	heap.Init(queue)
	visited := 0
	for queue.Len() > 0 && visited < maximumVisited {
		entry := heap.Pop(queue).(pathNode)
		if entry.cost != distanceByIndex[entry.index] {
			continue
		}
		visited++
		if entry.index == goalIndex {
			return previousByIndex, nil
		}
		for _, edge := range selected.polygons[entry.index].Edges {
			if edge.NeighborOffset == 0 {
				continue
			}
			neighborIndex := selected.polygonIndexesByOffset[edge.NeighborOffset]
			candidate := entry.cost + uint64(edge.TraversalCost)
			if candidate >= distanceByIndex[neighborIndex] {
				continue
			}
			distanceByIndex[neighborIndex] = candidate
			previousByIndex[neighborIndex] = entry.index
			heap.Push(queue, pathNode{index: neighborIndex, cost: candidate})
		}
	}
	return nil, errors.New("navigation path unavailable")
}

func reverse(index []int) {
	for left, right := 0, len(index)-1; left < right; left, right = left+1, right-1 {
		index[left], index[right] = index[right], index[left]
	}
}

package navigation

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"
)

// Obstacle is an upright static collision box in game coordinates. The
// selected layout owns these boxes; the cached terrain mesh remains immutable.
type Obstacle struct {
	MarkerID   uint32
	Position   Vec3
	Minimum    Vec3
	Maximum    Vec3
	Yaw        float32
	isDisabled *atomic.Bool
}

func (e *Mesh) WithObstacles(obstacles []Obstacle) (*Mesh, error) {
	if e == nil {
		return nil, errors.New("obstacle mesh unavailable")
	}
	for index, obstacle := range obstacles {
		if !isFinite(obstacle.Position) || !isFinite(obstacle.Minimum) ||
			!isFinite(obstacle.Maximum) || !isFiniteScalar(obstacle.Yaw) ||
			obstacle.Minimum.X >= obstacle.Maximum.X ||
			obstacle.Minimum.Y >= obstacle.Maximum.Y ||
			obstacle.Minimum.Z >= obstacle.Maximum.Z {
			return nil, fmt.Errorf("obstacleBounds[%d]: invalid geometry", index)
		}
	}
	result := *e
	result.obstacles = append([]Obstacle(nil), obstacles...)
	for index := range result.obstacles {
		result.obstacles[index].isDisabled = new(atomic.Bool)
	}
	return &result, nil
}

// DisableObstacle retires a placed collider without changing the shared terrain
// mesh. All actors using this layout observe the same collision lifetime.
func (e *Mesh) DisableObstacle(markerID uint32) {
	if e == nil || markerID == 0 {
		return
	}
	for _, obstacle := range e.obstacles {
		if obstacle.MarkerID == markerID && obstacle.isDisabled != nil {
			obstacle.isDisabled.Store(true)
		}
	}
}

func (e Obstacle) local(point Vec3) Vec3 {
	angle := float64(e.Yaw) * math.Pi / 180
	cosine, sine := float32(math.Cos(angle)), float32(math.Sin(angle))
	delta := subtract(point, e.Position)
	return Vec3{X: cosine*delta.X + sine*delta.Y, Y: -sine*delta.X + cosine*delta.Y, Z: delta.Z}
}

func (e Obstacle) intersects(start Vec3, end Vec3, radius float32, height float32) bool {
	if e.isDisabled != nil && e.isDisabled.Load() {
		return false
	}
	start, end = e.local(start), e.local(end)
	minimums := [3]float32{e.Minimum.X - radius, e.Minimum.Y - radius, e.Minimum.Z - height}
	maximums := [3]float32{e.Maximum.X + radius, e.Maximum.Y + radius, e.Maximum.Z}
	starts := [3]float32{start.X, start.Y, start.Z}
	ends := [3]float32{end.X, end.Y, end.Z}
	entry, exit := float32(0), float32(1)
	for axis := range starts {
		delta := ends[axis] - starts[axis]
		if math.Abs(float64(delta)) < 0.000001 {
			if starts[axis] <= minimums[axis] || starts[axis] >= maximums[axis] {
				return false
			}
			continue
		}
		first := (minimums[axis] - starts[axis]) / delta
		last := (maximums[axis] - starts[axis]) / delta
		if first > last {
			first, last = last, first
		}
		entry, exit = max(entry, first), min(exit, last)
		if entry >= exit {
			return false
		}
	}
	return entry < exit
}

func (e *Mesh) isObstructed(point Vec3, planLayer uint8) bool {
	info := e.layers[planLayer].info
	for _, obstacle := range e.obstacles {
		if obstacle.intersects(point, point, info.Radius, info.Height) {
			return true
		}
	}
	return false
}

func (e *Mesh) pathObstacles(path Path, planLayer uint8) []int {
	info := e.layers[planLayer].info
	indexes := make([]int, 0)
	for index, obstacle := range e.obstacles {
		for pointIndex := 1; pointIndex < len(path.Points); pointIndex++ {
			if obstacle.intersects(path.Points[pointIndex-1], path.Points[pointIndex], info.Radius, info.Height) {
				indexes = append(indexes, index)
				break
			}
		}
	}
	return indexes
}

type obstacleRoute struct {
	mesh              *Mesh
	options           PathOptions
	nodes             []Vec3
	paths             map[[2]int]Path
	areEdgesExamined  map[[2]int]bool
	obstacleIndexes   map[int]bool
	heights           map[int]float32
	areObstaclesAdded map[int]bool
	areContactsAdded  map[int]bool
}

// CreatePath retains terrain corridors, then searches around authored collision
// boxes when needed. Every candidate edge is itself a terrain path and is
// checked against every box. This compatibility overlay does not claim native
// topology splitting or dynamic-door lifecycle parity.
func (e *Mesh) CreatePath(start Vec3, goal Vec3, options PathOptions) (Path, error) {
	path, err := e.terrainPath(start, goal, options)
	if err != nil {
		return Path{}, fmt.Errorf("pathTerrain: %w", err)
	}
	indexes := e.pathObstacles(path, options.PlanLayer)
	if len(indexes) == 0 {
		return path, nil
	}
	options.ComponentID = path.Start.ComponentID
	options.IsComponentConstrained = true
	route := obstacleRoute{
		mesh: e, options: options, nodes: []Vec3{path.Start.Position, path.Goal.Position},
		paths: make(map[[2]int]Path), areEdgesExamined: make(map[[2]int]bool),
		obstacleIndexes: make(map[int]bool), heights: make(map[int]float32), areObstaclesAdded: make(map[int]bool),
		areContactsAdded: make(map[int]bool),
	}
	route.recordObstacles(path, indexes)
	for pass := 0; pass < len(e.obstacles); pass++ {
		if !route.addCorners() {
			break
		}
		result, isFound := route.search()
		if isFound {
			result.Start, result.Goal = path.Start, path.Goal
			return result, nil
		}
	}
	// A box corner can lie outside a narrow authored walkway. Search nearby
	// terrain portals as well rather than declaring that walkway impassable.
	// The client also searches polygon/edge connectivity (127 A59C40/A77F50);
	// this is a bounded compatibility fallback, not its full native backend.
	for pass := 0; pass < len(e.obstacles); pass++ {
		if !route.addTerrainContacts() {
			break
		}
		result, isFound := route.search()
		if isFound {
			result.Start, result.Goal = path.Start, path.Goal
			return result, nil
		}
	}
	return Path{}, errors.New("navigation obstacle route unavailable")
}

func (e *obstacleRoute) addTerrainContacts() bool {
	selected := e.mesh.layers[e.options.PlanLayer]
	padding := selected.info.Radius*2 + e.options.MaxDistance
	isAddedAny := false
	for index, obstacle := range e.mesh.obstacles {
		if !e.obstacleIndexes[index] || e.areContactsAdded[index] {
			continue
		}
		e.areContactsAdded[index] = true
		for _, polygon := range selected.polygons {
			if polygon.ComponentID != e.options.ComponentID && e.options.IsComponentConstrained {
				continue
			}
			for edgeIndex, edge := range polygon.Edges {
				if edge.NeighborOffset == 0 {
					continue
				}
				end := polygon.Edges[(edgeIndex+1)%len(polygon.Edges)].Vertex
				for _, fraction := range [3]float32{0.5, 0.05, 0.95} {
					point := add(edge.Vertex, scale(subtract(end, edge.Vertex), fraction))
					local := obstacle.local(point)
					if local.X < obstacle.Minimum.X-padding || local.X > obstacle.Maximum.X+padding ||
						local.Y < obstacle.Minimum.Y-padding || local.Y > obstacle.Maximum.Y+padding ||
						local.Z < obstacle.Minimum.Z-selected.info.Height-padding || local.Z > obstacle.Maximum.Z+padding {
						continue
					}
					if e.mesh.isObstructed(point, e.options.PlanLayer) || e.hasNode(point) {
						continue
					}
					if len(e.nodes) >= 128 {
						return isAddedAny
					}
					e.nodes = append(e.nodes, point)
					isAddedAny = true
				}
			}
		}
	}
	return isAddedAny
}

func (e *obstacleRoute) hasNode(point Vec3) bool {
	for _, node := range e.nodes {
		if squaredDistance(point, node) < 0.0025 {
			return true
		}
	}
	return false
}

func (e *obstacleRoute) addCorners() bool {
	isAddedAny := false
	terrain := *e.mesh
	terrain.obstacles = nil
	info := e.mesh.layers[e.options.PlanLayer].info
	// Keep projected corners outside the expanded collision boundary.
	padding := info.Radius + 0.05
	for index, obstacle := range e.mesh.obstacles {
		if !e.obstacleIndexes[index] || e.areObstaclesAdded[index] {
			continue
		}
		e.areObstaclesAdded[index] = true
		isAddedAny = true
		angle := float64(obstacle.Yaw) * math.Pi / 180
		cosine, sine := float32(math.Cos(angle)), float32(math.Sin(angle))
		corners := [4]Vec3{
			{X: obstacle.Minimum.X - padding, Y: obstacle.Minimum.Y - padding},
			{X: obstacle.Maximum.X + padding, Y: obstacle.Minimum.Y - padding},
			{X: obstacle.Maximum.X + padding, Y: obstacle.Maximum.Y + padding},
			{X: obstacle.Minimum.X - padding, Y: obstacle.Maximum.Y + padding},
		}
		for _, corner := range corners {
			if len(e.nodes) >= 128 {
				return isAddedAny
			}
			point := Vec3{
				X: obstacle.Position.X + cosine*corner.X - sine*corner.Y,
				Y: obstacle.Position.Y + sine*corner.X + cosine*corner.Y,
				Z: e.heights[index],
			}
			projection, err := terrain.Project(point, e.options.ProjectionOptions)
			if err != nil {
				continue // No reachable terrain corner; never invent an off-mesh detour.
			}
			probe := Path{Points: []Vec3{projection.Position, projection.Position}}
			indexes := e.mesh.pathObstacles(probe, e.options.PlanLayer)
			if len(indexes) != 0 {
				// An overlapping box can bury every corner of the first box.
				// Expand that box too rather than accepting its covered corner.
				e.recordObstacles(probe, indexes)
				continue
			}
			e.nodes = append(e.nodes, projection.Position)
		}
	}
	return isAddedAny
}

func (e *obstacleRoute) edge(source int, target int) (Path, bool) {
	key := [2]int{source, target}
	if e.areEdgesExamined[key] {
		path, isFound := e.paths[key]
		return path, isFound
	}
	e.areEdgesExamined[key] = true
	path, err := e.mesh.terrainPath(e.nodes[source], e.nodes[target], e.options)
	if err != nil {
		return Path{}, false // Disconnected corners are unavailable graph edges.
	}
	indexes := e.mesh.pathObstacles(path, e.options.PlanLayer)
	if len(indexes) != 0 {
		e.recordObstacles(path, indexes)
		return Path{}, false
	}
	e.paths[key] = path
	return path, true
}

// Take corner height from the colliding route segment, not the spawn point:
// a single route can climb from the lower factory to the upper machinery.
func (e *obstacleRoute) recordObstacles(path Path, indexes []int) {
	info := e.mesh.layers[e.options.PlanLayer].info
	for _, index := range indexes {
		if e.obstacleIndexes[index] {
			continue
		}
		e.obstacleIndexes[index] = true
		obstacle := e.mesh.obstacles[index]
		for pointIndex := 1; pointIndex < len(path.Points); pointIndex++ {
			start, end := path.Points[pointIndex-1], path.Points[pointIndex]
			if !obstacle.intersects(start, end, info.Radius, info.Height) {
				continue
			}
			delta := subtract(end, start)
			fraction := float32(0)
			squared := delta.X*delta.X + delta.Y*delta.Y
			if squared > 0 {
				fraction = max(0, min(1, ((obstacle.Position.X-start.X)*delta.X+
					(obstacle.Position.Y-start.Y)*delta.Y)/squared))
			}
			e.heights[index] = start.Z + delta.Z*fraction
			break
		}
	}
}

func (e *obstacleRoute) search() (Path, bool) {
	distances := make([]float32, len(e.nodes))
	previousIndexes := make([]int, len(e.nodes))
	areVisited := make([]bool, len(e.nodes))
	for index := range distances {
		distances[index], previousIndexes[index] = float32(math.Inf(1)), -1
	}
	distances[0] = 0
	for range e.nodes {
		source := -1
		for index := range e.nodes {
			if !areVisited[index] && !math.IsInf(float64(distances[index]), 1) &&
				(source < 0 || distances[index] < distances[source]) {
				source = index
			}
		}
		if source < 0 {
			return Path{}, false
		}
		if source == 1 {
			return e.assemble(previousIndexes), true
		}
		areVisited[source] = true
		for target := 1; target < len(e.nodes); target++ {
			if areVisited[target] || target == source {
				continue
			}
			path, isFound := e.edge(source, target)
			if !isFound {
				continue
			}
			cost := distances[source]
			for index := 1; index < len(path.Points); index++ {
				cost += distance(path.Points[index-1], path.Points[index])
			}
			if cost < distances[target] {
				distances[target], previousIndexes[target] = cost, source
			}
		}
	}
	return Path{}, false
}

func (e *obstacleRoute) assemble(previousIndexes []int) Path {
	indexes := []int{1}
	for indexes[len(indexes)-1] != 0 {
		indexes = append(indexes, previousIndexes[indexes[len(indexes)-1]])
	}
	reverse(indexes)
	result := Path{}
	for index := 1; index < len(indexes); index++ {
		path := e.paths[[2]int{indexes[index-1], indexes[index]}]
		if len(result.Points) == 0 {
			result.Points = append(result.Points, path.Points[0])
		}
		result.Points = append(result.Points, path.Points[1:]...)
		result.Corridor = append(result.Corridor, path.Corridor...)
	}
	return result
}

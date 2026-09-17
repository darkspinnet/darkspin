package navigation

import (
	"errors"
	"math"
)

func validatePolygonGeometry(polygon Polygon) error {
	if len(polygon.Edges) < 3 {
		return errors.New("edge count")
	}
	normal := Vec3{}
	origin := Vec3{}
	for edgeIndex := range polygon.Edges {
		current := polygon.Edges[edgeIndex].Vertex
		next := polygon.Edges[(edgeIndex+1)%len(polygon.Edges)].Vertex
		normal.X += (current.Y - next.Y) * (current.Z + next.Z)
		normal.Y += (current.Z - next.Z) * (current.X + next.X)
		normal.Z += (current.X - next.X) * (current.Y + next.Y)
		origin = add(origin, current)
	}
	origin = scale(origin, 1/float32(len(polygon.Edges)))
	normalLength := float32(math.Sqrt(float64(dot(normal, normal))))
	if normalLength <= 0 {
		return errors.New("zero area")
	}
	normal = scale(normal, 1/normalLength)
	maximumRadius := float32(0)
	for _, edge := range polygon.Edges {
		planeDistance := float32(math.Abs(float64(dot(subtract(edge.Vertex, origin), normal))))
		if planeDistance > bfxPlanarEpsilon {
			return errors.New("nonplanar")
		}
		maximumRadius = max(maximumRadius, distance(edge.Vertex, polygon.Center))
	}
	if float32(math.Abs(float64(maximumRadius-polygon.Radius))) > bfxRadiusEpsilon {
		return errors.New("radius mismatch")
	}
	winding := float32(0)
	for edgeIndex := range polygon.Edges {
		a := polygon.Edges[edgeIndex].Vertex
		b := polygon.Edges[(edgeIndex+1)%len(polygon.Edges)].Vertex
		c := polygon.Edges[(edgeIndex+2)%len(polygon.Edges)].Vertex
		turn := dot(cross(subtract(b, a), subtract(c, b)), normal)
		if float32(math.Abs(float64(turn))) <= bfxTurnEpsilon {
			continue
		}
		if winding == 0 {
			winding = turn
			continue
		}
		if winding*turn < 0 {
			return errors.New("nonconvex")
		}
	}
	if winding == 0 {
		return errors.New("zero winding")
	}
	return nil
}

func closestPointOnPolygon(point Vec3, polygon Polygon) Vec3 {
	best := polygon.Edges[0].Vertex
	bestDistance := squaredDistance(point, best)
	origin := polygon.Edges[0].Vertex
	for index := 1; index+1 < len(polygon.Edges); index++ {
		candidate := closestPointOnTriangle(
			point, origin, polygon.Edges[index].Vertex, polygon.Edges[index+1].Vertex,
		)
		candidateDistance := squaredDistance(point, candidate)
		if candidateDistance < bestDistance {
			best = candidate
			bestDistance = candidateDistance
		}
	}
	return best
}

func closestPointOnTriangle(point Vec3, a Vec3, b Vec3, c Vec3) Vec3 {
	ab := subtract(b, a)
	ac := subtract(c, a)
	ap := subtract(point, a)
	d1 := dot(ab, ap)
	d2 := dot(ac, ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}
	bp := subtract(point, b)
	d3 := dot(ab, bp)
	d4 := dot(ac, bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}
	vc := d1*d4 - d3*d2
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		fraction := d1 / (d1 - d3)
		return add(a, scale(ab, fraction))
	}
	cp := subtract(point, c)
	d5 := dot(ab, cp)
	d6 := dot(ac, cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}
	vb := d5*d2 - d1*d6
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		fraction := d2 / (d2 - d6)
		return add(a, scale(ac, fraction))
	}
	va := d3*d6 - d5*d4
	if va <= 0 && d4-d3 >= 0 && d5-d6 >= 0 {
		fraction := (d4 - d3) / ((d4 - d3) + (d5 - d6))
		return add(b, scale(subtract(c, b), fraction))
	}
	denominator := 1 / (va + vb + vc)
	v := vb * denominator
	w := vc * denominator
	return add(a, add(scale(ab, v), scale(ac, w)))
}

func add(a Vec3, b Vec3) Vec3 {
	return Vec3{X: a.X + b.X, Y: a.Y + b.Y, Z: a.Z + b.Z}
}

func subtract(a Vec3, b Vec3) Vec3 {
	return Vec3{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z}
}

func scale(vector Vec3, scalar float32) Vec3 {
	return Vec3{X: vector.X * scalar, Y: vector.Y * scalar, Z: vector.Z * scalar}
}

func dot(a Vec3, b Vec3) float32 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

func cross(a Vec3, b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

func squaredDistance(a Vec3, b Vec3) float32 {
	delta := subtract(a, b)
	return dot(delta, delta)
}

func distance(a Vec3, b Vec3) float32 {
	return float32(math.Sqrt(float64(squaredDistance(a, b))))
}

//go:build scenario

package scenario

import "time"

type WorldPathRequest struct {
	UserID              int64
	GameID              uint32
	PeerGeneration      uint64
	TransportGeneration uint64
	ZoneGeneration      uint64
	Start               WorldPosition
	Goal                WorldPosition
}

type WorldPathProjection struct {
	Position    WorldPosition
	PolygonID   uint32
	ComponentID uint32
	Distance    float32
}

// A route is a read-only bounded server navigation calculation, not movement,
// native reachability acknowledgment, or proof that the requested goal is exact.
type WorldPath struct {
	CapturedAt            time.Time
	GameID                uint32
	PeerGeneration        uint64
	TransportGeneration   uint64
	ZoneGeneration        uint64
	IsAvailable           bool
	UnavailableReason     string
	IsComplete            bool
	IsTruncated           bool
	PlanLayer             uint8
	FootprintRadius       float32
	MaxProjectionDistance float32
	MaxVisitedPolygon     int
	RequestedStart        WorldPosition
	RequestedGoal         WorldPosition
	Start                 WorldPathProjection
	Goal                  WorldPathProjection
	Points                []WorldPosition
	PointTotal            int
}

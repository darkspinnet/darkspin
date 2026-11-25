package activity

import "time"

// PVPOutcomeSample aggregates saved completed-match receipts by UTC completion
// day for the average round-time graph.
type PVPOutcomeSample struct {
	At              time.Time
	MatchCount      uint64
	PlaytimeSeconds uint64
}

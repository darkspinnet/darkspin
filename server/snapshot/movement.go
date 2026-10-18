package snapshot

import (
	"fmt"
	"math"
	"sort"
	"time"
)

const maximumMovementSampleCount = 8192

// movementSample pairs a local Fang observation with the next authoritative
// poll. The sample age is explicit: these are not simultaneous keyframes.
type movementSample struct {
	Sequence            uint64      `json:"sequence"`
	CapturedAt          time.Time   `json:"captured_at"`
	Remote              string      `json:"remote"`
	SessionGeneration   uint64      `json:"session_generation"`
	TransportGeneration uint64      `json:"transport_generation"`
	ZoneGeneration      uint64      `json:"zone_generation"`
	Object              ObjectState `json:"object"`
	ClientSource        string      `json:"client_source"`
	ClientLine          int         `json:"client_line"`
	ClientObjectID      uint32      `json:"client_object_id"`
	ClientTimeMS        uint64      `json:"client_time_ms"`
	ClientSampleAt      *time.Time  `json:"client_sample_at,omitempty"`
	ClientPosition      [3]float32  `json:"client_position"`
	ClientGoal          *[3]float32 `json:"client_goal,omitempty"`
	ClientPartialGoal   *[3]float32 `json:"client_partial_goal,omitempty"`
	ClientFlags         uint32      `json:"client_flags"`
	ClientTargetHandle  uint32      `json:"client_target_handle"`
	ClientDesiredStop   float32     `json:"client_desired_stop"`
	SampleAgeMS         float64     `json:"sample_age_ms"`
	IsTimingValid       bool        `json:"is_timing_valid"`
	Distance            float64     `json:"distance"`
	AllowedDistance     float64     `json:"allowed_distance"`
}

func newMovementSample(
	state StateFrame, session SessionState, object ObjectState, probe clientObjectProbe,
) movementSample {
	sample := movementSample{
		CapturedAt: state.CapturedAt, Remote: session.Remote,
		SessionGeneration:   session.SessionGeneration,
		TransportGeneration: session.TransportGeneration, ZoneGeneration: session.ZoneGeneration,
		Object: object, ClientSource: probe.source, ClientLine: probe.line,
		ClientObjectID: probe.clientObjectID, ClientTimeMS: probe.timeMS,
		ClientPosition: probe.position, ClientGoal: probe.goal,
		ClientPartialGoal: probe.partialGoal, ClientFlags: probe.flags,
		ClientTargetHandle: probe.targetHandle, ClientDesiredStop: probe.desiredStop,
		Distance:        positionDistance(object.Position, probe.position),
		AllowedDistance: automaticNPCDriftDistance,
	}
	if probe.sampleAt.IsZero() {
		return sample
	}
	sample.ClientSampleAt = &probe.sampleAt
	sample.SampleAgeMS = float64(state.CapturedAt.Sub(probe.sampleAt)) / float64(time.Millisecond)
	sample.IsTimingValid = sample.SampleAgeMS >= 0 && sample.SampleAgeMS <= 500
	speed := math.Max(float64(object.Speed), positionDistance(object.LinearVelocity, [3]float32{}))
	sample.AllowedDistance = driftBaseDistance + math.Abs(sample.SampleAgeMS)/1000*math.Max(speed, 1)
	return sample
}

func movementIdentity(sample movementSample) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d:%d", sample.Remote,
		sample.SessionGeneration, sample.TransportGeneration, sample.ZoneGeneration,
		sample.Object.ObjectID, sample.ClientObjectID)
}

func (e *Service) retainMovementLocked(sample movementSample) {
	e.nextMovementSequence++
	sample.Sequence = e.nextMovementSequence
	e.movements = append(e.movements, sample)
}

func (e *Service) pruneMovementsLocked(now time.Time) {
	cutoff := now.Add(-e.bufferDuration)
	start := 0
	for start < len(e.movements) && e.movements[start].CapturedAt.Before(cutoff) {
		start++
	}
	start = max(start, len(e.movements)-maximumMovementSampleCount)
	if start == 0 {
		return
	}
	count := copy(e.movements, e.movements[start:])
	clear(e.movements[count:])
	e.movements = e.movements[:count]
}

// Preserve a short lead-in for the incident even when the archive cooldown
// outlasts the rolling window. Identity includes session, zone and handle reuse.
func incidentMovements(samples []movementSample, incident anomaly) []movementSample {
	retained := make([]movementSample, 0, 32)
	identity := ""
	for index := len(samples) - 1; index >= 0 && len(retained) < 32; index-- {
		sample := samples[index]
		if sample.Remote != incident.Actor.Remote || sample.Object.ObjectID != incident.ObjectID {
			continue
		}
		if identity == "" {
			identity = movementIdentity(sample)
		}
		if movementIdentity(sample) != identity {
			continue
		}
		retained = append(retained, sample)
	}
	sort.Slice(retained, func(left, right int) bool { return retained[left].Sequence < retained[right].Sequence })
	return retained
}

func mergeMovements(samples, triggers []movementSample, actor Actor) []movementSample {
	merged := make([]movementSample, 0, len(samples)+len(triggers))
	sequences := make(map[uint64]struct{})
	for _, groups := range [][]movementSample{triggers, samples} {
		for _, sample := range groups {
			_, isSeen := sequences[sample.Sequence]
			if actor.Remote != "" && actor.Remote != sample.Remote || isSeen {
				continue
			}
			sequences[sample.Sequence] = struct{}{}
			merged = append(merged, sample)
		}
	}
	sort.Slice(merged, func(left, right int) bool { return merged[left].Sequence < merged[right].Sequence })
	return merged
}

type movementSummary struct {
	ObjectID                    uint32    `json:"object_id"`
	Kind                        string    `json:"kind"`
	Identity                    string    `json:"identity"`
	SampleCount                 int       `json:"sample_count"`
	TimedSampleCount            int       `json:"timed_sample_count"`
	FirstSequence               uint64    `json:"first_sequence"`
	LastSequence                uint64    `json:"last_sequence"`
	FirstAt                     time.Time `json:"first_at"`
	LastAt                      time.Time `json:"last_at"`
	FirstDistance               float64   `json:"first_distance"`
	LastDistance                float64   `json:"last_distance"`
	MaximumDistance             float64   `json:"maximum_distance"`
	ServerTravel                float64   `json:"server_travel"`
	ClientTravel                float64   `json:"client_travel"`
	LargestGapMS                float64   `json:"largest_gap_ms"`
	FirstOutlierSequence        uint64    `json:"first_outlier_sequence,omitempty"`
	LastWithinAllowanceSequence uint64    `json:"last_within_allowance_sequence,omitempty"`
	Pattern                     string    `json:"pattern"`
}

func summarizeMovements(samples []movementSample) []movementSummary {
	summaries := make([]movementSummary, 0)
	indexesByIdentity := make(map[string]int)
	lastSamplesByIdentity := make(map[string]movementSample)
	for _, sample := range samples {
		identity := movementIdentity(sample)
		index, isFound := indexesByIdentity[identity]
		if !isFound {
			index = len(summaries)
			indexesByIdentity[identity] = index
			summaries = append(summaries, movementSummary{
				ObjectID: sample.Object.ObjectID, Kind: sample.Object.Kind, Identity: identity,
				FirstSequence: sample.Sequence, FirstAt: sample.CapturedAt, FirstDistance: sample.Distance,
			})
		} else {
			previous := lastSamplesByIdentity[identity]
			gap := sample.CapturedAt.Sub(previous.CapturedAt)
			summaries[index].LargestGapMS = math.Max(summaries[index].LargestGapMS,
				float64(gap)/float64(time.Millisecond))
			// Do not count travel across cooldown gaps as a continuous trajectory.
			if gap >= 0 && gap <= time.Second && sample.IsTimingValid && previous.IsTimingValid {
				summaries[index].ServerTravel += positionDistance(previous.Object.Position, sample.Object.Position)
				summaries[index].ClientTravel += positionDistance(previous.ClientPosition, sample.ClientPosition)
			}
		}
		lastSamplesByIdentity[identity] = sample
		summary := &summaries[index]
		summary.SampleCount++
		summary.LastSequence = sample.Sequence
		summary.LastAt = sample.CapturedAt
		summary.LastDistance = sample.Distance
		summary.MaximumDistance = math.Max(summary.MaximumDistance, sample.Distance)
		if !sample.IsTimingValid {
			continue
		}
		summary.TimedSampleCount++
		if sample.Distance <= sample.AllowedDistance {
			summary.LastWithinAllowanceSequence = sample.Sequence
		} else if summary.FirstOutlierSequence == 0 {
			summary.FirstOutlierSequence = sample.Sequence
		}
	}
	for index := range summaries {
		summary := &summaries[index]
		summary.Pattern = "movement_divergence"
		if summary.FirstOutlierSequence == 0 {
			summary.Pattern = "no_timed_outlier"
		} else if summary.LastWithinAllowanceSequence > summary.FirstOutlierSequence &&
			summary.LastWithinAllowanceSequence == summary.LastSequence {
			summary.Pattern = "recovered"
		} else if summary.LargestGapMS > 1000 || summary.TimedSampleCount != summary.SampleCount {
			summary.Pattern = "incomplete_history"
		} else if summary.SampleCount >= 3 && summary.LastAt.Sub(summary.FirstAt) >= 2*time.Second &&
			summary.LastWithinAllowanceSequence == 0 && summary.ServerTravel < 0.1 && summary.ClientTravel < 0.1 {
			summary.Pattern = "stationary_offset"
		} else if summary.ServerTravel > 1 && summary.ClientTravel < 0.1 {
			summary.Pattern = "client_stationary_server_moving"
		}
	}
	return summaries
}

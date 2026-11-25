package activity

import (
	"errors"
	"math"
	"time"
)

type QueueKind string

const (
	QueueCampaign       QueueKind = "campaign"
	QueueArena1Ranked   QueueKind = "arena1_ranked"
	QueueArena1Unranked QueueKind = "arena1_unranked"
	QueueArena2Ranked   QueueKind = "arena2_ranked"
	QueueArena2Unranked QueueKind = "arena2_unranked"
)

// QueueObservation measures active waiting players, not completed queue attempts.
// WaitSeconds sums each queued human's current age; a party contributes each member.
type QueueObservation struct {
	Kind               QueueKind
	QueuedCount        int64
	WaitSeconds        float64
	MaximumWaitSeconds float64
}

func validateQueues(queues []QueueObservation) error {
	if queues == nil {
		return nil
	}
	if len(queues) != 5 {
		return errors.New("incomplete queue observations")
	}
	kinds := make(map[QueueKind]struct{}, len(queues))
	for _, queue := range queues {
		switch queue.Kind {
		case QueueCampaign, QueueArena1Ranked, QueueArena1Unranked, QueueArena2Ranked, QueueArena2Unranked:
		default:
			return errors.New("invalid observed queue")
		}
		_, isDuplicate := kinds[queue.Kind]
		if isDuplicate {
			return errors.New("duplicate observed queue")
		}
		kinds[queue.Kind] = struct{}{}
		if queue.QueuedCount < 0 || queue.WaitSeconds < 0 || queue.MaximumWaitSeconds < 0 ||
			math.IsNaN(queue.WaitSeconds) || math.IsInf(queue.WaitSeconds, 0) ||
			math.IsNaN(queue.MaximumWaitSeconds) || math.IsInf(queue.MaximumWaitSeconds, 0) ||
			queue.MaximumWaitSeconds > queue.WaitSeconds ||
			queue.QueuedCount == 0 && (queue.WaitSeconds != 0 || queue.MaximumWaitSeconds != 0) {
			return errors.New("invalid queue observation")
		}
	}
	return nil
}

// QueueSample averages queued counts over hourly observations and wait age over
// queued-player observations. No wait age is known when QueuedCount is zero.
type QueueSample struct {
	At                 time.Time
	Kind               QueueKind
	QueuedCount        float64
	AverageWaitSeconds float64
	MaximumWaitSeconds float64
	ObservationCount   int64
}

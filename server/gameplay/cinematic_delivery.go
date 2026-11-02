package gameplay

import (
	"log"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// A queued C9 owns its original enqueue deadline across failed transports.
// Native C9 reception starts a fresh local timer, so replaying the original
// duration would extend a cinematic each time delivery is retried.
type pendingCinematic struct {
	deadline time.Time
	message  raknet.CinematicMessage
}

func capturePendingCinematics(packets [][]byte, now time.Time) map[int]pendingCinematic {
	var cinematics map[int]pendingCinematic
	for index, packet := range packets {
		if len(packet) == 0 || raknet.PacketID(packet[0]) != raknet.CinematicMsgs {
			continue
		}
		if cinematics == nil {
			cinematics = make(map[int]pendingCinematic)
		}
		// An invalid or nonpositive hold is omitted rather than replayed. A
		// zero native duration cannot cancel a previously active camera.
		cinematics[index] = pendingCinematic{}
		message, err := raknet.DecodeCinematicMessage(packet[1:])
		if err != nil {
			log.Printf("Queued cinematic omitted after decoding failure: %v", err)
			continue
		}
		const maxMilliseconds = int64((1<<63 - 1) / time.Millisecond)
		if message.DurationMilliseconds <= 0 {
			continue
		}
		if message.DurationMilliseconds > maxMilliseconds ||
			!isFiniteCinematicCoordinate(message.Focus.X) ||
			!isFiniteCinematicCoordinate(message.Focus.Y) ||
			!isFiniteCinematicCoordinate(message.Focus.Z) ||
			!isFiniteCinematicCoordinate(message.Radius) {
			log.Printf("Queued cinematic omitted because duration or focus is invalid")
			continue
		}
		cinematics[index] = pendingCinematic{
			deadline: now.Add(time.Duration(message.DurationMilliseconds) * time.Millisecond),
			message:  message,
		}
	}
	return cinematics
}

func isFiniteCinematicCoordinate(coordinate float32) bool {
	return !math.IsNaN(float64(coordinate)) && !math.IsInf(float64(coordinate), 0)
}

func (e pendingPeerPacketBatch) packetsAt(now time.Time) [][]byte {
	if len(e.cinematics) == 0 {
		return e.packets
	}
	packets := make([][]byte, 0, len(e.packets))
	for index, packet := range e.packets {
		cinematic, isCinematic := e.cinematics[index]
		if !isCinematic {
			packets = append(packets, packet)
			continue
		}
		remaining := cinematic.deadline.Sub(now)
		if remaining <= 0 || cinematic.deadline.IsZero() {
			continue
		}
		message := cinematic.message
		// Flooring prevents delivery from extending the captured deadline.
		message.DurationMilliseconds = min(message.DurationMilliseconds, remaining.Milliseconds())
		if message.DurationMilliseconds <= 0 {
			continue
		}
		agedPacket, err := raknet.MarshalApplication(message)
		if err != nil {
			log.Printf("Queued cinematic omitted after encoding failure: %v", err)
			continue
		}
		packets = append(packets, agedPacket)
	}
	return packets
}

func (e pendingPeerPacketBatch) isExpiredCinematicBatch(now time.Time) bool {
	if len(e.cinematics) == 0 || len(e.cinematics) != len(e.packets) ||
		e.wreathRemoval != nil || e.omniRemoval != nil ||
		e.channelRemoval != nil || e.channelCreation != nil {
		return false
	}
	for _, cinematic := range e.cinematics {
		if !cinematic.deadline.IsZero() && cinematic.deadline.Sub(now) >= time.Millisecond {
			return false
		}
	}
	return true
}

func pruneExpiredCinematicBatches(batches []pendingPeerPacketBatch, now time.Time) []pendingPeerPacketBatch {
	firstExpired := -1
	for index, batch := range batches {
		if batch.isExpiredCinematicBatch(now) {
			firstExpired = index
			break
		}
	}
	if firstExpired < 0 {
		return batches
	}
	// Peer sessions are copied out of the registry. Do not compact their
	// shared backing array before the caller stores the updated aggregate.
	retainedBatches := make([]pendingPeerPacketBatch, 0, len(batches)-1)
	retainedBatches = append(retainedBatches, batches[:firstExpired]...)
	for _, batch := range batches[firstExpired+1:] {
		if !batch.isExpiredCinematicBatch(now) {
			retainedBatches = append(retainedBatches, batch)
		}
	}
	return retainedBatches
}

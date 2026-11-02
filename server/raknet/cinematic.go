package raknet

import (
	"encoding/binary"
	"fmt"
	"math"
)

// DecodeCinematicMessage reads the fixed payload consumed by build 103
// 450F20 and build 127 4530C0. The application packet ID is not part of payload.
func DecodeCinematicMessage(payload []byte) (CinematicMessage, error) {
	if len(payload) != 24 {
		return CinematicMessage{}, fmt.Errorf("cinematicLength[%d]: %w", len(payload), ErrInvalidPacket)
	}
	return CinematicMessage{
		DurationMilliseconds: int64(binary.LittleEndian.Uint64(payload[:8])),
		Focus: Vector3{
			X: math.Float32frombits(binary.LittleEndian.Uint32(payload[8:12])),
			Y: math.Float32frombits(binary.LittleEndian.Uint32(payload[12:16])),
			Z: math.Float32frombits(binary.LittleEndian.Uint32(payload[16:20])),
		},
		Radius: math.Float32frombits(binary.LittleEndian.Uint32(payload[20:24])),
	}, nil
}

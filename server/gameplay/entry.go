package gameplay

import "github.com/darkspinnet/darkspin/server/raknet"

// Preserve the authored entrance height and apply the same small party
// formation whenever a map does not provide a separate marker for a slot.
func campaignEntryOffset(position raknet.Vector3, playerSlot uint16) raknet.Vector3 {
	switch playerSlot {
	case 1:
		position.X += 3
	case 2:
		position.Y += 3
	case 3:
		position.X -= 3
	}
	return position
}

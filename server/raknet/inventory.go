package raknet

import "encoding/binary"

// LootDiscardedMessage removes a persistent item from the local inventory.
// Build 103 sub_4E17E0 handles client event 0xA717F30B by reading the uint64
// at +112 (field 18), calling sub_4C1880, then refreshing via sub_42F290.
type LootDiscardedMessage struct {
	ItemID uint64
}

func (LootDiscardedMessage) PacketID() PacketID { return ServerEvent }

func (e LootDiscardedMessage) EncodePayload() []byte {
	payload := []byte{15}
	payload = binary.LittleEndian.AppendUint32(payload, 0xA717F30B)
	payload = append(payload, 18)
	payload = binary.LittleEndian.AppendUint64(payload, e.ItemID)
	return append(payload, 0xff)
}

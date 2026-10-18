package raknet103

import "github.com/darkspinnet/darkspin/server/raknet"

// DNAAccountUpdate encodes the authoritative balance returned by a committed
// grant. This concrete message has no validation or fallible encoding; world
// collection messages must already have been prepared before granting DNA.
func DNAAccountUpdate(slot uint8, dna uint32) []byte {
	message := raknet.LabsPlayerDNAUpdateMessage{Slot: slot, DNA: dna}
	return append([]byte{byte(message.PacketID())}, message.EncodePayload()...)
}

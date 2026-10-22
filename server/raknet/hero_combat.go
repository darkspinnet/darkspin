package raknet

import "encoding/binary"

// HeroCombatStateMessage changes cAgentBlackboard's in-combat and mNumAttackers
// fields. Target, stealth and targetability keep their current state. The client
// passes the raw attacker count to audio and owns the music intensity threshold.
type HeroCombatStateMessage struct {
	ObjectID      uint32
	IsInCombat    bool
	AttackerCount uint32
}

func (HeroCombatStateMessage) PacketID() PacketID { return AgentBlackboardUpdate }

func (e HeroCombatStateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, e.ObjectID)
	payload = append(payload, 0x12, boolByte(e.IsInCombat))
	return binary.LittleEndian.AppendUint32(payload, e.AttackerCount)
}

// HeroStealthMessage preserves the independently replicated combat state.
type HeroStealthMessage struct {
	ObjectID uint32
	Stealth  uint8
}

func (HeroStealthMessage) PacketID() PacketID { return AgentBlackboardUpdate }

func (e HeroStealthMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, e.ObjectID)
	return append(payload, 0x0c, e.Stealth, 1)
}

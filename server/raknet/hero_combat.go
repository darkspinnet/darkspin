package raknet

import "encoding/binary"

// HeroCombatStateMessage changes only cAgentBlackboard's in-combat field.
// Target, stealth, targetability and attacker count keep their current state.
type HeroCombatStateMessage struct {
	ObjectID   uint32
	IsInCombat bool
}

func (HeroCombatStateMessage) PacketID() PacketID { return AgentBlackboardUpdate }

func (e HeroCombatStateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, e.ObjectID)
	return append(payload, 0x02, boolByte(e.IsInCombat))
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

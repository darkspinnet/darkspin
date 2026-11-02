package gameplay

import (
	"fmt"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Build-103 chunks118/218/505/690 request the persistent status_operative visual;
// chunk505 additionally owns a jetpack. Held attachments retain their identity
// through teardown and recipient reconnect until removals commit.
func (e *agentSupportState) attachPassiveEffects(registry *gameplaySessionRegistry, sessionKey string, member *gameplayPeerSession, pool *attachedEffectPool, isSpacetime bool) ([][]byte, error) {
	if e.passiveEffects != nil {
		return nil, nil
	}
	names := []string{"status_operative.ServerEventDef"}
	objectIDs := []uint32{e.ownerObjectID}
	if isSpacetime {
		names = append(names, "SpacetimeAgentJetpack.ServerEventDef")
		objectIDs = append(objectIDs, e.ownerObjectID)
	}
	effects, err := registry.allocateChannelEffectsLocked(member.zone, pool, objectIDs)
	if err != nil {
		return nil, fmt.Errorf("passiveAllocate: %w", err)
	}
	var packets [][]byte
	for index, attachment := range effects.Slots() {
		packet, marshalErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{Slot: attachment.slot + 1, ObjectID: attachment.objectID, IsForceAttached: true, Asset: util.HashID(names[index])})
		if marshalErr != nil {
			effects.Retire()
			return nil, fmt.Errorf("passiveMarshal: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	e.passiveEffects = effects
	registry.sessions[sessionKey] = *member
	err = registry.publishChannelEffectsLocked(effects, packets)
	*member = registry.sessions[sessionKey]
	if err != nil {
		effects.Retire()
		e.passiveEffects = nil
		return nil, fmt.Errorf("passivePublish: %w", err)
	}
	// Publication queues each observer through the established held-effect FIFO.
	return nil, nil
}

// Caller holds the registry lock. A zero source retires this peer's full set.
func (e *gameplayPeerSession) retireAgentPassiveEffectsLocked(sourceID uint32) error {
	for objectID, state := range e.agentSupports {
		if sourceID != 0 && objectID != sourceID {
			continue
		}
		state.passiveEffects.Retire()
		state.passiveEffects = nil
	}
	return nil
}

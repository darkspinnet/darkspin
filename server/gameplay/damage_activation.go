package gameplay

import (
	"fmt"
	"sync/atomic"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// Snapshot activation identity is independent of request timestamps and damage
// definition names. Copies of an initiator snapshot retain this identity;
// fresh native ability/modifier instances receive another identity.
var damageActivationSequence atomic.Uint64

func nextDamageActivationID() uint64 {
	activationID := damageActivationSequence.Add(1)
	if activationID == 0 {
		activationID = damageActivationSequence.Add(1)
	}
	return activationID
}

// The remaining squad wire route owns real character ability activations too.
// It includes targeted areas and Ghost Form which are not generic reroutes.
func (e campaignAbilityCommandRuntime) handleSquadActivation(
	packet raknet.Packet, command raknet.ActionCommandData,
	member gameplayPeerSession,
) ([][]byte, error) {
	e.damage.activationID = nextDamageActivationID()
	startedAt := e.now()
	packets, err := e.handleSquad(packet, command, member)
	if err != nil {
		return nil, fmt.Errorf("squadAbility: %w", err)
	}
	err = e.recordFireRavagerUse(packet, command, member, packets, startedAt)
	if err != nil {
		return nil, fmt.Errorf("squadUse: %w", err)
	}
	e.recordScenarioAbilityDispatch(packet, command, member, packets, startedAt)
	return packets, nil
}

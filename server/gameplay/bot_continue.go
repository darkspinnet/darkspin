package gameplay

import (
	"fmt"
	"math/bits"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

// Continue replaces the human's campaign session in place. Its socketless
// peers still point at the previous zone until they are retired and admitted
// again against the next level's binding.
func (e campaignResultRuntime) retireCampaignServerBots(owner gameplayPeerSession) {
	if e.registry == nil || owner.binding.IsServerBot {
		return
	}
	e.registry.mutex.Lock()
	members := make([]gameplayPeerSession, 0)
	for sessionKey, member := range e.registry.sessions {
		if !member.binding.IsServerBot || member.binding.GameID != owner.binding.GameID ||
			member.binding.BotOwnerUserID != owner.binding.UserID {
			continue
		}
		members = append(members, member)
		delete(e.registry.sessions, sessionKey)
		e.registry.clearActionLeasesLocked(sessionKey, 0)
		memberKey := gameplaySessionMemberKey(member)
		if e.registry.memberTransports[memberKey] == member.transportGeneration {
			delete(e.registry.memberTransports, memberKey)
			delete(e.registry.memberEndpoints, memberKey)
		}
	}
	e.registry.mutex.Unlock()
	for _, member := range members {
		stopGameplayPeerSession(member, e.modifierPool, e.effectPool)
	}
}

// A companion can fill a vacated slot between missions on the same transport.
// Build 103 needs PlayerJoined before it can bind the new hero to that slot.
func (e campaignResultRuntime) campaignContinueSetup(
	packet raknet.Packet, binding game.GameplayBinding, setupPacket []byte,
) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.RLock()
	recipient, isFound := e.registry.sessions[sessionKey]
	var botMask uint32
	if isFound && isSameCampaignStage(recipient.binding, binding) {
		for _, member := range e.registry.sessions {
			if member.binding.IsServerBot && member.binding.Slot < 32 &&
				isSameCampaignStage(member.binding, binding) {
				botMask |= uint32(1) << member.binding.Slot
			}
		}
	}
	e.registry.mutex.RUnlock()

	missingMask := botMask &^ recipient.knownPlayerMask
	packets := make([][]byte, 0, bits.OnesCount32(missingMask)+1)
	for slots := missingMask; slots != 0; slots &= slots - 1 {
		slot := uint8(bits.TrailingZeros32(slots))
		joinedPacket, err := raknet.MarshalApplication(raknet.PlayerSlotMessage{
			ID: raknet.PlayerJoined, Slot: slot,
		})
		if err != nil {
			return nil, fmt.Errorf("continueJoin[%d]: %w", slot, err)
		}
		packets = append(packets, joinedPacket)
	}
	if missingMask != 0 {
		delivery := campaignContinueRosterDelivery{
			registry: e.registry, sessionKey: sessionKey, generation: recipient.generation,
			transportGeneration: recipient.transportGeneration, playerMask: missingMask,
		}
		err := packet.AfterResponseCommit(delivery.commit)
		if err != nil {
			return nil, fmt.Errorf("continueRosterCommit: %w", err)
		}
	}
	return append(packets, setupPacket), nil
}

func isSameCampaignStage(first, second game.GameplayBinding) bool {
	return first.Mode == game.ModeChain && first.GameID == second.GameID &&
		first.ChainLevelIndex == second.ChainLevelIndex && first.Level == second.Level
}

type campaignContinueRosterDelivery struct {
	registry            *gameplaySessionRegistry
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	playerMask          uint32
}

func (e campaignContinueRosterDelivery) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	recipient, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || recipient.generation != e.generation ||
		recipient.transportGeneration != e.transportGeneration {
		return
	}
	recipient.knownPlayerMask |= e.playerMask
	e.registry.sessions[e.sessionKey] = recipient
}

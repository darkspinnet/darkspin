package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/emote"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

func (e gameplayPendingRuntime) playModEmote(
	packet raknet.Packet, queuedSession gameplayPeerSession, req game.PlayerEventCommand,
) ([][]byte, bool, error) {
	state, isKnown := emote.SporeState(req.Category)
	if !isKnown {
		return nil, false, nil
	}
	now := e.now()
	e.registry.mutex.Lock()
	sessionKey := packet.Address.String()
	session, isFound := e.registry.sessions[sessionKey]
	isMoving := false
	if session.playerMotion != nil {
		movement := session.playerMotion.Snapshot().Movement()
		if movement.IsMoving {
			// Movement snapshots retain the last consumed command time. Chat
			// requests do not carry a movement action, so sample the path at now
			// instead of rejecting a creature that has already reached its goal.
			position, sampleErr := session.playerMotion.SamplePosition(now)
			if sampleErr != nil {
				e.registry.mutex.Unlock()
				return nil, false, fmt.Errorf("emotePosition: %w", sampleErr)
			}
			isMoving = position != movement.Goal
		}
	}
	isEligible := isFound && session.generation == queuedSession.generation &&
		session.transportGeneration == queuedSession.transportGeneration &&
		session.stage.IsDungeon() && session.dungeonSetup.IsCommitted() &&
		!session.isZoneTerminal() && session.deployedObjectID != 0 &&
		session.deployedHitPoint() > 0 && session.isAbilityReleaseReady(now) &&
		!session.isEnemyStunActive(now) && !session.isEnemySleepActive(now) &&
		!session.isEnemyFearActive(now) &&
		!isMoving &&
		session.basicAttack == nil && session.followTargetUserID == 0 && !session.playerAI.isEnabled &&
		session.deployedCreatureIndex < uint32(len(session.binding.Creatures))
	isBound := session.deployedCreatureIndex < uint32(len(session.binding.Creatures)) &&
		session.binding.Creatures[session.deployedCreatureIndex].IsModBound
	if !isEligible || !isBound {
		e.registry.mutex.Unlock()
		e.logger.Printf("RakNet Mod emote rejected name=%s remote=%s moving=%t bound=%t basic_attack=%t follow=%t ai=%t",
			req.Category, packet.Address, isMoving, isBound, session.basicAttack != nil,
			session.followTargetUserID != 0, session.playerAI.isEnabled)
		return nil, false, nil
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: session.deployedObjectID, State: util.HashID(state),
		Timestamp: packet.SourceTime, Scale: 1,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("emoteMarshal: %w", err)
	}
	// Reuse the dance interruption path: movement and abilities restore their
	// normal animation instead of leaving the expression pinned on the hero.
	session.isDancing = true
	e.registry.sessions[sessionKey] = session
	e.registry.mutex.Unlock()
	packets := [][]byte{animationPacket}
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, false, fmt.Errorf("emotePublish: %w", err)
	}
	e.logger.Printf("RakNet Mod emote accepted name=%s object=%d", req.Category, session.deployedObjectID)
	return packets, true, nil
}

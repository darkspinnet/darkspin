package gameplay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	memberraknet "github.com/darkspinnet/darkspin/server/zone/member/raknet103"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

// A server bot has a logical participant identity and an independent hero
// runtime, but borrows its owner's transport for presentation and scheduling.
// The internal address is only a registry key; no socket is opened for it.
func serverBotAddress(binding game.GameplayBinding) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv6zero, Zone: fmt.Sprintf("bot-%d", binding.GameID), Port: int(binding.Slot) + 1}
}

func serverBotPacket(packet raknet.Packet, member gameplayPeerSession) raknet.Packet {
	packet = packet.Autonomous()
	packet.Address = serverBotAddress(member.binding)
	packet.TransportGeneration = member.transportGeneration
	return packet
}

// The human starts the shared world first. Bots join that exact zone without
// performing authenticated user lookups or loading a human checkpoint under
// their synthetic identity.
func (e campaignPreparation) initializeServerBot(binding game.GameplayBinding, member *gameplayPeerSession) error {
	if e.session == nil || binding.BotOwnerUserID == 0 {
		return errors.New("campaign bot owner unavailable")
	}
	e.session.mutex.RLock()
	var owner gameplayPeerSession
	isOwnerFound := false
	for _, candidate := range e.session.sessions {
		if !candidate.binding.IsServerBot && candidate.binding.GameID == binding.GameID &&
			candidate.binding.UserID == binding.BotOwnerUserID {
			owner = candidate
			isOwnerFound = true
			break
		}
	}
	e.session.mutex.RUnlock()
	if !isOwnerFound || owner.zone == nil || owner.binding.Level != binding.Level || !owner.stage.IsDungeon() {
		return errors.New("campaign bot owner zone unavailable")
	}
	var creatureFootprints [squad.Size]float32
	for index, creature := range binding.Creatures {
		footprint, err := e.program.FootprintRadiusByNoun(creature.Noun)
		if err != nil || footprint <= 0 {
			footprint = campaignSecurityBlitzFootprintFallback
		}
		creatureFootprints[index] = footprint
	}
	err := owner.zone.Join(zone.Member{
		UserID: binding.UserID, PeerGeneration: member.generation, Slot: binding.Slot,
		IsServerBot: true, BotOwnerUserID: binding.BotOwnerUserID,
		AbilityCount: zoneunlock.InitialAbilityCount(binding), IsReplay: binding.IsReplay,
		Roster: binding.Roster(), CreatureFootprints: creatureFootprints,
	})
	if err != nil {
		return fmt.Errorf("botZoneJoin: %w", err)
	}
	member.zone = owner.zone
	member.crystalInventory.IsDiagonalUnlocked = binding.IsDiagonalCatalystUnlocked
	err = member.zone.SetCrystalInventory(binding.UserID, member.generation, member.crystalInventory)
	if err != nil {
		return fmt.Errorf("botCrystal: %w", err)
	}
	member.nextProjectileObjectID = max(member.nextProjectileObjectID, zoneobject.ProjectileIDStart)
	return nil
}

func (e gameplayJoinRuntime) installServerBots(ctx context.Context, packet raknet.Packet, binding game.GameplayBinding) error {
	if binding.Mode != game.ModeArena && binding.Mode != game.ModeChain {
		return nil
	}
	bindings, err := e.gameplayJoin.BotBindings(ctx, binding)
	if err != nil {
		return fmt.Errorf("botBindings: %w", err)
	}
	for _, botBinding := range bindings {
		err = botBinding.ConfigureDefenseDamage(e.ratingConversions)
		if err != nil {
			return fmt.Errorf("botDefense: %w", err)
		}
		key := serverBotAddress(botBinding).String()
		e.registry.mutex.Lock()
		if e.registry.isClosed {
			e.registry.mutex.Unlock()
			return errors.New("bot registry closed")
		}
		current, isFound := e.registry.sessions[key]
		if isFound && current.binding.GameID == botBinding.GameID && current.binding.UserID == botBinding.UserID {
			e.registry.mutex.Unlock()
			continue
		}
		abilityInterval := playerAIAbilityInterval
		if botBinding.Mode == game.ModeChain {
			abilityInterval /= 2
		}
		member := gameplayPeerSession{
			zoneMembership: zoneMembership{generation: e.registry.lifecycle.nextGeneration()},
			controlledHeroState: controlledHeroState{
				overdriveEnergy: campaignInitialOverdriveEnergy(botBinding),
				playerAI:        playerAIState{isEnabled: true, nextAbilityAt: e.registry.now().Add(abilityInterval)},
			},
			binding: botBinding, transportGeneration: e.registry.lifecycle.nextGeneration(),
			schedulePackets:      packet.Autonomous().Schedule,
			schedulePacket:       gameplaySchedulePacket(packet.Autonomous()),
			knownPlayerMask:      botBinding.PlayerMask,
			isArenaSquadAccepted: true, isArenaPreparationAcknowledged: true,
		}
		e.registry.sessions[key] = member
		memberKey := gameplaySessionMemberKey(member)
		e.registry.memberTransports[memberKey] = member.transportGeneration
		e.registry.memberEndpoints[memberKey] = key
		e.registry.mutex.Unlock()
		if e.logger != nil {
			e.logger.Printf("Arena server bot admitted game=%d user=%d slot=%d team=%d owner=%d", botBinding.GameID, botBinding.UserID, botBinding.Slot, botBinding.Team, botBinding.BotOwnerUserID)
		}
	}
	return nil
}

func (e gameplayStatusRuntime) startServerBotDungeons(ctx context.Context, packet raknet.Packet, owner gameplayPeerSession) error {
	e.registry.mutex.RLock()
	keys := make([]string, 0)
	for key, member := range e.registry.sessions {
		if member.binding.IsServerBot && member.binding.GameID == owner.binding.GameID && member.binding.BotOwnerUserID == owner.binding.UserID {
			keys = append(keys, key)
		}
	}
	e.registry.mutex.RUnlock()
	slices.Sort(keys)
	for _, key := range keys {
		err := e.startServerBotDungeon(ctx, packet, key)
		if err != nil {
			return fmt.Errorf("botDungeon: %w", err)
		}
	}
	return nil
}

func (e gameplayStatusRuntime) publishServerBotStatus(
	owner gameplayPeerSession, status raknet.PlayerStatus,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	keys := make([]string, 0)
	for key, member := range e.registry.sessions {
		if member.binding.IsServerBot && member.binding.GameID == owner.binding.GameID &&
			member.binding.BotOwnerUserID == owner.binding.UserID {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	packets := make([][]byte, 0, len(keys))
	for _, key := range keys {
		member := e.registry.sessions[key]
		packet, err := memberraknet.Status(memberraknet.StatusRequest{
			PlayerIndex: uint8(member.binding.Slot),
			Status:      status.Status,
			Progress:    status.Progress,
		})
		if member.binding.Mode == game.ModeChain && status.Status == 2 {
			packet, err = marshalCampaignInitialPlayer(member.binding, status, member.presentedOverdriveEnergy(e.registry.now()))
		}
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("botStatus[%d]: %w", member.binding.Slot, err)
		}
		member.lastPlayerStatus = status
		member.isPlayerStatusKnown = true
		e.registry.sessions[key] = member
		packets = append(packets, packet)
	}
	e.registry.mutex.Unlock()
	return packets, nil
}

func (e gameplayStatusRuntime) startServerBotDungeon(ctx context.Context, packet raknet.Packet, key string) error {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[key]
	e.registry.mutex.RUnlock()
	if !isFound || !member.binding.IsServerBot {
		return nil
	}
	memberMutex, err := e.registry.lockMember(ctx, gameplaySessionMemberKey(member))
	if err != nil {
		return fmt.Errorf("botLock: %w", err)
	}
	defer memberMutex.Unlock()
	e.registry.mutex.RLock()
	member, isFound = e.registry.sessions[key]
	e.registry.mutex.RUnlock()
	if !isFound || member.stage.IsDungeon() {
		return nil
	}
	abilityInterval := playerAIAbilityInterval
	if member.binding.Mode == game.ModeChain {
		abilityInterval /= 2
	}
	member.playerAI = playerAIState{isEnabled: true, nextAbilityAt: e.registry.now().Add(abilityInterval)}
	member.lastPlayerStatus = raknet.PlayerStatus{Status: 8, Progress: 1}
	member.isPlayerStatusKnown = true
	botPacket := serverBotPacket(packet, member)
	var startPackets [][]byte
	if member.binding.Mode == game.ModeChain {
		startPackets, err = e.startCampaign(ctx, botPacket, member, raknet.PlayerStatus{Status: 8, Progress: 1})
	} else {
		startPackets, err = e.startArena(botPacket, member, raknet.PlayerStatus{Status: 8, Progress: 1})
	}
	if err != nil {
		return fmt.Errorf("botStart: %w", err)
	}
	// The owner's own setup projects every roster member. A bot's local setup
	// must never select its controlled object in the owner's client.
	_ = startPackets
	return nil
}

// Bots have no native loading state. Their owned human's post-GameStart ping
// is the presentation boundary for committing bot setup as well.
func (e gameplaySetupRuntime) publishServerBots(ctx context.Context, packet raknet.Packet) ([][]byte, error) {
	e.registry.mutex.RLock()
	owner, isFound := e.registry.sessions[packet.Address.String()]
	keys := make([]string, 0)
	if isFound && !owner.binding.IsServerBot && owner.dungeonSetup.IsCommitted() {
		for key, member := range e.registry.sessions {
			if member.binding.GameID == owner.binding.GameID && member.binding.IsServerBot &&
				member.binding.BotOwnerUserID == owner.binding.UserID && member.stage.IsDungeon() && !member.dungeonSetup.IsCommitted() {
				keys = append(keys, key)
			}
		}
	}
	e.registry.mutex.RUnlock()
	slices.Sort(keys)
	packets := make([][]byte, 0)
	for _, key := range keys {
		botPackets, err := e.publishServerBot(ctx, packet, key)
		if err != nil {
			return nil, fmt.Errorf("botSetup: %w", err)
		}
		packets = append(packets, botPackets...)
	}
	return packets, nil
}

func (e gameplaySetupRuntime) publishServerBot(ctx context.Context, packet raknet.Packet, key string) ([][]byte, error) {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[key]
	e.registry.mutex.RUnlock()
	if !isFound || !member.binding.IsServerBot {
		return nil, nil
	}
	memberMutex, err := e.registry.lockMember(ctx, gameplaySessionMemberKey(member))
	if err != nil {
		return nil, fmt.Errorf("setupBotLock: %w", err)
	}
	defer memberMutex.Unlock()
	// Arena setup is private; campaign setup uses the public roster branch.
	packets, isPublished, err := e.publishDungeon(ctx, serverBotPacket(packet, member))
	if err != nil {
		return nil, fmt.Errorf("setupBotPublish: %w", err)
	}
	if member.binding.Mode == game.ModeChain && isPublished {
		// These public hero packets share the owner's response commit, so
		// arrival cannot become active before the owner receives the roster.
		return packets, nil
	}
	if !isPublished && len(packets) != 0 && e.logger != nil {
		e.logger.Printf("Arena bot setup returned packets without commit game=%d user=%d", member.binding.GameID, member.binding.UserID)
	}
	return nil, nil
}

func (e gameplayPendingRuntime) pollServerBots(ctx context.Context, packet raknet.Packet) ([][]byte, error) {
	e.registry.mutex.RLock()
	owner, isOwnerFound := e.registry.sessions[packet.Address.String()]
	members := make([]gameplayPeerSession, 0)
	if isOwnerFound && !owner.binding.IsServerBot && owner.stage.IsDungeon() &&
		owner.dungeonSetup.IsCommitted() && !owner.isRejoinPending && !owner.isZoneTerminal() {
		for _, member := range e.registry.sessions {
			if member.binding.IsServerBot && member.binding.GameID == owner.binding.GameID &&
				member.binding.BotOwnerUserID == owner.binding.UserID &&
				(member.binding.Mode == game.ModeChain || member.isArenaCombatActive) &&
				isPlayerAISession(member) {
				members = append(members, member)
			}
		}
	}
	e.registry.mutex.RUnlock()
	slices.SortFunc(members, func(first, second gameplayPeerSession) int { return int(first.binding.Slot) - int(second.binding.Slot) })
	packets := make([][]byte, 0)
	for _, member := range members {
		err := e.refreshServerBotControl(member)
		if err != nil {
			return nil, fmt.Errorf("botControl: %w", err)
		}
		botPackets, err := e.pollPlayerAI(ctx, serverBotPacket(packet, member))
		if err != nil {
			return nil, fmt.Errorf("botPoll: %w", err)
		}
		packets = append(packets, botPackets...)
	}
	return packets, nil
}

// Bots borrow the human transport instead of running their own world poll.
// Release arrival/root holds even when no AI command is ready, and deliver
// the same authoritative control transition to every real observer.
func (e gameplayPendingRuntime) refreshServerBotControl(member gameplayPeerSession) error {
	sessionKey := serverBotAddress(member.binding).String()
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	current, isFound := e.registry.sessions[sessionKey]
	if !isFound || !current.binding.IsServerBot || current.generation != member.generation ||
		current.transportGeneration != member.transportGeneration {
		return nil
	}
	packets, err := current.refreshEnemyImmobilizedAttributes(e.now())
	if err != nil {
		return fmt.Errorf("botImmobilized: %w", err)
	}
	e.registry.sessions[sessionKey] = current
	e.registry.queueHeroControlPeersLocked(sessionKey, current, packets)
	return nil
}

func (e gameplaySessionLifecycle) disconnectServerBots(owner gameplayPeerSession, modifierInstancePool *modifierPool, effectPool *attachedEffectPool) {
	if owner.binding.IsServerBot {
		return
	}
	e.registry.mutex.RLock()
	keys := make([]string, 0)
	for key, member := range e.registry.sessions {
		if member.binding.IsServerBot && member.binding.GameID == owner.binding.GameID && member.binding.BotOwnerUserID == owner.binding.UserID {
			keys = append(keys, key)
		}
	}
	e.registry.mutex.RUnlock()
	for _, key := range keys {
		isDisconnected := e.disconnectMember(key, modifierInstancePool, effectPool)
		if !isDisconnected && e.registry.logger != nil {
			e.registry.logger.Printf("Arena server bot retirement deferred remote=%s", key)
		}
	}
}

// A synthetic campaign peer owns the same hero and passive runtime as a
// connected player. Publish only its public roster; its owner's setup already
// supplies the world snapshot and selects the human's controlled object.
func (e gameplaySetupRuntime) publishCampaignServerBot(
	packet raknet.Packet, member gameplayPeerSession, setupEpoch uint64,
	position raknet.Vector3, shieldPackets [][]byte, dronePackets [][]byte,
) ([][]byte, bool, error) {
	packets, err := marshalZoneHeroRoster(zoneprojection.HeroRoster{
		UserID: member.binding.UserID, PlayerSlot: member.binding.Slot,
		Team: member.binding.Team, Roster: member.binding.Roster(),
		Position: zonePosition(position), CreatureIndex: member.deployedCreatureIndex,
	})
	if err != nil {
		return nil, false, fmt.Errorf("botRoster: %w", err)
	}
	beamPackets, err := heroraknet.BeamIn(member.deployedObjectID,
		campaignCharacterEntry(member.binding.Creatures[member.deployedCreatureIndex]),
		zonePosition(position), packet.SourceTime)
	if err != nil {
		return nil, false, fmt.Errorf("botArrival: %w", err)
	}
	packets = append(packets, beamPackets...)
	packets = append(packets, shieldPackets...)
	packets = append(packets, dronePackets...)
	member.zone.SetCampaignBot(zonecheckpoint.CampaignBot{
		Request: game.BotRequest{
			OwnerUserID: member.binding.BotOwnerUserID, Slot: member.binding.Slot,
			Team: member.binding.Team, DisplayName: member.binding.BotDisplayName, AvatarID: member.binding.AvatarID,
			SourceSquadID:            member.binding.BotSourceSquadID,
			IsSquadMirroringDisabled: member.binding.IsBotSquadMirroringDisabled,
			Roster:                   member.binding.Roster(), IsAppearanceResolved: true,
		},
		CreatureIndex: member.deployedCreatureIndex, Position: game.Vec3(position), HitPoint: member.deployedHitPoint(),
	})
	e.registry.mutex.Lock()
	current, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || current.generation != member.generation || !current.dungeonSetup.Commit(setupEpoch) {
		e.registry.mutex.Unlock()
		return nil, false, errors.New("campaign bot setup stale")
	}
	e.registry.sessions[packet.Address.String()] = current
	e.registry.mutex.Unlock()
	passivePacket, err := e.passive.start(packet, packet.Address.String(), member.generation)
	if err != nil {
		return nil, false, fmt.Errorf("botPassive: %w", err)
	}
	if passivePacket != nil {
		packets = append(packets, passivePacket)
	}
	err = e.energyPassive.start(packet, packet.Address.String(), member.generation)
	if err != nil {
		return nil, false, fmt.Errorf("botEnergy: %w", err)
	}
	err = startTrapperStealth(e.damage, packet, packet.Address.String(), member.generation)
	if err != nil {
		return nil, false, fmt.Errorf("botStealth: %w", err)
	}
	lightspeedPackets, err := startLightspeedPassive(e.damage, packet, packet.Address.String(), member.generation)
	if err != nil {
		return nil, false, fmt.Errorf("botLightspeed: %w", err)
	}
	packets = append(packets, lightspeedPackets...)
	soulPackets, err := startSoulRavagerPresentation(e.damage, packet.Address.String(), member.generation)
	if err != nil {
		return nil, false, fmt.Errorf("botSoul: %w", err)
	}
	packets = append(packets, soulPackets...)
	publication := serverBotRosterPublication{registry: e.registry, member: member, packets: clonePendingPackets(packets)}
	err = packet.AfterResponseCommit(publication.commit)
	if err != nil {
		return nil, false, fmt.Errorf("botPeerArrival: %w", err)
	}
	arrival := heroArrivalCommit{registry: e.registry, sessionKey: packet.Address.String(),
		generation: member.generation, objectID: member.deployedObjectID, now: e.now}
	err = packet.AfterResponseCommit(arrival.commit)
	if err != nil {
		return nil, false, fmt.Errorf("botArrivalCommit: %w", err)
	}
	sagePackets, err := e.startSagePassive(packet, packet.Address.String(), member.generation, e.now())
	if err != nil {
		return nil, false, fmt.Errorf("botSage: %w", err)
	}
	packets = append(packets, sagePackets...)
	return packets, true, nil
}

type serverBotRosterPublication struct {
	registry *gameplaySessionRegistry
	member   gameplayPeerSession
	packets  [][]byte
}

func (e serverBotRosterPublication) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	current, isFound := e.registry.sessions[serverBotAddress(e.member.binding).String()]
	if !isFound || current.generation != e.member.generation || current.zone != e.member.zone {
		return
	}
	for key, candidate := range e.registry.sessions {
		if candidate.binding.IsServerBot || candidate.binding.UserID == e.member.binding.BotOwnerUserID ||
			candidate.zone != current.zone || !candidate.isCampaignPresentationAvailable() || candidate.isRejoinPending {
			continue
		}
		err := candidate.queueCampaignPresentation(e.packets)
		if err != nil && e.registry.logger != nil {
			e.registry.logger.Printf("Campaign bot roster publication failed user=%d: %v", candidate.binding.UserID, err)
		}
		e.registry.sessions[key] = candidate
	}
}

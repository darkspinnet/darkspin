package gameplay

import (
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/snapshot"
)

func snapshotHeroSpeed(peerSession gameplayPeerSession) *snapshot.HeroSpeedState {
	if peerSession.deployedObjectID == 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return nil
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	attackSpeed := peerSession.enemyAttackSpeed()
	movementSpeed := peerSession.enemyMovementSpeedBuff()
	return &snapshot.HeroSpeedState{
		ObjectID:                peerSession.deployedObjectID,
		EquipmentAttackSpeed:    creature.PartAttribute[23],
		EquipmentMovementSpeed:  creature.PartAttribute[48],
		PassiveMovementIncrease: creature.PassiveMovementIncrease,
		DebuffAttackSpeed:       attackSpeed, DebuffMovementSpeed: movementSpeed,
		ExpectedSlowAttribute23: creature.PartAttribute[23] + attackSpeed,
		ExpectedSlowAttribute48: creature.PartAttribute[48] + movementSpeed,
	}
}

func snapshotEnemyDamageTicks(peerSession gameplayPeerSession) []snapshot.DamageTickState {
	if len(peerSession.campaignNPCPoisons) == 0 {
		return nil
	}
	modifiers := peerSession.zone.Effect().Snapshot()
	instances := make(map[uint32]struct{}, len(modifiers))
	for _, modifier := range modifiers {
		instances[modifier.InstanceID] = struct{}{}
	}
	states := make([]snapshot.DamageTickState, 0, len(peerSession.campaignNPCPoisons))
	for _, run := range peerSession.campaignNPCPoisons {
		if run == nil || run.modifier == nil {
			continue
		}
		profile := run.plan.Profile
		_, isInInventory := instances[run.modifier.instanceID]
		states = append(states, snapshot.DamageTickState{
			ModifierName: run.key.modifierName, GUID: profile.ModifierGUID(),
			InstanceID:     run.modifier.instanceID,
			SourceObjectID: run.plan.SourceObjectID, TargetObjectID: run.key.targetObjectID,
			AbilityName: profile.AbilityName, Revision: run.revision,
			StackCount:        run.stackCount,
			MinimumTickDamage: run.tickDamage.Minimum, MaximumTickDamage: run.tickDamage.Maximum,
			TickDurationMS: profile.ModifierTickDuration.Milliseconds(),
			DurationMS:     profile.ModifierDuration.Milliseconds(),
			DamageType:     profile.DamageType, DamageSource: profile.DamageSource,
			IsInModifierInventory:   isInInventory,
			IsTransferGUIDSupported: isFieldMedicStatusSupported(profile.ModifierGUID()),
		})
	}
	sort.Slice(states, func(left, right int) bool {
		return states[left].InstanceID < states[right].InstanceID
	})
	return states
}

func snapshotCryosGeysers(
	peerSession gameplayPeerSession, capturedAt time.Time,
) []snapshot.GeyserState {
	geysers := peerSession.zone.CryosGeyserSnapshot()
	states := make([]snapshot.GeyserState, 0, len(geysers))
	for _, geyser := range geysers {
		objectID, cycle := geyser.ObjectID, geyser.Cycle
		phaseName := "rest"
		if !capturedAt.Before(geyser.SpoutAt) && capturedAt.Before(geyser.EndsAt) {
			phaseName = "spout"
		} else if !capturedAt.Before(geyser.WarningAt) && capturedAt.Before(geyser.SpoutAt) {
			phaseName = "warning"
		}
		warningCycle, isWarningRecorded := peerSession.cryosGeyserWarnings[objectID]
		spoutCycle, isSpoutRecorded := peerSession.cryosGeyserSpouts[objectID]
		states = append(states, snapshot.GeyserState{
			MarkerID: geyser.MarkerID, ObjectID: objectID, Position: vec3(geyser.Position),
			Phase: phaseName, Cycle: cycle, IsActive: capturedAt.Before(geyser.EndsAt),
			WarningCycle: warningCycle, SpoutCycle: spoutCycle,
			IsWarningRecorded: isWarningRecorded, IsSpoutRecorded: isSpoutRecorded,
		})
	}
	sort.Slice(states, func(left, right int) bool {
		return states[left].MarkerID < states[right].MarkerID
	})
	return states
}

package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Allocation, UniqueResets lookup and creation are one registry transaction:
// simultaneous egg deaths cannot both reserve the same target/GUID namespace.
func (e campaignFixtureBlastStep) startEggPoison(targetID uint32, profile zonenpc.ActionProfile) ([][]byte, error) {
	r := e.runtime
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil || member.zone.NPCs() != e.npc || member.isZoneTerminal() || r.registry.isFixtureDiscardingLocked(member.binding.GameID) {
		return nil, nil
	}
	target, isTargetFound := member.zone.NPCs().LiveNPC(targetID)
	if !isTargetFound || !target.IsPublished || target.Faction != e.source.Faction {
		return nil, nil
	}
	now := r.npc.now()
	isAdmitted, err := r.npc.poisonAdmissionLocked(member, targetID, now)
	if err != nil {
		return nil, fmt.Errorf("eggImmunity: %w", err)
	}
	if !isAdmitted {
		return nil, nil
	}
	refreshed, isRefreshed, err := e.refreshEggPoisonLocked(member, targetID, now)
	if err != nil {
		return nil, fmt.Errorf("eggRefresh: %w", err)
	}
	if isRefreshed {
		return refreshed, nil
	}
	if e.sourceProfile == nil {
		return nil, errors.New("egg initiator snapshot unavailable")
	}
	run, err := newCampaignNPCModifierRun(r.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("eggAllocate: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: util.HashID("EggPoison"),
		SourceObjectID: e.source.Plan.ObjectID, InitiatorObject: e.source.Plan.ObjectID,
		TargetObjectID: targetID, Rank: uint32(zonenpc.EffectivePassiveRank(e.source.Plan)),
		Duration: profile.ModifierDuration, ExpiresAt: now.Add(profile.ModifierDuration),
		Kind: zoneeffect.ModifierKindDebuff, StackCount: 1,
	}
	blast := e
	blast.publication = nil
	run.fixtureBurn = &campaignFixtureBurnState{
		blast: blast, profile: profile, npc: e.npc, sourceProfile: *e.sourceProfile,
		startedAt: now, expiresAt: run.record.ExpiresAt, sourceTimestamp: e.timestamp,
		nextTickDeadline: profile.ModifierTickDuration, inventory: member.zone.Effect(),
		handoffGeneration: 1, publication: e.publication,
	}
	// Legacy SourceID is native NPCAffixAssetID. Ordinary EggPoison has none.
	created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: targetID, ModifierGUID: run.record.GUID, InstanceID: run.instanceID,
		StackCount: 1, StartMilliseconds: e.timestamp,
		DurationMilliseconds: uint32(profile.ModifierDuration.Milliseconds()),
	})
	if err == nil {
		err = run.fixtureBurn.inventory.Put(run.record)
	}
	if err == nil {
		err = member.trackCampaignNPCModifier(run)
	}
	if err == nil {
		if !run.create() {
			err = errors.New("egg modifier reservation unavailable")
		}
	}
	if err == nil {
		err = r.npc.attachEggPoisonLocked(member, run)
	}
	if err != nil {
		member.untrackCampaignNPCModifier(run)
		r.registry.sessions[e.sessionKey] = member
		isCreated, releaseErr := run.release(r.npc.modifierPool)
		// Creation was not returned or staged, so there is no wire A4.
		return nil, fmt.Errorf("eggAdmission[%t]: %w", isCreated, errors.Join(err, releaseErr))
	}
	// attachEggPoisonLocked may queue channel ownership onto this session.
	// Preserve those queued packets instead of overwriting with our old copy.
	current := r.registry.sessions[e.sessionKey]
	current.campaignNPCModifiers = member.campaignNPCModifiers
	r.registry.sessions[e.sessionKey] = current
	step := campaignFixtureBurnStep{blast: blast, run: run, targetID: targetID,
		profile: profile, deadline: profile.ModifierTickDuration, handoffGeneration: 1}
	e.publication.stagedBurns = append(e.publication.stagedBurns, step)
	return [][]byte{created}, nil
}

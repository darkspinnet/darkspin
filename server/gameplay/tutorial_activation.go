package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zone "github.com/darkspinnet/darkspin/server/zone"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
	unlockraknet "github.com/darkspinnet/darkspin/server/zone/unlock/raknet103"
)

type campaignTutorialActivation struct {
	runtime          campaignEncounterRuntime
	zone             *zone.Zone
	triggerMember    zone.Member
	members          []zone.Member
	callback         string
	isAnyUnbeaten    bool
	plan             zone.NamedBossPlan
	isHorde          bool
	activationHeroID uint32
	sourceTime       uint64
}

func isConnectedTutorialMember(campaignZone *zone.Zone, member zone.Member) bool {
	for _, current := range campaignZone.Snapshot().Members {
		if current.UserID == member.UserID && current.PeerGeneration == member.PeerGeneration {
			return current.IsConnected
		}
	}
	return false
}

func hasBeatenTutorialLevel(peerSession gameplayPeerSession) bool {
	return zoneunlock.HasBeatenThisLevel(peerSession.binding,
		peerSession.zone.TutorialMajorStageCount(), peerSession.zone.CrystalMinorStageCount())
}

// The registry lock protects the participant bindings. Enumerate the actual
// connected identities; sparse player slots do not imply missing participants.
func (e campaignEncounterRuntime) newTutorialActivationLocked(
	peerSession gameplayPeerSession, callback string,
) campaignTutorialActivation {
	step := campaignTutorialActivation{
		runtime: e, zone: peerSession.zone, callback: callback,
		triggerMember: zoneResultMember(peerSession),
		sourceTime:    peerSession.tutorialActivationSourceTime,
	}
	for _, member := range peerSession.zone.Snapshot().Members {
		if !member.IsConnected {
			continue
		}
		step.members = append(step.members, member)
		isParticipantFound := false
		for _, participant := range e.registry.sessions {
			if participant.zone != peerSession.zone || participant.binding.UserID != member.UserID ||
				participant.generation != member.PeerGeneration {
				continue
			}
			isParticipantFound = true
			if !hasBeatenTutorialLevel(participant) {
				step.isAnyUnbeaten = true
			}
			break
		}
		if !isParticipantFound {
			// A missing native player fails HasBeatenThisLevel. Mutation and
			// activation still require a current connected membership.
			step.isAnyUnbeaten = true
		}
	}
	return step
}

func (e campaignEncounterRuntime) startTutorialActivationLocked(
	peerSession *gameplayPeerSession, plan zone.NamedBossPlan, isHorde bool,
) error {
	if len(plan.Actors) == 0 || e.timer == nil || peerSession.zone.Timeline() == nil {
		return errors.New("tutorial activation authority unavailable")
	}
	key := fmt.Sprintf("tutorial:%s:%d:%d", plan.Publication.CallbackName,
		plan.Publication.MarkerSetOrdinal, plan.Publication.TriggerMarkerID)
	if !peerSession.zone.TutorialActivations().Claim(key) {
		return nil
	}
	step := e.newTutorialActivationLocked(*peerSession, plan.Publication.CallbackName)
	step.plan = plan
	step.isHorde = isHorde
	if step.isAnyUnbeaten {
		err := step.scheduleMutation(key + ":mutation")
		if err != nil {
			peerSession.zone.TutorialActivations().Release(key)
			return fmt.Errorf("mutationSchedule: %w", err)
		}
	}
	err := peerSession.zone.Timeline().Schedule(key,
		zoneunlock.TutorialActivationDelay(step.callback, step.isAnyUnbeaten),
		step.activate, e.timer.Schedule)
	if err != nil {
		peerSession.zone.Timeline().Cancel(key + ":mutation")
		peerSession.zone.TutorialActivations().Release(key)
		return fmt.Errorf("activationSchedule: %w", err)
	}
	return nil
}

func (e campaignTutorialActivation) scheduleMutation(key string) error {
	err := e.zone.Timeline().Schedule(key, zoneunlock.TutorialMutationDelay(e.callback),
		e.mutate, e.runtime.timer.Schedule)
	if err != nil {
		return fmt.Errorf("tutorialMutation: %w", err)
	}
	return nil
}

func (e campaignTutorialActivation) mutate() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	for _, member := range e.members {
		if !isConnectedTutorialMember(e.zone, member) {
			continue
		}
		for sessionKey, peerSession := range e.runtime.registry.sessions {
			if peerSession.zone != e.zone || peerSession.binding.UserID != member.UserID ||
				peerSession.generation != member.PeerGeneration {
				continue
			}
			isBeaten := hasBeatenTutorialLevel(peerSession)
			// Catalyst's long branch emits its drop call for beaten allies too;
			// only the unlock mutation is conditional at the six-second boundary.
			if isBeaten && e.callback != zonecallback.CatalystUnlock {
				continue
			}
			packets, err := e.mutateParticipant(&peerSession, isBeaten)
			if err != nil {
				e.runtime.logger.Printf("Tutorial mutation failed user=%d callback=%q: %v", member.UserID, e.callback, err)
				continue
			}
			e.runtime.registry.sessions[sessionKey] = peerSession
			for recipientKey, recipient := range e.runtime.registry.sessions {
				if recipient.zone == e.zone {
					recipient.queuePackets(packets)
					e.runtime.registry.sessions[recipientKey] = recipient
				}
			}
			break
		}
	}
}

func (e campaignTutorialActivation) mutateParticipant(
	peerSession *gameplayPeerSession, isBeaten bool,
) ([][]byte, error) {
	deadline := zoneunlock.TutorialMutationDelay(e.callback)
	if e.callback == zonecallback.CatalystUnlock {
		run, err := unlockraknet.NewCatalystRun(e.zone.CatalystProgram(), uint8(peerSession.binding.Slot))
		if err != nil {
			return nil, fmt.Errorf("catalystRun: %w", err)
		}
		defer run.Stop()
		batch, err := run.Advance(e.zone.Context(), deadline)
		if err != nil {
			return nil, fmt.Errorf("catalystAdvance: %w", err)
		}
		if isBeaten {
			batch.Packets = nil
		} else {
			peerSession.binding.IsCatalystUnlocked = true
		}
		hero, isFound := e.zone.Hero().Snapshot(peerSession.binding.UserID, peerSession.generation)
		if !isFound {
			return batch.Packets, nil
		}
		packets, err := peerSession.materializeCampaignCatalystUnlock(batch, sim.Position(hero.Position), time.Duration(e.sourceTime)*time.Millisecond+deadline)
		if err != nil {
			return nil, fmt.Errorf("catalystMaterialize: %w", err)
		}
		return packets, nil
	}
	if e.callback == zonecallback.OverdriveUnlock {
		run, err := unlockraknet.NewOverdriveRun(e.zone.OverdriveProgram(), uint8(peerSession.binding.Slot))
		if err != nil {
			return nil, fmt.Errorf("overdriveRun: %w", err)
		}
		defer run.Stop()
		packets, err := run.Advance(e.zone.Context(), deadline)
		if err != nil {
			return nil, fmt.Errorf("overdriveAdvance: %w", err)
		}
		peerSession.binding.IsOverdriveUnlocked = true
		peerSession.overdriveEnergy = float32(campaignOverdriveMaximumEnergy)
		peerSession.isOverdrivePersistencePending = true
		return packets, nil
	}
	abilityCount, isFound := e.zone.AbilityCount(zoneResultMember(*peerSession))
	if !isFound {
		return nil, nil
	}
	run, err := unlockraknet.NewSoloSupportRun(e.runtime.program.SoloSupportUnlock,
		uint8(peerSession.binding.Slot), abilityCount)
	if err != nil {
		return nil, fmt.Errorf("supportRun: %w", err)
	}
	defer run.Stop()
	packets, err := run.Advance(e.zone.Context(), deadline)
	if err != nil {
		return nil, fmt.Errorf("supportAdvance: %w", err)
	}
	nextAbilityCount := abilityCount + 1
	if nextAbilityCount > zoneunlock.FullAbilityBoundary {
		nextAbilityCount = zoneunlock.SupportAbilityBoundary
	}
	e.zone.RaiseAbilityCount(zoneResultMember(*peerSession), nextAbilityCount)
	return packets, nil
}

func (e campaignTutorialActivation) activate() {
	// Resolve the current controlled hero here, after all authored waits.
	if !isConnectedTutorialMember(e.zone, e.triggerMember) {
		return
	}
	hero, isFound := e.zone.Hero().Snapshot(e.triggerMember.UserID, e.triggerMember.PeerGeneration)
	if !isFound || hero.ObjectID == 0 {
		return
	}
	e.runtime.logger.Printf("Tutorial ActivateHordeSpawn reached callback=%q user=%d hero=%d", e.callback, e.triggerMember.UserID, hero.ObjectID)
	// Admission retains the existing server policy after the script boundary.
	e.activationHeroID = hero.ObjectID
	admissionDelay := zoneboss.InitialAdmissionDelay
	if e.isHorde {
		admissionDelay = zonehorde.InitialAdmissionDelay
	}
	err := e.zone.Timeline().Schedule(fmt.Sprintf("tutorial-admission:%d", e.plan.Actors[0].ObjectID),
		admissionDelay, e.admit, e.runtime.timer.Schedule)
	if err != nil {
		e.runtime.logger.Printf("Tutorial admission schedule failed: %v", err)
	}
}

func (e campaignTutorialActivation) admit() {
	e.runtime.registry.mutex.Lock()
	peerSession, sessionKey, plans, err := e.admitLocked()
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		e.runtime.logger.Printf("Tutorial admission failed: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}
	timestamp := e.sourceTime + uint64(zoneunlock.TutorialActivationDelay(e.callback, e.isAnyUnbeaten)/time.Millisecond)
	actionPackets, err := e.runtime.npc.scheduleFirstActions(peerSession.schedulePacket,
		sessionKey, peerSession.generation, plans, timestamp)
	if err != nil {
		e.runtime.logger.Printf("Tutorial admitted NPC actions failed: %v", err)
		return
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	for recipientKey, recipient := range e.runtime.registry.sessions {
		if recipient.zone == e.zone {
			recipient.queuePackets(actionPackets)
			e.runtime.registry.sessions[recipientKey] = recipient
		}
	}
}

func (e campaignTutorialActivation) admitLocked() (gameplayPeerSession, string, []zonenpc.SpawnPlan, error) {
	if !isConnectedTutorialMember(e.zone, e.triggerMember) {
		return gameplayPeerSession{}, "", nil, nil
	}
	for sessionKey, peerSession := range e.runtime.registry.sessions {
		if peerSession.zone != e.zone || peerSession.binding.UserID != e.triggerMember.UserID ||
			peerSession.generation != e.triggerMember.PeerGeneration {
			continue
		}
		if e.isHorde {
			packets, err := e.admitHorde()
			if err != nil {
				return gameplayPeerSession{}, "", nil, fmt.Errorf("tutorialHorde: %w", err)
			}
			for recipientKey, recipient := range e.runtime.registry.sessions {
				if recipient.zone == e.zone {
					recipient.queuePackets(packets)
					e.runtime.registry.sessions[recipientKey] = recipient
				}
			}
			return peerSession, sessionKey, e.plan.Actors, nil
		}
		plans, packets, isAdmitted, err := peerSession.admitCampaignGenericBossNow(e.plan, e.activationHeroID)
		if err != nil {
			return gameplayPeerSession{}, "", nil, fmt.Errorf("tutorialBoss: %w", err)
		}
		if !isAdmitted {
			return gameplayPeerSession{}, "", nil, nil
		}
		if e.runtime.logger != nil {
			bossState := e.zone.Boss().Snapshot()
			e.runtime.logger.Printf(
				"Campaign boss admission stage=admitted game=%d level=%q marker_set=%q trigger=%d leader=%d phase=%d deferred=%t source=tutorial",
				peerSession.binding.GameID, peerSession.binding.Level,
				e.plan.Publication.MarkerSetName,
				e.plan.NamedPublication.SourceObjectID,
				bossState.LeaderObjectID, bossState.Phase,
				bossState.IsLeaderDeferred,
			)
		}
		peerSession.isClientBossBoundaryPending = false
		peerSession.queuePackets(packets)
		e.runtime.registry.sessions[sessionKey] = peerSession
		err = e.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: plans,
			TargetObjectID: e.activationHeroID,
			IsBossActive:   !(e.plan.Actors[0].IsCaptain && len(e.plan.Actors) > 1),
			BossObjectID:   e.plan.Actors[0].ObjectID, IsFinalBoss: zoneboss.IsFinalBossNoun(e.plan.Actors[0].NounName)}, peerSession.binding.UserID, peerSession.generation)
		if err != nil {
			return gameplayPeerSession{}, "", nil, fmt.Errorf("tutorialPublish: %w", err)
		}
		if e.runtime.logger != nil {
			bossState := e.zone.Boss().Snapshot()
			e.runtime.logger.Printf(
				"Campaign boss admission stage=published game=%d level=%q marker_set=%q leader=%d actors=%d deferred=%t source=tutorial",
				peerSession.binding.GameID, peerSession.binding.Level,
				e.plan.Publication.MarkerSetName,
				bossState.LeaderObjectID, len(plans), bossState.IsLeaderDeferred,
			)
			if !bossState.IsLeaderDeferred &&
				!isCampaignBossIntroDelayed(e.plan.Actors[0]) {
				e.runtime.logger.Printf(
					"Campaign boss admission stage=active game=%d level=%q marker_set=%q leader=%d source=tutorial",
					peerSession.binding.GameID, peerSession.binding.Level,
					e.plan.Publication.MarkerSetName,
					bossState.LeaderObjectID,
				)
			}
		}
		return peerSession, sessionKey, plans, nil
	}
	return gameplayPeerSession{}, "", nil, nil
}

func (e campaignTutorialActivation) admitHorde() ([][]byte, error) {
	packets, err := npcraknet.TargetedSpawns(e.plan.Actors, e.activationHeroID)
	if err != nil {
		return nil, fmt.Errorf("hordeMarshal: %w", err)
	}
	barrierPlans := e.zone.HordeBarrierPlans(e.plan.Publication.MarkerSetName)
	if len(barrierPlans) != 0 {
		barrierPackets, err := barrierraknet.Create(barrierPlans)
		if err != nil {
			return nil, fmt.Errorf("barrierMarshal: %w", err)
		}
		packets = append(barrierPackets, packets...)
	}
	statePacket, err := raknet.MarshalApplication(raknet.DirectorStateMessage{
		IsHordeSpawned: true, IsHordeSpawnedPresent: true,
	})
	if err != nil {
		return nil, fmt.Errorf("hordeState: %w", err)
	}
	err = e.zone.AdmitFirstHorde(e.plan.Publication, e.activationHeroID, e.plan.Actors)
	if err != nil {
		return nil, fmt.Errorf("hordeAdmit: %w", err)
	}
	return append(packets, statePacket), nil
}

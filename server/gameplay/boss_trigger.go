package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type campaignNamedBossTriggerStep struct {
	runtime     campaignEncounterRuntime
	zone        *zone.Zone
	publication game.CampaignDirectorPublication
	sessionKey  string
	generation  uint64
	center      game.Vec3
	radius      float32
	attempt     uint8
}

func (e campaignNamedBossTriggerStep) execute() {
	err := e.admit()
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Campaign named boss trigger admission failed marker_set=%q marker=%d attempt=%d: %v",
			e.publication.MarkerSetName, e.publication.TriggerMarkerID,
			e.attempt+1, err,
		)
	}
	if err != nil && e.attempt < 2 && e.zone != nil &&
		e.zone.Timeline() != nil && e.runtime.timer != nil {
		retry := e
		retry.attempt++
		key := fmt.Sprintf("named-boss:%d:%d", e.publication.TriggerMarkerID, e.publication.EventOrdinal)
		scheduleErr := e.zone.Timeline().Schedule(
			key, time.Second, retry.execute, e.runtime.timer.Schedule,
		)
		if scheduleErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"Campaign named boss trigger retry unavailable marker_set=%q marker=%d: %v",
				e.publication.MarkerSetName, e.publication.TriggerMarkerID,
				scheduleErr,
			)
		}
	}
}

func (e campaignNamedBossTriggerStep) admit() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		!e.isEligibleSession(peerSession) {
		isFound = false
		for _, candidate := range e.runtime.registry.sessions {
			if !e.isEligibleSession(candidate) {
				continue
			}
			if isFound && candidate.binding.UserID >= peerSession.binding.UserID {
				continue
			}
			peerSession = candidate
			isFound = true
		}
	}
	if !isFound {
		return nil
	}
	if e.zone.Boss() == nil || !e.zone.Boss().IsDormant() {
		return nil
	}
	err := e.zone.CanAcceptPublication(e.publication)
	if err != nil {
		return fmt.Errorf("bossTriggerPending: %w", err)
	}
	encounter, err := e.zone.PlanBossFromTrigger(
		e.publication, peerSession.binding.GameID,
		peerSession.binding.ChainLevelIndex,
	)
	if err != nil {
		return fmt.Errorf("bossTriggerPlan: %w", err)
	}
	if len(encounter.Actors) == 0 {
		return fmt.Errorf("bossTriggerPlan: empty encounter")
	}
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Campaign boss admission stage=planned game=%d level=%q marker_set=%q trigger=%d anchor=%d leader=%q actors=%d deferred=%t target=%d",
			peerSession.binding.GameID, peerSession.binding.Level, e.publication.MarkerSetName,
			e.publication.TriggerMarkerID, encounter.Publication.TriggerMarkerID,
			encounter.Actors[0].NounName, len(encounter.Actors),
			encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1,
			peerSession.deployedObjectID,
		)
	}
	livePlans := encounter.Actors
	if encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1 {
		livePlans = encounter.Actors[1:]
	}
	packets, err := npcraknet.TargetedSpawns(livePlans, peerSession.deployedObjectID)
	if err != nil {
		return fmt.Errorf("bossTriggerMarshal: %w", err)
	}
	if len(packets) == 0 {
		return fmt.Errorf("bossTriggerMarshal: no packets for %d actors", len(livePlans))
	}
	if !(encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1) &&
		!isCampaignBossIntroDelayed(encounter.Actors[0]) {
		activePacket, activeErr := bossraknet.Active(
			encounter.Actors[0].ObjectID,
			zoneboss.IsFinalBossNoun(encounter.Actors[0].NounName),
		)
		if activeErr != nil {
			return fmt.Errorf("bossTriggerActive: %w", activeErr)
		}
		if len(activePacket) == 0 {
			return fmt.Errorf("bossTriggerActive: empty packet")
		}
	}
	err = e.zone.AdmitTriggeredNamedBossEncounter(
		encounter.NamedPublication, encounter.Publication, e.publication,
		peerSession.deployedObjectID, encounter.Actors,
	)
	if err != nil {
		return fmt.Errorf("bossTriggerCommit: %w", err)
	}
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Campaign boss admission stage=admitted game=%d level=%q marker_set=%q trigger=%d leader=%d deferred=%t",
			peerSession.binding.GameID, peerSession.binding.Level, e.publication.MarkerSetName,
			e.publication.TriggerMarkerID, encounter.Actors[0].ObjectID,
			encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1,
		)
	}
	err = e.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: livePlans, TargetObjectID: peerSession.deployedObjectID,
		IsBossAddPhase: encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1,
		IsBossActive:   !(encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1),
		BossObjectID:   encounter.Actors[0].ObjectID,
		IsFinalBoss:    zoneboss.IsFinalBossNoun(encounter.Actors[0].NounName),
	}, 0, 0)
	if err != nil {
		return fmt.Errorf("bossTriggerPublish: %w", err)
	}
	if e.runtime.logger != nil {
		bossState := e.zone.Boss().Snapshot()
		e.runtime.logger.Printf(
			"Campaign boss admission stage=published game=%d level=%q marker_set=%q trigger=%d actors=%d leader=%d phase=%d deferred=%t subscribers=all",
			peerSession.binding.GameID, peerSession.binding.Level, e.publication.MarkerSetName,
			e.publication.TriggerMarkerID, len(livePlans),
			bossState.LeaderObjectID, bossState.Phase,
			bossState.IsLeaderDeferred,
		)
		if !bossState.IsLeaderDeferred &&
			!isCampaignBossIntroDelayed(encounter.Actors[0]) {
			e.runtime.logger.Printf(
				"Campaign boss admission stage=active game=%d level=%q marker_set=%q leader=%d source=trigger",
				peerSession.binding.GameID, peerSession.binding.Level, e.publication.MarkerSetName,
				bossState.LeaderObjectID,
			)
		}
	}
	return nil
}

func (e campaignNamedBossTriggerStep) isEligibleSession(
	peerSession gameplayPeerSession,
) bool {
	if peerSession.zone != e.zone || peerSession.isRejoinPending ||
		peerSession.binding.Mode == game.ModeTutorial ||
		peerSession.isZoneTerminal() ||
		peerSession.deployedObjectID == 0 || peerSession.deployedHitPoint() <= 0 {
		return false
	}
	if e.publication.IsDwellComplete {
		return true
	}
	position := game.Vec3(peerSession.playerPosition)
	deltaX := position.X - e.center.X
	deltaY := position.Y - e.center.Y
	deltaZ := position.Z - e.center.Z
	return deltaX*deltaX+deltaY*deltaY+deltaZ*deltaZ <= e.radius*e.radius
}

func (r campaignEncounterRuntime) scheduleNamedBossTriggers(
	sessionKey string, commandSession gameplayPeerSession,
	publications []game.CampaignDirectorPublication,
) error {
	// The tutorial reuses this callback on its horde arena marker, but its
	// wave session owns progression and completion. It has no campaign boss
	// roster or chain stage for the named-boss planner to select from.
	if commandSession.binding.Mode == game.ModeTutorial {
		return nil
	}
	isNamedBossTrigger := false
	for _, publication := range publications {
		if publication.CallbackName == zoneboss.GenericCallback {
			isNamedBossTrigger = true
			break
		}
	}
	if !isNamedBossTrigger {
		return nil
	}
	if commandSession.zone == nil || r.timer == nil {
		return fmt.Errorf("bossTriggerScheduler: unavailable")
	}
	if commandSession.zone.Timeline() == nil {
		return fmt.Errorf("bossTriggerTimeline: unavailable")
	}
	for _, publication := range publications {
		if publication.CallbackName != zoneboss.GenericCallback {
			continue
		}
		center, radius, delay, err := commandSession.zone.DirectorDefinition().
			BossTriggerActivation(publication)
		if err != nil {
			return fmt.Errorf("bossTriggerActivation: %w", err)
		}
		if publication.IsDwellComplete {
			// The shared zone trigger already completed authored dwell.
			delay = 0
		}
		key := fmt.Sprintf("named-boss:%d:%d", publication.TriggerMarkerID, publication.EventOrdinal)
		if isNamedBossTriggerScheduled(commandSession.zone, key) {
			continue
		}
		step := campaignNamedBossTriggerStep{
			runtime: r, zone: commandSession.zone, publication: publication,
			sessionKey: sessionKey, generation: commandSession.generation,
			center: center, radius: radius,
		}
		err = commandSession.zone.Timeline().Schedule(key, delay, step.execute, r.timer.Schedule)
		if err != nil {
			if isNamedBossTriggerScheduled(commandSession.zone, key) {
				continue
			}
			return fmt.Errorf("bossTriggerSchedule: %w", err)
		}
	}
	return nil
}

func isNamedBossTriggerScheduled(currentZone *zone.Zone, key string) bool {
	if currentZone == nil || currentZone.Timeline() == nil {
		return false
	}
	for _, activeKey := range currentZone.Timeline().Keys() {
		if strings.EqualFold(activeKey, key) {
			return true
		}
	}
	return false
}

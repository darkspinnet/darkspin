package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type drainerDeathRun struct {
	runtime        campaignNPCActionRuntime
	key            companionDeathKey
	gameID         uint32
	userID         uint64
	generation     uint64
	source         zonenpc.Snapshot
	plan           zonenpc.AttackPlan
	criticalRating float32
	deadline       time.Time
	timestamp      uint64
	deletePacket   []byte
	cancel         func()
}

func (e *drainerDeathRun) execute() {
	registry := e.runtime.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if registry.drainerDeaths[e.key] != e {
		return
	}
	delete(registry.drainerDeaths, e.key)
	e.cancel = nil
	current, isFound := e.key.world.NPCs().NPC(e.key.objectID)
	if registry.isClosed || !isFound || !current.IsPublished || !current.IsDefeated || current.ActionGeneration != e.source.ActionGeneration ||
		current.Plan.NounName != e.source.Plan.NounName {
		return
	}
	if !e.key.world.IsActive() {
		e.finishLocked()
		return
	}
	for _, target := range e.runtime.drainerDeathTargetsLocked(e.key.world, e.plan.SourcePosition) {
		err := e.hitLocked(target)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Drainer death burst source=%d target=%d: %v", e.key.objectID, target.ObjectID, err)
		}
	}
	e.finishLocked()
}

func (e *drainerDeathRun) hitLocked(target zone.NPCTarget) error {
	registry := e.runtime.registry
	for sessionKey, driver := range registry.sessions {
		if driver.zone != e.key.world || driver.isZoneTerminal() || driver.binding.UserID != target.UserID || driver.generation != target.PeerGeneration {
			continue
		}
		current, isFound := e.runtime.drainerDeathTargetLocked(e.key.world, target.ObjectID)
		if !isFound || current.Position.Sub(e.plan.SourcePosition).Length() > 6 {
			return nil
		}
		plan := e.plan
		plan.TargetObjectID = current.ObjectID
		plan.TargetPosition = current.Position
		result, err := zonenpc.CommitAttack(e.key.world.NPCRandom(), plan, e.criticalRating, e.runtime.program.Critical)
		if err != nil {
			return fmt.Errorf("drainerRoll: %w", err)
		}
		hitEffect, err := npcraknet.DeathDetonation(current.ObjectID, "shadow_bolt_impact.ServerEventDef")
		if err != nil {
			return fmt.Errorf("drainerImpact: %w", err)
		}
		packets, statDelta, isApplied, err := e.runtime.applyEnemyDamage(&driver, driver.generation,
			plan, result, e.timestamp, false, false, true)
		damageErr := err
		if !isApplied {
			// Chunk 621 notifies independently of TakeDamage's boolean result.
		}
		packets = append(packets, hitEffect)
		driver.queueCampaignPackets(packets)
		driver.queueStatDelta(statDelta)
		registry.sessions[sessionKey] = driver
		registry.queueCompanionDeathPeersLocked(&driver, gameplayPeerPresentationPackets(packets))
		if damageErr != nil {
			return fmt.Errorf("drainerDamage: %w", damageErr)
		}
		return nil
	}
	return nil
}

func (e *drainerDeathRun) finishLocked() {
	if e.key.world.NPCs().FinishDrainerDeath(e.source) && !e.runtime.registry.isClosed {
		err := e.key.world.RecordNPCDeathObjectives(e.key.world.Context(), []zonenpc.DeathEvent{{Kind: zonenpc.DeathDelete, TargetObjectID: e.key.objectID}})
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Drainer deletion state object=%d: %v", e.key.objectID, err)
		}
		e.runtime.registry.queueCompanionDeathWorldLocked(e.key.world, [][]byte{e.deletePacket})
	}
}

func (e *gameplaySessionRegistry) retireDrainerDeathsLocked(gameID uint32) {
	for key, run := range e.drainerDeaths {
		if gameID != 0 && run.gameID != gameID {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		delete(e.drainerDeaths, key)
		run.finishLocked()
	}
}

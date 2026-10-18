package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignArcturusMissile) pollTracked() ([][]byte, error) {
	run := e.launch
	run.runtime.registry.mutex.Lock()
	current, isFound := run.runtime.registry.sessions[run.sessionKey]
	if e.retirement != nil && (e.retirement.isRetired || e.retirement.isRetirementPending) {
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isCurrent := isFound && current.generation == run.generation && current.zone == e.zone &&
		current.campaignNPCProjectiles[e.projectileObjectID] == e.projectile
	isStarted := isCurrent && run.runtime.registry.beginCampaignProjectilePublicationLocked(e.retirement)
	run.runtime.registry.mutex.Unlock()
	if !isStarted {
		return nil, nil
	}
	packets, err := e.poll()
	run.runtime.registry.mutex.Lock()
	retireErr := run.runtime.registry.finishCampaignProjectilePublicationLocked(e.retirement, run.sessionKey, packets, err)
	run.runtime.registry.mutex.Unlock()
	if retireErr != nil && run.runtime.logger != nil {
		run.runtime.logger.Printf("RakNet Arcturus missile cleanup failed projectile=%d: %v", e.projectileObjectID, retireErr)
	}
	if err != nil {
		return nil, fmt.Errorf("missilePollProduce: %w", err)
	}
	return nil, nil
}

// Poll the shared motion owner: freeze, gravity, slowing and destruction must
// change impact authority as well as the rendered missile.
func (e campaignArcturusMissile) poll() ([][]byte, error) {
	run := e.launch
	run.runtime.registry.mutex.Lock()
	current, isFound := run.runtime.registry.sessions[run.sessionKey]
	if e.retirement != nil && (e.retirement.isRetired || e.retirement.isRetirementPending) {
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if !isFound || current.generation != run.generation || current.zone != e.zone {
		run.runtime.registry.mutex.Unlock()
		e.projectile.Stop()
		return nil, nil
	}
	now := run.runtime.now()
	snapshot := e.projectile.Snapshot(now)
	if current.isZoneTerminal() || current.campaignNPCProjectiles[e.projectileObjectID] != e.projectile || !snapshot.IsActive {
		current.untrackCampaignNPCProjectile(e.projectileObjectID, e.projectile)
		run.runtime.registry.sessions[run.sessionKey] = current
		run.runtime.registry.mutex.Unlock()
		e.projectile.Stop()
		return e.cleanupPackets()
	}
	position := game.Vec3(snapshot.Position)
	ground := position
	ground.Z = e.position.Z
	projected, isProjected, err := zonenavigation.ProjectPosition(current.zone.Navigation(), ground, 0.5)
	if err == nil && isProjected {
		ground = projected
	}
	isImpact := !snapshot.IsFrozen && (position.Z <= ground.Z || snapshot.RemainingDistance <= 0)
	run.runtime.registry.mutex.Unlock()
	if err != nil {
		return e.retire(fmt.Errorf("missileGround: %w", err))
	}
	if current.zone.Navigation() != nil && !isProjected {
		return e.retire(fmt.Errorf("missileGround: no safe impact surface"))
	}
	if isImpact {
		e.position = ground
		e.direction = snapshot.Direction
		e.flightDelay = now.Sub(e.startedAt)
		e.timestamp += uint64(e.flightDelay / time.Millisecond)
		return e.impact()
	}
	shadowPacket, err := npcraknet.RestorePose(e.shadowObjectID, ground, game.Vec3{Y: 1})
	if err != nil {
		return e.retire(fmt.Errorf("missileShadowMove: %w", err))
	}
	err = scheduleNPCProducer(run.runtime.registry, run.packet, 50*time.Millisecond, e.pollTracked)
	if err != nil {
		return e.retire(fmt.Errorf("missilePoll: %w", err))
	}
	return [][]byte{shadowPacket}, nil
}

func (e campaignArcturusMissile) retire(cause error) ([][]byte, error) {
	run := e.launch
	run.runtime.registry.mutex.Lock()
	err := run.runtime.registry.retireCampaignProjectileLocked(e.retirement)
	run.runtime.registry.mutex.Unlock()
	if run.runtime.logger != nil {
		run.runtime.logger.Printf("Arcturus missile retired projectile=%d: %v", e.projectileObjectID, cause)
		if err != nil {
			run.runtime.logger.Printf("Arcturus missile retirement failed projectile=%d: %v", e.projectileObjectID, err)
		}
	}
	return nil, nil
}

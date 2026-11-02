package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type graviticProjectile struct {
	run        *abilityraknet.ProjectileRun
	burst      *abilityraknet.BurstRun
	lob        *graviticLob
	retirement *campaignProjectileRetirement
	objectID   uint32
	instanceID uint32
}

func (e graviticProjectile) setSpeed(now time.Time, scale float32) bool {
	if e.lob != nil {
		return e.lob.setSpeed(now, scale)
	}
	if e.burst != nil {
		return e.burst.SetSpeedScale(e.objectID, now, scale)
	}
	return e.run.SetSpeedScale(now, scale)
}

type graviticField struct {
	objectID uint32
	position game.Vec3
	radius   float32
}

func graviticFieldSource(fields []graviticField, position game.Vec3) uint32 {
	for _, field := range fields {
		if field.position.Sub(position).Length() < field.radius {
			return field.objectID
		}
	}
	return 0
}

func (e gameplayPendingRuntime) updateGraviticProjectilesLocked(
	member *gameplayPeerSession, fields []graviticField, now time.Time,
) ([][]byte, error) {
	if len(fields) == 0 && len(member.graviticProjectiles) == 0 {
		return nil, nil
	}
	runs := make([]*abilityraknet.ProjectileRun, 0, len(member.sageAttacks)+len(member.heroProjectileRuns)+2)
	for _, run := range member.sageAttacks {
		runs = append(runs, run)
	}
	for _, run := range member.heroProjectileRuns {
		if run != nil {
			runs = append(runs, run.projectile)
		}
	}
	for _, attack := range member.fieldMedicDroneAttacks {
		runs = append(runs, attack)
	}
	if member.fireTempestActive != nil {
		runs = append(runs, member.fireTempestActive.attack)
	}
	projectiles := make(map[uint32]graviticProjectile)
	lobs := make(map[uint32]*graviticLob, len(member.tossAttacks)+len(member.cloudLobAttacks))
	for objectID, run := range member.tossAttacks {
		lobs[objectID] = run.lob
	}
	for objectID, run := range member.cloudLobAttacks {
		lobs[objectID] = run.lob
	}
	for objectID, lob := range lobs {
		position, isActive := lob.position(now)
		sourceID := graviticFieldSource(fields, position)
		if isActive && sourceID != 0 {
			projectiles[objectID] = graviticProjectile{lob: lob, objectID: objectID}
		}
	}
	for _, run := range runs {
		snapshot := run.Snapshot(now)
		retirement := e.registry.projectileRetirements[run]
		if (member.sageAttacks[snapshot.ObjectID] == run && retirement == nil) ||
			(retirement != nil && (!retirement.isAdmitted || !retirement.isPublished ||
				retirement.isRetired || retirement.isRetirementPending)) {
			// Advance can expose an active simulator before its launch producer
			// publishes ObjectCreate. The retained wire owner admits contact.
			continue
		}
		sourceID := graviticFieldSource(fields, game.Vec3(snapshot.Position))
		if snapshot.IsActive && sourceID != 0 {
			projectiles[snapshot.ObjectID] = graviticProjectile{run: run, objectID: snapshot.ObjectID, retirement: retirement}
		}
	}
	for _, run := range member.heroBurstAttacks {
		for _, snapshot := range run.Snapshots(now) {
			sourceID := graviticFieldSource(fields, game.Vec3(snapshot.Position))
			if snapshot.IsActive && sourceID != 0 {
				projectiles[snapshot.ObjectID] = graviticProjectile{burst: run, objectID: snapshot.ObjectID}
			}
		}
	}
	if member.graviticProjectiles == nil {
		member.graviticProjectiles = make(map[uint32]graviticProjectile)
	}
	packets := make([][]byte, 0)
	for objectID, tracked := range member.graviticProjectiles {
		current, isInside := projectiles[objectID]
		if isInside && tracked.run == current.run && tracked.burst == current.burst && tracked.lob == current.lob {
			continue
		}
		tracked.setSpeed(now, 1)
		err := e.modifierPool.Release(tracked.instanceID)
		if err != nil {
			return nil, fmt.Errorf("graviticModifierRelease: %w", err)
		}
		delete(member.graviticProjectiles, objectID)
		packets, err = e.publishGraviticProjectileLocked(member, tracked,
			raknet.ModifierDeletedMessage{TargetID: objectID, InstanceID: tracked.instanceID}, packets)
		if err != nil {
			return nil, fmt.Errorf("graviticDelete: %w", err)
		}
	}
	for objectID, projectile := range projectiles {
		if _, isTracked := member.graviticProjectiles[objectID]; isTracked {
			continue
		}
		instanceID, err := e.modifierPool.Allocate()
		if err != nil {
			return nil, fmt.Errorf("graviticModifierAllocate: %w", err)
		}
		if !projectile.setSpeed(now, 1+zonenpc.GraviticMovementSpeedBuff) {
			err = e.modifierPool.Release(instanceID)
			if err != nil {
				return nil, fmt.Errorf("graviticExpiredRelease: %w", err)
			}
			continue
		}
		projectile.instanceID = instanceID
		member.graviticProjectiles[objectID] = projectile
		packets, err = e.publishGraviticProjectileLocked(member, projectile, raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: util.HashID("ZelemSlow"),
			// Build 103's legacy SourceID slot is an NPC affix asset, not the
			// fixture's object ID. Ordinary field slows have no affix association.
			InstanceID: instanceID, StackCount: 1,
			StartMilliseconds: 1,
		}, packets)
		if err != nil {
			return nil, fmt.Errorf("graviticCreate: %w", err)
		}
	}
	return packets, nil
}

// Projectile contact uses the launch owner's FIFO, never the polling response.
// A response can otherwise overtake an already queued ObjectCreate on its caster.
func (e gameplayPendingRuntime) publishGraviticProjectileLocked(
	member *gameplayPeerSession, projectile graviticProjectile,
	message raknet.ApplicationMessage, packets [][]byte,
) ([][]byte, error) {
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return packets, fmt.Errorf("graviticMarshal: %w", err)
	}
	retirement := projectile.retirement
	if retirement == nil {
		return append(packets, packet), nil
	}
	if !retirement.isAdmitted || !retirement.isPublished || retirement.isRetired || retirement.isRetirementPending {
		return packets, nil
	}
	owner, isFound := e.registry.sessions[retirement.sessionKey]
	if !isFound || owner.generation != retirement.generation ||
		owner.binding.UserID != member.binding.UserID || !retirement.isMemberRealm(*member) {
		return packets, nil
	}
	// Persist this poll's modifier membership, then retain the publication's
	// updated queues in the working session written back by the caller.
	e.registry.sessions[retirement.sessionKey] = *member
	e.registry.queueCampaignProjectilePublicationLocked(retirement, retirement.sessionKey, [][]byte{packet})
	*member = e.registry.sessions[retirement.sessionKey]
	return packets, nil
}

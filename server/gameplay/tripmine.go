package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func isTripMine(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_prefab_tnx_object_tripmine.Noun")
}

// Chunk112 captures the source once. The shared pointer owns one outer loop,
// regardless of which party member happens to poll the world this frame.
type tripMineRun struct {
	source      zonenpc.Snapshot
	damage      game.DamageRange
	nextAt      time.Time
	targets     []zone.NPCTarget
	targetIndex int
	shotIndex   int
	isWarning   bool
	isFiring    bool
	isStopped   bool
}

func (e *tripMineRun) rest(now time.Time, member *gameplayPeerSession) error {
	seconds, err := sim.ScriptRandom(member.zone.NPCRandom(), 3, 7)
	if err != nil {
		return fmt.Errorf("mineRestRandom: %w", err)
	}
	e.nextAt = now.Add(time.Duration(seconds * float64(time.Second)))
	e.isWarning = false
	e.isFiring = false
	e.targets = nil
	e.targetIndex = 0
	e.shotIndex = 0
	return nil
}

func withTripMineDeath(snapshot zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	if !snapshot.Plan.IsFixture || !isTripMine(snapshot.Plan.NounName) {
		return definition
	}
	// ScaldronColumnDeath348 has chainingDelay=0, no damage, no deletion,
	// and no RemovePhysics. The retained remnant owns its dead graphics.
	definition.isRemnantRetained = true
	definition.isCollisionRetained = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.deleteDelay = 0
	definition.graphicsState = util.HashID("dead")
	definition.explosionEffectName = "scaldron_instrument_destruction_effect.ServerEventDef"
	return definition
}

// The caller holds registry.mutex. Request callbacks run only after this
// response commits, outside that lock, and retain the actual fixture identity.
func (e gameplayPendingRuntime) pollTripMinesLocked(member *gameplayPeerSession, req raknet.Packet, now time.Time) ([][]byte, error) {
	if member.tripMines == nil {
		member.tripMines = make(map[uint32]*tripMineRun)
	}
	packets := make([][]byte, 0)
	for _, source := range member.zone.NPCs().Snapshots() {
		if !source.Plan.IsFixture || !isTripMine(source.Plan.NounName) || !source.IsPublished {
			continue
		}
		objectID := source.Plan.ObjectID
		run := member.tripMines[objectID]
		if run == nil && (source.IsDefeated || source.HitPoint <= 0) {
			continue
		}
		if run == nil {
			for _, peer := range e.registry.sessions {
				if peer.zone == member.zone && peer.tripMines[objectID] != nil {
					run = peer.tripMines[objectID]
					break
				}
			}
			if run == nil {
				run = &tripMineRun{source: source}
				profile, err := tripMineMissileProfile(uint32(source.Plan.NPCProfile.NPCRank))
				if err != nil {
					return nil, fmt.Errorf("mineDamageProfile: %w", err)
				}
				plan, err := zonenpc.PlanRetainedAreaAttackWithProfile(source, source.Plan.ObjectID, source.Plan.Position, profile)
				if err != nil {
					return nil, fmt.Errorf("mineSnapshot: %w", err)
				}
				run.damage = plan.Damage
				err = run.rest(now, member)
				if err != nil {
					return nil, fmt.Errorf("mineStart: %w", err)
				}
			}
			member.tripMines[objectID] = run
		}
		if source.IsDefeated || source.HitPoint <= 0 {
			run.isStopped = true
		}
		if run.isStopped || now.Before(run.nextAt) {
			continue
		}
		if !run.isWarning {
			asset := util.HashID("effect_environment_Scaldron_tem_column.ServerEventDef")
			warning, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{Asset: &asset, ObjectID: &objectID})
			if err != nil {
				return nil, fmt.Errorf("mineWarning: %w", err)
			}
			packets = append(packets, warning)
			run.isWarning = true
			run.nextAt = now.Add(3 * time.Second)
			continue
		}
		if !run.isFiring {
			targets, err := e.registry.sampleNPCTargetsLocked(member.zone, member.zone.LiveNPCTargets(), now)
			if err != nil {
				return nil, fmt.Errorf("mineTargets: %w", err)
			}
			for _, target := range targets {
				if target.HitPoint > 0 && target.ObjectID != objectID &&
					target.Position.Sub(run.source.Plan.Position).Length() <= 30 {
					run.targets = append(run.targets, target)
				}
			}
			run.isFiring = true
		}
		// There is no wait after shot three. The next recipient's first shot
		// is requested in the same frame, exactly as the serial Lua loop.
		for run.targetIndex < len(run.targets) && !now.Before(run.nextAt) {
			target := run.targets[run.targetIndex]
			launch := tripMineLaunch{runtime: e.damage.npc, packet: req.Autonomous(),
				sessionKey: req.Address.String(), generation: member.generation,
				source: run.source, target: target, damage: run.damage, timestamp: req.SourceTime}
			launch.runtime.registry = e.registry
			err := req.AfterResponseCommit(launch.commit)
			if err != nil {
				return nil, fmt.Errorf("mineRequest: %w", err)
			}
			gfx, err := raknet.MarshalApplication(raknet.SetObjectGFXStateMessage{
				ObjectID: objectID, State: util.HashID("fired"), Timestamp: req.SourceTime,
			})
			if err != nil {
				return nil, fmt.Errorf("mineFired: %w", err)
			}
			packets = append(packets, gfx)
			run.shotIndex++
			if run.shotIndex < 3 {
				run.nextAt = now.Add(550 * time.Millisecond)
				break
			}
			run.shotIndex = 0
			run.targetIndex++
		}
		if run.isFiring && run.targetIndex == len(run.targets) {
			err := run.rest(now, member)
			if err != nil {
				return nil, fmt.Errorf("mineRepeat: %w", err)
			}
		}
	}
	if len(packets) == 0 {
		return nil, nil
	}
	publication := campaignPeerPublication{registry: e.registry,
		identity: gameplayProducerIdentityFromSession(req.Address.String(), *member, true),
		packets:  clonePendingPackets(packets)}
	err := req.AfterResponseCommit(publication.commit)
	if err != nil {
		return nil, fmt.Errorf("minePublish: %w", err)
	}
	return packets, nil
}

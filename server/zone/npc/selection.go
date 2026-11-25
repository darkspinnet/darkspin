package npc

import (
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// SnapshotSelection identifies a narrow polling consumer. Filtering happens
// under the NPC lock before copying the large combat records into an output
// array. Results remain detached, ordered and current; no cache is retained.
type SnapshotSelection uint8

const (
	SelectFireVent SnapshotSelection = iota + 1
	SelectCryosGeyser
	SelectCryosMalfunction
	SelectCryosFungus
	SelectWarpTotem
	SelectTripMine
	SelectSilenceField
	SelectIdleAction
	SelectScenarioFixture
	SelectNativeSilence
	SelectPlayerAligned
)

func (e *Session) SelectedSnapshots(selection SnapshotSelection) []Snapshot {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshAttributeMirrorsLocked()
	var snapshots []Snapshot
	for _, objectID := range e.objectIDs {
		npc := e.npcs[objectID]
		if !matchesSnapshotSelection(&npc, selection) {
			continue
		}
		snapshots = append(snapshots, npc)
	}
	return snapshots
}

func matchesSnapshotSelection(npc *Snapshot, selection SnapshotSelection) bool {
	switch selection {
	case SelectFireVent:
		return game.SceneryHazardAbility(npc.Plan.NounName) == "VentFlameCone"
	case SelectCryosGeyser:
		return game.CryosGeyserHook(npc.Plan.NounName, npc.Plan.NPCProfile) != ""
	case SelectCryosMalfunction:
		return IsCryosMalfunction(npc.Plan)
	case SelectCryosFungus:
		return IsCryosFungus(npc.Plan)
	case SelectWarpTotem:
		return IsWarpTotem(npc.Plan)
	case SelectTripMine:
		return npc.Plan.IsFixture && strings.EqualFold(npc.Plan.NounName, "DEST_prefab_tnx_object_tripmine.Noun")
	case SelectSilenceField:
		return npc.Plan.IsFixture && strings.EqualFold(npc.Plan.NounName, "DEST_prefab_tnx_object_silenceField.Noun")
	case SelectIdleAction:
		return !npc.IsDefeated && npc.IsScenarioAIEnabled() && !npc.Plan.IsFixture &&
			npc.TargetObjectID != 0 && !npc.IsActionStarted && !IsAgentHealingChild(npc.Plan) &&
			!IsArcturusTurret(npc.Plan) &&
			!time.Now().Before(npc.actionRetryAt)
	case SelectScenarioFixture:
		return npc.Plan.IsFixture && npc.IsScenarioControlled()
	case SelectNativeSilence:
		return !npc.IsDefeated && !npc.IsBirthPending && NativePassiveAura(npc.Plan) == "CitadelSpecialTwo_SilenceAura"
	case SelectPlayerAligned:
		return !npc.IsDefeated && !npc.IsBirthPending && npc.Faction == FactionPlayerAligned
	default:
		return false
	}
}

// ActorSnapshot contains only the live geometry and target state needed by
// contacts and combat-presence polling, without profiles, affixes or timelines.
type ActorSnapshot struct {
	ObjectID                        uint32
	Position                        game.Vec3
	FootprintRadius                 float32
	HitPoint                        float32
	Faction                         Faction
	TargetObjectID                  uint32
	IsPublished                     bool
	IsFixture                       bool
	IsInvisibleToSecurityTeleporter bool
}

// LiveActorSnapshots preserves LiveSnapshots' defeat/birth filtering. In
// particular, unpublished actors and zero-health actors retain their semantics
// at the consuming feature, rather than being silently filtered here.
func (e *Session) LiveActorSnapshots() []ActorSnapshot {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshAttributeMirrorsLocked()
	actors := make([]ActorSnapshot, 0, len(e.objectIDs))
	for _, objectID := range e.objectIDs {
		npc := e.npcs[objectID]
		if npc.IsDefeated || npc.IsBirthPending {
			continue
		}
		actors = append(actors, ActorSnapshot{
			ObjectID: objectID, Position: npc.Plan.Position, FootprintRadius: npc.Plan.NPCProfile.FootprintRadius,
			HitPoint: npc.HitPoint, Faction: npc.Faction, TargetObjectID: npc.TargetObjectID,
			IsPublished: npc.IsPublished, IsFixture: npc.Plan.IsFixture,
			IsInvisibleToSecurityTeleporter: npc.IsInvisibleToSecurityTeleporter,
		})
	}
	return actors
}

// TargetSnapshot is the detached hostile-candidate view used by player AI.
// Pending births keep the existing publication-based targeting semantics.
type TargetSnapshot struct {
	ObjectID       uint32
	Position       game.Vec3
	Radius         float32
	TargetObjectID uint32
	HealthRatio    float32
	IsDestructible bool
	IsPriority     bool
}

func (e *Session) TargetSnapshots() []TargetSnapshot {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshAttributeMirrorsLocked()
	var targets []TargetSnapshot
	for _, objectID := range e.objectIDs {
		npc := e.npcs[objectID]
		if !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 ||
			npc.Faction != FactionNonPlayerAligned || npc.IsSpawnStealthActive || !npc.Plan.NPCProfile.IsTargetable {
			continue
		}
		targets = append(targets, TargetSnapshot{ObjectID: npc.Plan.ObjectID, Position: npc.Plan.Position,
			Radius: npc.Plan.ActorFootprintRadius(), TargetObjectID: npc.TargetObjectID,
			HealthRatio:    npc.HitPoint / max(float32(1), npc.Plan.NPCProfile.HitPoint),
			IsDestructible: npc.Plan.IsFixture || npc.Plan.NPCProfile.NounType == game.NounTypeDestructibleOrnament,
			IsPriority:     npc.Plan.IsElite || npc.Plan.IsCaptain || npc.Plan.IsBoss,
		})
	}
	return targets
}

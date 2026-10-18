package npc

import (
	"errors"
	"fmt"
	"math"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// AllyAlertRules is a server reconstruction, not a recovered native scan.
// Threat arithmetic and AlertObject's mode gate are intentionally not tunable.
type AllyAlertRules struct {
	IsEnabled                  bool
	RangePercent               uint32
	RangeOwner                 string
	MaxHops                    uint32
	IsDiagnosticLoggingEnabled bool
}

func DefaultAllyAlertRules() AllyAlertRules {
	return AllyAlertRules{
		IsEnabled: true, RangePercent: 100, RangeOwner: "recipient", MaxHops: 1,
	}
}

func (e AllyAlertRules) Validate() error {
	if e.RangePercent == 0 || e.RangePercent > 1000 {
		return errors.New("ally alert range_percent must be between 1 and 1000")
	}
	if e.RangeOwner != "source" && e.RangeOwner != "recipient" {
		return errors.New("ally alert range_owner must be source or recipient")
	}
	if e.MaxHops == 0 || e.MaxHops > 16 {
		return errors.New("ally alert max_hops must be between 1 and 16")
	}
	return nil
}

type AllyAlert struct {
	SourceObjectID uint32
	Recipient      Snapshot
	Hop            uint32
	Distance       float64
	Range          float64
}

type AllyAlertRequest struct {
	SourceObjectIDs []uint32
	Targets         []Target
	Rules           AllyAlertRules
}

type allyAlertSource struct {
	NPC    Snapshot
	Target Target
	Hop    uint32
}

// AlertAllies admits each idle recipient once, in breadth-first registration
// order. It never publishes staged actors or acquires encounter bosses.
func (e *Session) AlertAllies(req AllyAlertRequest) ([]AllyAlert, error) {
	if e == nil {
		return nil, errors.New("nil npc session")
	}
	err := req.Rules.Validate()
	if err != nil {
		return nil, fmt.Errorf("alertRules: %w", err)
	}
	if !req.Rules.IsEnabled || len(req.SourceObjectIDs) == 0 {
		return nil, nil
	}
	targetsByID := make(map[uint32]Target, len(req.Targets))
	for _, target := range req.Targets {
		if target.ObjectID == 0 || !zonegeometry.IsFinite(target.Position) ||
			target.Faction == FactionUnknown {
			return nil, errors.New("ally alert target invalid")
		}
		if target.IsAlive {
			targetsByID[target.ObjectID] = target
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	sources := make([]allyAlertSource, 0, len(req.SourceObjectIDs))
	seenObjectIDs := make(map[uint32]struct{}, len(req.SourceObjectIDs))
	for _, objectID := range req.SourceObjectIDs {
		if _, isSeen := seenObjectIDs[objectID]; isSeen {
			continue
		}
		seenObjectIDs[objectID] = struct{}{}
		npc, isFound := e.npcs[objectID]
		target, isTargetFound := targetsByID[npc.TargetObjectID]
		if !isFound || !isTargetFound || !isAllyAlertActor(npc) ||
			npc.IsActionStarted || npc.Plan.NPCProfile.AggroType == 2 ||
			target.Faction == npc.Faction {
			continue
		}
		sources = append(sources, allyAlertSource{NPC: npc, Target: target})
	}
	alerts := make([]AllyAlert, 0)
	for index := 0; index < len(sources); index++ {
		source := sources[index]
		if source.Hop >= req.Rules.MaxHops {
			continue
		}
		for _, objectID := range e.objectIDs {
			recipient := e.npcs[objectID]
			if !isAllyAlertActor(recipient) || recipient.Plan.IsBoss ||
				recipient.TargetObjectID != 0 || recipient.IsActionStarted ||
				recipient.Faction != source.NPC.Faction ||
				recipient.Plan.NPCProfile.AggroType != 0 {
				continue
			}
			profile := recipient.Plan.NPCProfile
			if req.Rules.RangeOwner == "source" {
				profile = source.NPC.Plan.NPCProfile
			}
			radius := float64(profile.AlertRange) * float64(req.Rules.RangePercent) / 100
			if !profile.IsClassKnown || radius <= 0 || math.IsNaN(radius) || math.IsInf(radius, 0) {
				continue
			}
			deltaX := float64(recipient.Plan.Position.X) - float64(source.NPC.Plan.Position.X)
			deltaY := float64(recipient.Plan.Position.Y) - float64(source.NPC.Plan.Position.Y)
			deltaZ := float64(recipient.Plan.Position.Z) - float64(source.NPC.Plan.Position.Z)
			distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
			if distanceSquared >= radius*radius {
				continue
			}
			recipient, isAdded, alertErr := e.alertObjectLocked(objectID, source.Target.ObjectID)
			if alertErr != nil {
				return alerts, fmt.Errorf("allyAlert: %w", alertErr)
			}
			if !isAdded {
				continue
			}
			recipient.TargetFaction = source.Target.Faction
			recipient.TargetOwner = source.Target.Owner
			e.npcs[objectID] = recipient
			hop := source.Hop + 1
			alerts = append(alerts, AllyAlert{
				SourceObjectID: source.NPC.Plan.ObjectID, Recipient: recipient,
				Hop: hop, Distance: math.Sqrt(distanceSquared), Range: radius,
			})
			sources = append(sources, allyAlertSource{NPC: recipient, Target: source.Target, Hop: hop})
		}
	}
	return alerts, nil
}

func isAllyAlertActor(npc Snapshot) bool {
	return npc.IsPublished && !npc.IsDefeated && npc.HitPoint > 0 &&
		!npc.Plan.IsFixture && npc.Faction == FactionNonPlayerAligned &&
		!npc.Plan.NPCProfile.IsPlayerPet && zonegeometry.IsFinite(npc.Plan.Position)
}

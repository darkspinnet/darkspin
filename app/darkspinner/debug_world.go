//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugWorldPosition struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}
type debugWorldHero struct {
	ObjectID         uint32             `json:"object_id"`
	Index            uint32             `json:"index"`
	Position         debugWorldPosition `json:"position"`
	HitPoint         float32            `json:"hit_point"`
	ManaPoint        float32            `json:"mana_point"`
	MaximumHitPoint  float32            `json:"maximum_hit_point"`
	MaximumManaPoint float32            `json:"maximum_mana_point"`
	IsDefeated       bool               `json:"is_defeated"`
}
type debugWorldNPC struct {
	ObjectID             uint32             `json:"object_id"`
	Noun                 string             `json:"noun"`
	Position             debugWorldPosition `json:"position"`
	HitPoint             float32            `json:"hit_point"`
	IsDead               bool               `json:"is_dead"`
	IsPublished          bool               `json:"is_published"`
	IsHostile            bool               `json:"is_hostile"`
	IsHostilityAvailable bool               `json:"is_hostility_available"`
	IsFixture            bool               `json:"is_fixture"`
	IsBoss               bool               `json:"is_boss"`
	DistanceSquared      float64            `json:"distance_squared"`
}
type debugWorldTeleporter struct {
	RouteIndex            int                `json:"route_index"`
	MarkerID              uint32             `json:"marker_id"`
	DestinationMarkerID   uint32             `json:"destination_marker_id"`
	ObjectID              uint32             `json:"object_id"`
	Source                debugWorldPosition `json:"source"`
	Destination           debugWorldPosition `json:"destination"`
	IsSecurity            bool               `json:"is_security"`
	IsBoss                bool               `json:"is_boss"`
	IsTunnel              bool               `json:"is_tunnel"`
	IsActivationAvailable bool               `json:"is_activation_available"`
	IsActive              bool               `json:"is_active"`
}
type debugWorldBoss struct {
	Phase              uint8   `json:"phase"`
	LeaderObjectID     uint32  `json:"leader_object_id"`
	LeaderHitPoint     float32 `json:"leader_hit_point"`
	IsComplete         bool    `json:"is_complete"`
	IsBeamOutCommitted bool    `json:"is_beam_out_committed"`
}
type debugWorldState struct {
	NativeReadiness         *controlGameplayState  `json:"native_readiness,omitempty"`
	Ability                 *debugAbilityState     `json:"ability"`
	ClientID                string                 `json:"client_id"`
	InstanceID              string                 `json:"instance_id"`
	PID                     uint32                 `json:"pid"`
	UserID                  int64                  `json:"user_id"`
	GameID                  uint32                 `json:"game_id"`
	Source                  string                 `json:"source"`
	CapturedAt              time.Time              `json:"captured_at"`
	IsAvailable             bool                   `json:"is_available"`
	UnavailableReason       string                 `json:"unavailable_reason,omitempty"`
	PeerGeneration          uint64                 `json:"peer_generation"`
	TransportGeneration     uint64                 `json:"transport_generation"`
	ZoneGeneration          uint64                 `json:"zone_generation"`
	SessionStage            string                 `json:"session_stage"`
	Hero                    *debugWorldHero        `json:"hero"`
	Arena                   *debugWorldArena       `json:"arena,omitempty"`
	NPCs                    []debugWorldNPC        `json:"npcs"`
	NPCTotal                int                    `json:"npc_total"`
	IsNPCTruncated          bool                   `json:"is_npc_truncated"`
	Pickups                 []debugWorldPickup     `json:"pickups"`
	PickupTotal             int                    `json:"pickup_total"`
	IsPickupTruncated       bool                   `json:"is_pickup_truncated"`
	IsPickupAvailable       bool                   `json:"is_pickup_available"`
	PickupUnavailableReason string                 `json:"pickup_unavailable_reason,omitempty"`
	Teleporters             []debugWorldTeleporter `json:"teleporters"`
	IsTeleporterTruncated   bool                   `json:"is_teleporter_truncated"`
	IsMissionComplete       bool                   `json:"is_mission_complete"`
	IsZoneTerminal          bool                   `json:"is_zone_terminal"`
	IsVictoryAvailable      bool                   `json:"is_victory_available"`
	IsVictoryCommitted      bool                   `json:"is_victory_committed"`
	IsObjectiveComplete     bool                   `json:"is_objective_complete"`
	Boss                    *debugWorldBoss        `json:"boss"`
}

func debugWorldPositionDTO(position scenario.WorldPosition) debugWorldPosition {
	return debugWorldPosition{X: position.X, Y: position.Y, Z: position.Z}
}

func observeDebugWorld(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugWorldState, error) {
	result := debugWorldState{Source: "server_world_snapshot", NPCs: []debugWorldNPC{}, Teleporters: []debugWorldTeleporter{}, Pickups: []debugWorldPickup{}}
	if ctx == nil || host == nil || clientManager == nil {
		return result, errors.New("world observation requires an owned server/client")
	}
	var req struct {
		ClientID string `json:"client_id"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("worldRequest: %w", err)
	}
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return result, fmt.Errorf("worldClient: %w", err)
	}
	result.ClientID = client.ClientID
	result.InstanceID = client.InstanceID
	result.PID = client.ProcessID
	result.UserID = client.UserID
	if !client.IsRunning || client.Status == "liveness_unavailable" || client.ProfileName == "" {
		return result, errors.New("world observation requires a live owned client/profile")
	}
	profile, err := host.ProfileState(ctx, client.ProfileName)
	if err != nil {
		return result, fmt.Errorf("worldProfile: %w", err)
	}
	if client.UserID <= 0 || profile.UserID != client.UserID {
		return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "worldAccount", Message: "owned profile alias no longer matches this client's launch account"}
	}
	result.UserID = profile.UserID
	result.GameID = profile.GameID
	if !profile.IsAuthenticated || !profile.IsGameplayActive || profile.GameID == 0 || profile.GameplayGameID != profile.GameID {
		result.UnavailableReason = "owned profile has no active admitted gameplay"
		return result, nil
	}
	world, err := host.server.DebugWorldState(ctx, profile.UserID, profile.GameID)
	if err != nil {
		return result, fmt.Errorf("worldSnapshot: %w", err)
	}
	current, err := clientManager.State(req.ClientID)
	if err != nil {
		return result, fmt.Errorf("worldRecheck: %w", err)
	}
	after, err := host.ProfileState(ctx, client.ProfileName)
	if err != nil {
		return result, fmt.Errorf("worldLogin: %w", err)
	}
	if !current.IsRunning || current.Status == "liveness_unavailable" || current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || current.ProfileName != client.ProfileName || current.UserID != client.UserID || !after.IsAuthenticated || after.SessionID != profile.SessionID || after.UserID != profile.UserID || after.GameID != profile.GameID {
		return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "worldIdentity", Message: "owned process, account or game changed during world observation"}
	}
	result.CapturedAt = world.CapturedAt
	result.IsAvailable = world.IsAvailable
	result.UnavailableReason = world.UnavailableReason
	result.PeerGeneration = world.PeerGeneration
	result.TransportGeneration = world.TransportGeneration
	result.ZoneGeneration = world.ZoneGeneration
	result.SessionStage = world.SessionStage
	result.Arena = debugWorldArenaDTO(world.Arena)
	result.NPCTotal = world.NPCTotal
	result.IsNPCTruncated = world.IsNPCTruncated
	result.PickupTotal = world.PickupTotal
	result.IsPickupTruncated = world.IsPickupTruncated
	result.IsPickupAvailable = world.IsPickupAvailable
	result.PickupUnavailableReason = world.PickupUnavailableReason
	for _, pickup := range world.Pickups {
		result.Pickups = append(result.Pickups, debugWorldPickupDTO(pickup))
	}
	result.IsTeleporterTruncated = world.IsTeleporterTruncated
	result.IsMissionComplete = world.IsMissionComplete
	result.IsZoneTerminal = world.IsZoneTerminal
	result.IsVictoryAvailable = world.IsVictoryAvailable
	result.IsVictoryCommitted = world.IsVictoryCommitted
	result.IsObjectiveComplete = world.IsObjectiveComplete
	if world.Ability != nil {
		ability := debugAbilityStateDTO(*world.Ability)
		result.Ability = &ability
	}
	if world.Hero != nil {
		hero := world.Hero
		result.Hero = &debugWorldHero{ObjectID: hero.ObjectID, Index: hero.Index, Position: debugWorldPositionDTO(hero.Position), HitPoint: hero.HitPoint, ManaPoint: hero.ManaPoint, MaximumHitPoint: hero.MaximumHitPoint, MaximumManaPoint: hero.MaximumManaPoint, IsDefeated: hero.IsDefeated}
	}
	for _, npc := range world.NPCs {
		result.NPCs = append(result.NPCs, debugWorldNPC{ObjectID: npc.ObjectID, Noun: npc.Noun, Position: debugWorldPositionDTO(npc.Position), HitPoint: npc.HitPoint, IsDead: npc.IsDead, IsPublished: npc.IsPublished, IsHostile: npc.IsHostile, IsHostilityAvailable: npc.IsHostilityAvailable, IsFixture: npc.IsFixture, IsBoss: npc.IsBoss, DistanceSquared: npc.DistanceSquared})
	}
	for _, teleporter := range world.Teleporters {
		result.Teleporters = append(result.Teleporters, debugWorldTeleporter{RouteIndex: teleporter.RouteIndex, MarkerID: teleporter.MarkerID, DestinationMarkerID: teleporter.DestinationMarkerID, ObjectID: teleporter.ObjectID, Source: debugWorldPositionDTO(teleporter.Source), Destination: debugWorldPositionDTO(teleporter.Destination), IsSecurity: teleporter.IsSecurity, IsBoss: teleporter.IsBoss, IsTunnel: teleporter.IsTunnel, IsActivationAvailable: teleporter.IsActivationAvailable, IsActive: teleporter.IsActive})
	}
	if world.Boss != nil {
		boss := world.Boss
		result.Boss = &debugWorldBoss{Phase: boss.Phase, LeaderObjectID: boss.LeaderObjectID, LeaderHitPoint: boss.LeaderHitPoint, IsComplete: boss.IsComplete, IsBeamOutCommitted: boss.IsBeamOutCommitted}
	}
	return result, nil
}

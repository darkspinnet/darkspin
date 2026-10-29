package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const spoffitNoun = "Spoffit.Noun"
const spoffitCrystalResourceID = uint32(4260)
const spoffitCrystalModifier = "SpoffitCrystal"

func spoffitProfile(member gameplayPeerSession) (game.CampaignNPCProfile, error) {
	profile, isFound := member.zone.DirectorDefinition().NPCProfilesByNoun["spoffit.noun"]
	graph := profile.AIGraph
	if !isFound || !profile.IsKnown || !profile.IsClassKnown || !profile.AreClassAttributeBasesKnown || profile.NPCClassInstanceID != 3212084876 ||
		graph == nil || !graph.IsResolved || graph.DefinitionID != 3212084876 ||
		graph.CombatIdle != "nBehavior_FollowOwner" || graph.PassiveIdle != "nBehavior_FollowOwner" ||
		len(graph.Nodes) != 1 || graph.Nodes[0].Phase == nil || len(graph.Nodes[0].Phase.Gambits) != 1 {
		return game.CampaignNPCProfile{}, errors.New("Spoffit exact installed class/graph unavailable")
	}
	gambit := graph.Nodes[0].Phase.Gambits[0]
	if gambit.Ability == nil || *gambit.Ability != "FollowOwner" || gambit.Condition != nil ||
		profile.ClassAttributeBases[0] != 25 || profile.BaseCombatSpeed != 6 || profile.IdleMovementSpeed != 6 || profile.NPCType != 0 || profile.NPCRank != 1 {
		return game.CampaignNPCProfile{}, errors.New("Spoffit authored follower contract unavailable")
	}
	// CreateCreature copies no owner attributes. Director difficulty projection
	// is not a crystal summon multiplier; retain this exact class's own base HP.
	profile.HitPoint = profile.ClassAttributeBases[0]
	profile.BaseHitPoint = profile.HitPoint
	return profile, nil
}

// NavigationUtils245.GetRandomSpawnPosition uses four random angle/radius
// attempts, closest-spawn projection, and the returned annulus distance.
func (e *spoffitRun) spawnPosition(member gameplayPeerSession, now time.Time) (game.Vec3, bool, error) {
	ownerPosition, err := e.ownerPosition(member, now)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("spoffitOwnerPose: %w", err)
	}
	creature := member.binding.Creatures[member.deployedCreatureIndex]
	ownerRadius, err := e.runtime.program.NavigationRadiusByNoun(creature.Noun, 1)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("spoffitOwnerRadius: %w", err)
	}
	if ownerRadius <= 0 || e.world.Navigation() == nil {
		return game.Vec3{}, false, errors.New("Spoffit spawn navigation unavailable")
	}
	random := e.world.NPCRandom()
	if random == nil {
		return game.Vec3{}, false, errors.New("Spoffit spawn random owner unavailable")
	}
	for range 4 {
		angle := random.Float64() * 2 * math.Pi
		distance := ownerRadius + float32(random.Float64())*2
		candidate := ownerPosition.Add(game.Vec3{X: float32(math.Cos(angle)) * distance, Y: float32(math.Sin(angle)) * distance})
		position, isProjected, projectErr := zonenavigation.ProjectPosition(e.world.Navigation(), candidate, e.footprintRadius)
		if projectErr != nil {
			return game.Vec3{}, false, fmt.Errorf("spoffitSpawnProject: %w", projectErr)
		}
		if !isProjected {
			continue
		}
		projectedDistance := position.Sub(ownerPosition).Length()
		if projectedDistance >= ownerRadius && projectedDistance <= ownerRadius+2 {
			return position, true, nil
		}
	}
	return game.Vec3{}, false, nil
}

func (e *spoffitRun) spawnLocked(member gameplayPeerSession, now time.Time) ([][]byte, bool, error) {
	position, isPlaced, err := e.spawnPosition(member, now)
	if err != nil || !isPlaced {
		if err != nil {
			return nil, false, fmt.Errorf("spoffitPlacement: %w", err)
		}
		return nil, false, nil
	}
	objectID, err := e.world.ReserveObjectIDs(1)
	if err != nil {
		return nil, false, fmt.Errorf("spoffitObject: %w", err)
	}
	packets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID(spoffitNoun), OwnerID: e.ownerObjectID,
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, IsCollisionEnabled: true,
	})
	if err != nil {
		return nil, false, fmt.Errorf("spoffitBirth: %w", err)
	}
	messages := []raknet.ApplicationMessage{
		raknet.CombatantDataUpdateMessage{ObjectID: objectID, HitPoints: e.profile.HitPoint, ManaPoints: e.profile.PowerPoint},
		raknet.AttributeDataUpdateMessage{ObjectID: objectID, Value: map[uint8]float32{
			0: e.profile.Strength, 1: e.profile.Dexterity, 2: e.profile.Mind,
			4: e.profile.HitPoint, 5: e.profile.PowerPoint, 7: e.profile.DodgeRating,
			9: e.profile.ResistRating, 10: e.profile.CriticalRating,
			11: e.profile.IdleMovementSpeed, 12: e.profile.BaseCombatSpeed,
		}},
		raknet.AgentBlackboardUpdateMessage{ObjectID: objectID, IsTargetable: e.profile.IsTargetable},
		raknet.SetAnimationStateMessage{ObjectID: objectID, State: util.HashID(sageSpawnAnimation), Timestamp: advanceHeroArrivalTimestamp(e.sourceTime, e.startedAt, now), Scale: 1},
	}
	for index, message := range messages {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, false, fmt.Errorf("spoffitBirthMarshal[%d]: %w", index, marshalErr)
		}
		packets = append(packets, packet)
	}
	err = e.world.Companion().Put(zonecompanion.Actor{
		UserID: e.userID, PeerGeneration: e.generation, ObjectID: objectID,
		OwnerObjectID: e.ownerObjectID, Noun: util.HashID(spoffitNoun), Position: position,
		FootprintRadius: e.footprintRadius, ActorFootprint: e.profile.ActorFootprint,
		HitPoint: e.profile.HitPoint, MaximumHitPoint: e.profile.HitPoint,
		// The native noun has a combatant body, even though its authored kit
		// only follows. Receiving health/status rules must remain independent
		// of attack/pursuit admission.
		IsTargetable: e.profile.IsTargetable, IsCombatant: true,
		IsAttackDisabled: true, IsFollowManaged: true,
		MovementSpeed: e.profile.BaseCombatSpeed,
	})
	if err != nil {
		return nil, false, fmt.Errorf("spoffitActor: %w", err)
	}
	err = e.world.Companion().Recall(objectID, now, now.Add(sageSpawnDuration), sageSpawnAnimation)
	if err != nil {
		e.world.Companion().Remove(objectID)
		return nil, false, fmt.Errorf("spoffitSpawnHold: %w", err)
	}
	e.objectID = objectID
	e.spawnReleaseAt = now.Add(sageSpawnDuration)
	e.nextAbilityAt = e.spawnReleaseAt
	return packets, true, nil
}

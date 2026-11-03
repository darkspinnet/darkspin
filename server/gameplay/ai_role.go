package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

const campaignBotRoleInterval = 30 * time.Second

type campaignBotRoles struct {
	counts             map[string]int
	humanSentinelCount int
	humanRavagerCount  int
}

// Caller owns the registry lock. Count active heroes across this shared world,
// excluding this bot; reserve heroes do not represent the party's active roles.
func (e *gameplaySessionRegistry) campaignBotRolesLocked(member gameplayPeerSession) campaignBotRoles {
	roles := campaignBotRoles{counts: make(map[string]int)}
	for _, ally := range e.sessions {
		if ally.binding.GameID != member.binding.GameID || ally.zone != member.zone ||
			ally.binding.UserID == member.binding.UserID || ally.binding.Mode != game.ModeChain ||
			!ally.stage.IsDungeon() || ally.deployedCreatureIndex >= uint32(len(ally.binding.Creatures)) ||
			ally.deployedHitPoint() <= 0 {
			continue
		}
		role := strings.ToLower(ally.binding.Creatures[ally.deployedCreatureIndex].ClassType)
		roles.counts[role]++
		if ally.binding.IsServerBot {
			continue
		}
		if role == "sentinel" {
			roles.humanSentinelCount++
		}
		if role == "ravager" {
			roles.humanRavagerCount++
		}
	}
	return roles
}

func campaignBotRoleScore(role string, healthRatio, powerRatio float32, roles campaignBotRoles) float32 {
	score := float32(-40 * roles.counts[role])
	switch role {
	case "sentinel":
		if roles.counts["sentinel"] == 0 {
			score += 45
		}
		if roles.humanSentinelCount > 0 {
			score -= 30
		}
	case "tempest":
		if roles.counts["tempest"] == 0 {
			score += 35
		}
		if roles.humanSentinelCount > 0 {
			score += 60
		}
	case "ravager":
		if roles.humanSentinelCount > 0 {
			score += 25
		}
		if roles.humanRavagerCount > 0 {
			score -= 30
		}
	default:
		return -1000
	}
	score += min(healthRatio, float32(1))*10 + min(powerRatio, float32(1))*5
	// Resource readiness dominates an improvement in role coverage.
	if healthRatio <= 0.25 {
		score -= 250
	}
	if powerRatio <= 0.20 {
		score -= 150
	}
	return score
}

func campaignBotRoleIndex(member gameplayPeerSession, roles campaignBotRoles) uint32 {
	selected := member.deployedCreatureIndex
	if member.squad == nil || selected >= uint32(len(member.binding.Creatures)) {
		return selected
	}
	bestScore := float32(-10000)
	currentScore := bestScore
	for index, creature := range member.binding.Creatures {
		character, isFound := member.squad.Character(uint32(index))
		if !isFound || !character.IsAvailable || character.HitPoints <= 0 || creature.Noun == 0 {
			continue
		}
		maximum := member.characterHitPointMaximum(uint32(index))
		powerMaximum := member.characterResourceMaximums(uint32(index)).manaPoint
		if maximum <= 0 || powerMaximum <= 0 {
			continue
		}
		score := campaignBotRoleScore(strings.ToLower(creature.ClassType), character.HitPoints/maximum,
			character.ManaPoints/powerMaximum, roles)
		if uint32(index) == member.deployedCreatureIndex {
			currentScore = score
		}
		if score > bestScore {
			bestScore, selected = score, uint32(index)
		}
	}
	// Keep the current hero on ties and small resource-only differences.
	if bestScore <= currentScore+15 {
		return member.deployedCreatureIndex
	}
	return selected
}

// Choose the opening hero before shields, passives and the public roster spawn.
// Checkpoint restoration retains its exact deployed hero and resources.
func (e gameplaySetupRuntime) initializeCampaignBotRole(member *gameplayPeerSession) error {
	if !member.binding.IsServerBot || member.binding.Mode != game.ModeChain {
		return nil
	}
	member.playerAI.nextRoleCheckAt = e.now().Add(campaignBotRoleInterval)
	// Restoration is authoritative even when the saved hero is currently dead.
	if member.zone.IsRestored() {
		return nil
	}
	if member.squad == nil {
		var err error
		member.squad, err = newCampaignSquad(member.binding.Creatures)
		if err != nil {
			return fmt.Errorf("roleSquad: %w", err)
		}
	}
	e.registry.mutex.RLock()
	roles := e.registry.campaignBotRolesLocked(*member)
	e.registry.mutex.RUnlock()
	index := campaignBotRoleIndex(*member, roles)
	err := member.squad.Deploy(index)
	if err != nil {
		return fmt.Errorf("roleDeploy: %w", err)
	}
	member.deployedCreatureIndex = index
	member.deployedObjectID = zonehero.ObjectID(member.binding.Slot, index)
	if e.logger != nil {
		e.logger.Printf("Campaign bot opening role game=%d slot=%d hero=%d class=%s", member.binding.GameID,
			member.binding.Slot, index, member.binding.Creatures[index].ClassType)
	}
	return nil
}

func (e gameplayPendingRuntime) playerAIRoleSwapLocked(member *gameplayPeerSession,
	command raknet.ActionCommandData, now time.Time,
) (raknet.ActionCommandData, bool) {
	if !member.binding.IsServerBot || member.binding.Mode != game.ModeChain || member.squad == nil ||
		now.Before(member.playerAI.nextRoleCheckAt) || !member.squad.IsDeployReady(now) ||
		member.playerAI.survival.recentLoss(now) > 0 || member.heroHealingTicks != nil ||
		member.playerAI.isTreeHealing || (member.basicSequence != nil && !member.basicSequence.IsReady(now)) {
		return command, false
	}
	// A nearby ally still taking damage is not downtime, even without our own target.
	for _, ally := range e.registry.sessions {
		if ally.zone == member.zone && ally.binding.GameID == member.binding.GameID &&
			ally.playerAI.survival.recentLoss(now) > 0 {
			return command, false
		}
	}
	member.playerAI.nextRoleCheckAt = now.Add(campaignBotRoleInterval)
	index := campaignBotRoleIndex(*member, e.registry.campaignBotRolesLocked(*member))
	if index == member.deployedCreatureIndex {
		return command, false
	}
	command.Common.Type = raknet.ActionSwitchCharacter
	command.Value = index
	member.playerAI.isMoving = false
	member.playerAI.isIdleWandering = false
	member.playerAI.nextDecisionAt = now.Add(time.Second)
	if e.logger != nil {
		e.logger.Printf("Campaign bot idle role swap game=%d slot=%d from=%d to=%d class=%s", member.binding.GameID,
			member.binding.Slot, member.deployedCreatureIndex, index, member.binding.Creatures[index].ClassType)
	}
	return command, true
}

package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

// Keep completed rounds independently of live network membership: build 103
// leaves gameplay while opening the result screen.
type arenaResultState struct {
	matchID         string
	isRanked        bool
	statEntries     []arenaStatEntry
	statMaximums    map[uint64]arenaStatMaximums
	isStatsSaved    bool
	startedAt       time.Time
	elapsed         time.Duration
	roundIndex      int
	isRoundComplete bool
	isCompleted     bool
	players         map[uint64]raknet.ArenaPlayerResult
	body            raknet.ArenaResultsBody
}

func (e *gameplaySessionRegistry) startArenaResultLocked(gameID uint32, now time.Time) {
	match := e.arenaMatches[gameID]
	if match == nil || match.result.isCompleted || !match.result.startedAt.IsZero() && !match.result.isRoundComplete {
		return
	}
	if match.result.players == nil {
		match.result.players = make(map[uint64]raknet.ArenaPlayerResult)
	}
	if match.result.isRoundComplete {
		match.result.roundIndex++
	}
	match.result.startedAt = now
	match.result.isRoundComplete = false
	match.recentDamages = nil
	for _, member := range e.sessions {
		if member.binding.GameID != gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		player, isFound := match.result.players[member.binding.UserID]
		if !isFound {
			player = arenaPlayerResult(member)
		}
		player.AvatarID = member.binding.AvatarID
		player.Team = uint8(member.binding.Team)
		if match.result.roundIndex < len(player.Round) {
			player.Round[match.result.roundIndex] = arenaPlayerResult(member).Round[0]
		}
		match.result.players[member.binding.UserID] = player
		if member.binding.Slot < uint16(len(match.result.body.Player)) {
			match.result.body.Player[member.binding.Slot] = player
		}
	}
}

func (e *arenaResultState) recordDamage(sourceUserID uint64, targetUserID uint64, amount float32, isLethal bool) {
	if e.isRoundComplete || e.isCompleted || e.startedAt.IsZero() || amount <= 0 {
		return
	}
	source, isSourceFound := e.players[sourceUserID]
	target, isTargetFound := e.players[targetUserID]
	if !isSourceFound || !isTargetFound || sourceUserID == targetUserID {
		return
	}
	if e.statMaximums == nil {
		e.statMaximums = make(map[uint64]arenaStatMaximums)
	}
	maximums := e.statMaximums[sourceUserID]
	maximums.damage = max(maximums.damage, amount)
	e.statMaximums[sourceUserID] = maximums
	source.DamageDealt += amount
	target.DamageTaken += amount
	if isLethal {
		source.Kills++
		target.Deaths++
	}
	e.players[sourceUserID] = source
	e.players[targetUserID] = target
}

func (e *arenaResultState) recordHealing(sourceUserID uint64, targetUserID uint64, amount float32) {
	if e.isRoundComplete || e.isCompleted || e.startedAt.IsZero() || amount <= 0 {
		return
	}
	source, isSourceFound := e.players[sourceUserID]
	target, isTargetFound := e.players[targetUserID]
	if !isSourceFound || !isTargetFound {
		return
	}
	if e.statMaximums == nil {
		e.statMaximums = make(map[uint64]arenaStatMaximums)
	}
	maximums := e.statMaximums[sourceUserID]
	maximums.healing = max(maximums.healing, amount)
	e.statMaximums[sourceUserID] = maximums
	if sourceUserID == targetUserID {
		source.HealingDealt += amount
		source.HealingReceived += amount
		e.players[sourceUserID] = source
		return
	}
	source.HealingDealt += amount
	target.HealingReceived += amount
	e.players[sourceUserID] = source
	e.players[targetUserID] = target
}

func arenaPlayerResult(member gameplayPeerSession) raknet.ArenaPlayerResult {
	player := raknet.ArenaPlayerResult{PlayerID: member.binding.UserID, AvatarID: member.binding.AvatarID, Team: uint8(member.binding.Team)}
	for index, creature := range member.binding.Creatures {
		if index >= len(player.Round[0].CreatureResource) {
			break
		}
		player.Round[0].CreatureResource[index] = zonehero.AppearanceAsset(creature)
		if member.squad == nil {
			continue
		}
		character, isFound := member.squad.Character(uint32(index))
		if !isFound {
			continue
		}
		player.Round[0].HealthPercent[index] = arenaResultPercent(character.HitPoints, creature.HitPoint)
		player.Round[0].ManaPercent[index] = arenaResultPercent(character.ManaPoints, creature.PowerPoint)
	}
	return player
}

func (e *gameplaySessionRegistry) completeArenaResultLocked(gameID uint32, winnerTeam uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.result.isRoundComplete || match.result.isCompleted {
		return
	}
	result := &match.result
	if result.roundIndex >= len(result.body.Player[0].Round) {
		return
	}
	if !result.startedAt.IsZero() {
		result.elapsed += max(time.Duration(0), e.now().Sub(result.startedAt))
	}
	if winnerTeam == 1 {
		result.body.CountA++
	} else if winnerTeam == 2 {
		result.body.CountB++
	}
	result.body.SelectorA = winnerTeam
	result.body.RoundTimeSeconds = int32(result.elapsed / time.Second)
	for _, member := range e.sessions {
		if member.binding.GameID != gameID || member.binding.Mode != game.ModeArena || member.binding.Slot >= uint16(len(result.body.Player)) {
			continue
		}
		player := result.players[member.binding.UserID]
		current := arenaPlayerResult(member)
		player.PlayerID = current.PlayerID
		player.AvatarID = current.AvatarID
		player.Team = current.Team
		player.Round[result.roundIndex] = current.Round[0]
		result.players[member.binding.UserID] = player
		result.body.Player[member.binding.Slot] = player
	}
	// A disconnected participant still owns their stable result slot. Refresh
	// counters from the retained match roster, even without a live session.
	for index, retained := range result.body.Player {
		player, isFound := result.players[retained.PlayerID]
		if !isFound {
			continue
		}
		if uint32(player.Team) == winnerTeam {
			player.Round[result.roundIndex].Outcome = 3
		}
		result.players[player.PlayerID] = player
		result.body.Player[index] = player
	}
	result.isRoundComplete = true
	result.isCompleted = result.body.CountA >= 2 || result.body.CountB >= 2
	result.freezeStats(e.now())
	match.isCombatActive = false
}

func (e gameplayArenaRuntime) arenaResultBodyLocked(gameID uint32) raknet.ArenaResultsBody {
	match := e.registry.arenaMatches[gameID]
	if match == nil {
		return raknet.ArenaResultsBody{}
	}
	result := match.result.body
	// Build 103 copies this body to cArenaResultsUI+4. OnContinueClicked
	// checks its second word (UI+8) before requesting Spaceship state 2;
	// zero only hides the results UI. Intermediate rounds must retain zero.
	result.SelectorB = 0
	if match.result.isCompleted {
		result.SelectorB = 1
	}
	return result
}

func arenaResultPercent(current float32, maximum float32) float32 {
	if maximum <= 0 {
		return 0
	}
	return min(float32(1), max(float32(0), current/maximum))
}

func (e *gameplaySessionRegistry) retireArenaResultLocked(gameID uint32) func() {
	match := e.arenaMatches[gameID]
	if match == nil {
		return nil
	}
	if len(match.result.statEntries) != 0 && !match.result.isStatsSaved {
		return nil
	}
	for _, member := range e.sessions {
		if member.binding.GameID == gameID {
			return nil
		}
	}
	for key := range e.retainedSessions {
		if key.gameID == gameID {
			return nil
		}
	}
	e.retireArenaForcePulsesLocked(gameID)
	delete(e.arenaMatches, gameID)
	return match.cancel
}

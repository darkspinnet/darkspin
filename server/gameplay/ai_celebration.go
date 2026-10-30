package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type arenaDamagePair struct {
	sourceUserID uint64
	targetUserID uint64
}

func (e *arenaResultState) recordCombatExchange(sourceUserID, targetUserID uint64, amount float32, at time.Time) {
	if e.isRoundComplete || e.isCompleted || amount <= 0 || sourceUserID == targetUserID {
		return
	}
	if e.damageContributions == nil {
		e.damageContributions = make(map[arenaDamagePair][]arenaRecentDamage)
	}
	pair := arenaDamagePair{sourceUserID: sourceUserID, targetUserID: targetUserID}
	hits := e.damageContributions[pair]
	first := 0
	for first < len(hits) && at.Sub(hits[first].at) >= 4*time.Second {
		first++
	}
	e.damageContributions[pair] = append(hits[first:], arenaRecentDamage{at: at, amount: amount})
}

type arenaVictoryDance struct {
	runtime    campaignAbilityCommandRuntime
	match      *arenaMatchState
	gameID     uint32
	sessionKey string
	generation uint64
	objectID   uint32
	startedAt  time.Time
	sourceTime uint64
}

func (e campaignAbilityCommandRuntime) arenaVictoryDanceLocked(match *arenaMatchState, victim gameplayPeerSession, winnerTeam uint32) *arenaVictoryDance {
	if victim.binding.IsServerBot || match.random == nil {
		return nil
	}
	killerID := match.result.lastKillers[victim.binding.UserID]
	maximum := victim.characterHitPointMaximum(victim.deployedCreatureIndex)
	contribution := float32(0)
	for _, hit := range match.result.damageContributions[arenaDamagePair{sourceUserID: killerID, targetUserID: victim.binding.UserID}] {
		if e.now().Sub(hit.at) < 4*time.Second {
			contribution += hit.amount
		}
	}
	if maximum <= 0 {
		return nil
	}
	for key, member := range e.registry.sessions {
		if member.binding.GameID != victim.binding.GameID || !member.binding.IsServerBot ||
			member.binding.UserID != killerID || uint32(member.binding.Team) != winnerTeam || member.deployedHitPoint() <= 0 ||
			zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(victim.playerPosition)) > 3 {
			continue
		}
		received := float32(0)
		for _, hit := range match.result.damageContributions[arenaDamagePair{sourceUserID: victim.binding.UserID, targetUserID: killerID}] {
			if e.now().Sub(hit.at) < 4*time.Second {
				received += hit.amount
			}
		}
		killerMaximum := member.characterHitPointMaximum(member.deployedCreatureIndex)
		dealtRatio := contribution / maximum
		isBurst := dealtRatio >= 0.25
		isDominant := killerMaximum > 0 && dealtRatio >= 0.10 && received/killerMaximum <= dealtRatio*0.35
		if !isBurst && !isDominant {
			return nil
		}
		if match.random.Float64() >= 0.30 {
			return nil
		}
		now := e.now()
		sourceTime := uint64(0)
		if match.clock != nil {
			sourceTime = match.clock.sourceTime + uint64(max(time.Duration(0), now.Sub(match.clock.startedAt))/time.Millisecond)
		}
		return &arenaVictoryDance{runtime: e, match: match, gameID: member.binding.GameID,
			sessionKey: key, generation: member.generation, objectID: member.deployedObjectID,
			startedAt: now, sourceTime: sourceTime}
	}
	return nil
}

func (e *arenaVictoryDance) scheduleLocked() {
	if e.runtime.registry.timer == nil {
		return
	}
	cancel, err := e.runtime.registry.timer.Schedule(250*time.Millisecond, e.execute)
	if err != nil || cancel == nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena victory dance schedule failed game=%d: error=%v cancellation=%t", e.gameID, err, cancel != nil)
		}
	}
}

func (e *arenaVictoryDance) execute() {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	if e.runtime.registry.isClosed || !isFound || match == nil || match != e.match || !match.result.isCompleted ||
		member.generation != e.generation || member.deployedObjectID != e.objectID ||
		member.isArenaResultReady || e.runtime.now().Sub(e.startedAt) >= 4*time.Second {
		e.runtime.registry.mutex.Unlock()
		return
	}
	if !member.isAbilityReleaseReady(e.runtime.now()) || member.basicAttack != nil {
		e.scheduleLocked()
		e.runtime.registry.mutex.Unlock()
		return
	}
	e.runtime.registry.mutex.Unlock()
	packet := member.schedulePacket.Autonomous()
	packet.Address = serverBotAddress(member.binding)
	packet.SourceTime = e.sourceTime + uint64(max(time.Duration(0), e.runtime.now().Sub(e.startedAt))/time.Millisecond)
	simple := gameplaySimpleActionRuntime{registry: e.runtime.registry, program: e.runtime.program,
		now: e.runtime.now, logger: e.runtime.logger}
	packets, err := simple.dance(packet, raknet.ActionCommandData{Common: raknet.ActionCommon{
		Type: raknet.ActionDance, ObjectID: e.objectID, Position: member.playerPosition}})
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena victory dance failed game=%d: %v", e.gameID, err)
		}
		return
	}
	e.runtime.registry.mutex.Lock()
	if e.runtime.registry.arenaMatches[e.gameID] == e.match {
		e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	}
	e.runtime.registry.mutex.Unlock()
}

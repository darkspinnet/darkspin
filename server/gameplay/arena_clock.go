package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
)

// Arena has no campaign zone. Publish the same active game-state clock used
// by campaign, with an elapsed epoch owned by the current combat round.
type arenaRoundClock struct {
	registry      *gameplaySessionRegistry
	match         *arenaMatchState
	gameID        uint32
	roundIndex    int
	startedAt     time.Time
	regeneratedAt time.Time
	sourceTime    uint64
	cancel        func()
}

func (e *arenaRoundClock) stopLocked() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e *arenaRoundClock) scheduleLocked() error {
	if e.registry.timer == nil {
		return errors.New("Arena clock timer unavailable")
	}
	cancel, err := e.registry.timer.Schedule(time.Second, e.publish)
	if err != nil {
		return fmt.Errorf("clockSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("Arena clock cancellation unavailable")
	}
	e.cancel = cancel
	return nil
}

func (e *arenaRoundClock) publish() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	match := e.registry.arenaMatches[e.gameID]
	if e.registry.isClosed || match == nil || match != e.match || match.clock != e ||
		!match.isCombatActive || match.result.isRoundComplete || match.result.isCompleted ||
		match.result.roundIndex != e.roundIndex {
		return
	}
	e.cancel = nil
	now := e.registry.now()
	elapsed := uint64(max(time.Duration(0), now.Sub(e.startedAt)) / time.Millisecond)
	packet, err := raknet.MarshalApplication(raknet.GameStateMessage{Data: raknet.GameStateData{
		GameTime: e.sourceTime + elapsed, TimeElapsed: elapsed,
		State: raknet.GameDungeon, Type: raknet.GameTypeArena,
	}})
	if err != nil {
		if e.registry.logger != nil {
			e.registry.logger.Printf("Arena round clock publication failed game=%d: %v", e.gameID, err)
		}
		return
	}
	if e.regeneratedAt.IsZero() {
		e.regeneratedAt = e.startedAt
	}
	regenSeconds := max(float64(0), now.Sub(e.regeneratedAt).Seconds())
	e.regeneratedAt = now
	e.regenerateReservesLocked(regenSeconds)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena ||
			member.binding.IsServerBot || !member.isArenaCombatActive || !member.stage.IsDungeon() {
			continue
		}
		member.queuePackets([][]byte{packet})
		e.registry.sessions[key] = member
	}
	err = e.scheduleLocked()
	if err != nil && e.registry.logger != nil {
		e.registry.logger.Printf("Arena round clock stopped game=%d: %v", e.gameID, err)
	}
}

// regenerateReservesLocked restores power to living, inactive Arena heroes in
// every squad. Resource updates reach every real player so reserve HUDs agree.
func (e *arenaRoundClock) regenerateReservesLocked(seconds float64) {
	if seconds <= 0 {
		return
	}
	for _, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena ||
			!member.isArenaCombatActive || !member.stage.IsDungeon() || member.squad == nil ||
			member.squad.IsGameOver() {
			continue
		}
		for index := uint32(0); index < squad.Size; index++ {
			if index == member.deployedCreatureIndex {
				continue
			}
			character, isFound := member.squad.Character(index)
			if !isFound || !character.IsAvailable || character.HitPoints <= 0 {
				continue
			}
			maximum := member.characterManaPointMaximum(index)
			cap := maximum * 0.5
			if cap <= 0 || character.ManaPoints >= cap {
				continue
			}
			manaPoint := min(cap, character.ManaPoints+maximum*float32(seconds)*0.01)
			resourcePacket, err := member.marshalCampaignCharacterResourceValues(index, character.HitPoints, manaPoint)
			if err != nil {
				e.logReserveRegenError(member.binding.UserID, index, err)
				continue
			}
			err = member.squad.SetManaPoints(index, manaPoint)
			if err != nil {
				e.logReserveRegenError(member.binding.UserID, index, err)
				continue
			}
			for recipientKey, recipient := range e.registry.sessions {
				if recipient.binding.GameID != e.gameID || recipient.binding.Mode != game.ModeArena ||
					recipient.binding.IsServerBot || !recipient.stage.IsDungeon() {
					continue
				}
				recipient.queuePackets([][]byte{resourcePacket})
				e.registry.sessions[recipientKey] = recipient
			}
		}
	}
}

func (e *arenaRoundClock) logReserveRegenError(userID uint64, index uint32, err error) {
	if e.registry.logger != nil {
		e.registry.logger.Printf("Arena reserve regeneration failed game=%d user=%d creature=%d: %v",
			e.gameID, userID, index, err)
	}
}

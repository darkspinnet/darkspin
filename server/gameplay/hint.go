package gameplay

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

var hintDirections = [...]string{
	"North",
	"North-Northeast",
	"Northeast",
	"East-Northeast",
	"East",
	"East-Southeast",
	"Southeast",
	"South-Southeast",
	"South",
	"South-Southwest",
	"Southwest",
	"West-Southwest",
	"West",
	"West-Northwest",
	"Northwest",
	"North-Northwest",
}

func (e *gameplaySessionRegistry) Hint(
	ctx context.Context, req chat.HintRequest,
) (chat.HintResult, error) {
	err := ctx.Err()
	if err != nil {
		return chat.HintResult{}, fmt.Errorf("hintContext: %w", err)
	}
	if e == nil || req.Sender.ID <= 0 || req.GameID == 0 {
		return chat.HintResult{}, fmt.Errorf("hintRequest: %w", chat.ErrHintUnavailable)
	}
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	for _, peerSession := range e.sessions {
		if peerSession.binding.UserID != uint64(req.Sender.ID) ||
			peerSession.binding.GameID != req.GameID || peerSession.zone == nil {
			continue
		}
		return hintForSession(peerSession)
	}
	return chat.HintResult{}, fmt.Errorf("hintSession: %w", chat.ErrHintUnavailable)
}

func hintForSession(peerSession gameplayPeerSession) (chat.HintResult, error) {
	npcSession := peerSession.zone.NPCs()
	if npcSession == nil {
		return chat.HintResult{}, fmt.Errorf("hintZone: %w", chat.ErrHintUnavailable)
	}
	playerPosition := game.Vec3(peerSession.playerPosition)
	nearestDistanceSquared := float32(math.Inf(1))
	nearestNPC := zonenpc.Snapshot{}
	for _, npc := range npcSession.Snapshots() {
		if npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 ||
			npc.Plan.IsFixture || npc.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		deltaX := npc.Plan.Position.X - playerPosition.X
		deltaY := npc.Plan.Position.Y - playerPosition.Y
		distanceSquared := deltaX*deltaX + deltaY*deltaY
		if distanceSquared >= nearestDistanceSquared {
			continue
		}
		nearestDistanceSquared = distanceSquared
		nearestNPC = npc
	}
	if nearestNPC.Plan.ObjectID == 0 {
		return chat.HintResult{}, nil
	}
	return chat.HintResult{
		Name:      hintNPCName(peerSession, nearestNPC),
		Direction: hintDirection(playerPosition, nearestNPC.Plan.Position),
	}, nil
}

func hintNPCName(peerSession gameplayPeerSession, npc zonenpc.Snapshot) string {
	if displayName := strings.TrimSpace(npc.Plan.BossIdentity.DisplayName); displayName != "" {
		return displayName
	}
	director := peerSession.zone.DirectorDefinition()
	identity, isFound := director.NPCIdentitiesByNoun[strings.ToLower(npc.Plan.NounName)]
	if isFound {
		if displayName := strings.TrimSpace(identity.DisplayName); displayName != "" {
			return displayName
		}
	}
	nounName := strings.TrimSuffix(strings.TrimSpace(npc.Plan.NounName), ".noun")
	nounName = strings.TrimSuffix(nounName, ".Noun")
	nounName = strings.ReplaceAll(nounName, "_", " ")
	if nounName == "" {
		return "Unknown mob"
	}
	return nounName
}

func hintDirection(source game.Vec3, target game.Vec3) string {
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	if deltaX == 0 && deltaY == 0 {
		return "Nearby"
	}
	angle := math.Atan2(float64(deltaX), float64(deltaY))
	if angle < 0 {
		angle += 2 * math.Pi
	}
	directionIndex := int(math.Floor(angle/(2*math.Pi/16)+0.5)) % len(hintDirections)
	return hintDirections[directionIndex]
}

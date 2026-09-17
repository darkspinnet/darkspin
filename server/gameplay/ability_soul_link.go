package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/squad"
)

type soulLinkShare struct {
	creatureIndex uint32
	hitPoint      float32
	damage        float32
}

type soulLinkDistribution struct {
	activeDamage float32
	shares       []soulLinkShare
}

func (e gameplayPeerSession) prepareSoulLinkDamage(
	damage float32,
) (soulLinkDistribution, error) {
	distribution := soulLinkDistribution{activeDamage: damage}
	if damage <= 0 || e.squad == nil || e.heroModifierRun == nil ||
		!e.heroModifierRun.isSoulLink ||
		e.heroModifierRun.creatureIndex != e.deployedCreatureIndex {
		return distribution, nil
	}
	livingCount := e.squad.LivingCount()
	if livingCount <= 1 {
		return distribution, nil
	}
	distribution.activeDamage = damage / float32(livingCount)
	distribution.shares = make([]soulLinkShare, 0, livingCount-1)
	for index := uint32(0); index < squad.Size; index++ {
		if index == e.deployedCreatureIndex {
			continue
		}
		character, isFound := e.squad.Character(index)
		if !isFound {
			return soulLinkDistribution{}, fmt.Errorf(
				"soulLinkCharacter[%d]: %w", index, errors.New("missing"),
			)
		}
		if !character.IsAvailable || character.HitPoints <= 0 {
			continue
		}
		hitPoint := max(float32(0), character.HitPoints-distribution.activeDamage)
		distribution.shares = append(distribution.shares, soulLinkShare{
			creatureIndex: index, hitPoint: hitPoint,
			damage: character.HitPoints - hitPoint,
		})
	}
	return distribution, nil
}

func (e soulLinkDistribution) commit(
	peerSession *gameplayPeerSession,
) ([][]byte, float32, error) {
	if peerSession == nil || peerSession.squad == nil {
		return nil, 0, errors.New("soul link squad unavailable")
	}
	packets := make([][]byte, len(e.shares))
	for index, share := range e.shares {
		packet, err := peerSession.marshalCampaignCharacterResourceAt(
			share.creatureIndex, share.hitPoint,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("soulLinkResource[%d]: %w", index, err)
		}
		packets[index] = packet
	}
	totalDamage := float32(0)
	for index, share := range e.shares {
		_, err := peerSession.squad.SetHitPoints(
			share.creatureIndex, share.hitPoint,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("soulLinkHealth[%d]: %w", index, err)
		}
		totalDamage += share.damage
	}
	return packets, totalDamage, nil
}

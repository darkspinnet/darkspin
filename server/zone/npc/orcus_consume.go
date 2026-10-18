package npc

// The authored LowHealth conditions activate once at 75, 50 and 25 percent.
// Store the claimed bands on the shared boss, rather than on a co-op peer.
func (e *Session) ClaimOrcusConsume(objectID uint32) (uint8, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	boss, isFound := e.npcs[objectID]
	if !isFound || boss.IsDefeated || boss.HitPoint <= 0 || boss.Plan.NPCProfile.HitPoint <= 0 {
		return 0, false
	}
	for index, fraction := range [...]float32{.75, .5, .25} {
		band := uint8(1 << index)
		if boss.orcusConsumeBands&band != 0 || boss.HitPoint > boss.Plan.NPCProfile.HitPoint*fraction {
			continue
		}
		boss.orcusConsumeBands |= band
		e.npcs[objectID] = boss
		return band, true
	}
	return 0, false
}

func (e *Session) RollbackOrcusConsume(objectID uint32, band uint8) {
	e.mu.Lock()
	defer e.mu.Unlock()
	boss, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	boss.orcusConsumeBands &^= band
	e.npcs[objectID] = boss
}

// Consume takes control of every living servant. Advancing the generation
// retires its previous attack callbacks without changing its hostile target.
func (e *Session) SetOrcusFollowing(objectID uint32, isFollowing bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	servant, isFound := e.npcs[objectID]
	if !isFound || servant.IsDefeated || servant.IsOrcusFollowing == isFollowing {
		return
	}
	servant.IsOrcusFollowing = isFollowing
	servant.ActionGeneration++
	servant.ActionOwner = ActionOwner{}
	if isFollowing {
		boss, isBossFound := e.npcs[servant.Plan.OwnerObjectID]
		if isBossFound {
			servant.ActionOwner = boss.ActionOwner
		}
	}
	servant.IsActionStarted = isFollowing
	if !isFollowing {
		servant.IsNavigationCollisionEnabled = true
	}
	e.npcs[objectID] = servant
}

func (e *Session) UpdateOrcusFollowCollision(objectID uint32, isNearOwner bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	servant, isFound := e.npcs[objectID]
	if !isFound || !servant.IsOrcusFollowing || !isNearOwner {
		return
	}
	servant.IsNavigationCollisionEnabled = false
	e.npcs[objectID] = servant
}

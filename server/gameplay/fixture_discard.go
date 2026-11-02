package gameplay

// A ticket spans the caller's WHOLE discard/reset boundary, including unlocked
// waits. Independent tickets prevent one caller from reopening another's world.
type fixtureDiscard struct {
	registry *gameplaySessionRegistry
	gameID   uint32
}

func (e *gameplaySessionRegistry) beginFixtureBlastDiscardLocked(gameID uint32) *fixtureDiscard {
	ticket := &fixtureDiscard{registry: e, gameID: gameID}
	if e.fixtureDiscards == nil {
		e.fixtureDiscards = make(map[*fixtureDiscard]struct{})
	}
	e.fixtureDiscards[ticket] = struct{}{}
	return ticket
}

func (e *gameplaySessionRegistry) isFixtureDiscardingLocked(gameID uint32) bool {
	for ticket := range e.fixtureDiscards {
		if ticket.gameID == 0 || ticket.gameID == gameID {
			return true
		}
	}
	return false
}

func (e *fixtureDiscard) prepareLocked() (fixtureHandoffBatch, []<-chan struct{}) {
	registry := e.registry
	publications := make([]*campaignFixturePublication, 0)
	settlements := make([]<-chan struct{}, 0)
	for publication := range registry.fixturePublications {
		if e.gameID != 0 && publication.identity.memberKey.gameID != e.gameID {
			continue
		}
		publication.isDiscardPending = true
		publications = append(publications, publication)
		if publication.isProducing {
			settlements = append(settlements, publication.productionDone)
		}
	}
	batch := fixtureHandoffBatch{}
	if len(settlements) != 0 {
		return batch, settlements
	}
	for _, publication := range publications {
		publication.retireBlastLocked(&batch)
	}
	return batch, nil
}

// The caller invokes this only after its actual zone discard/reset completes.
func (e *fixtureDiscard) finishLocked() {
	delete(e.registry.fixtureDiscards, e)
}

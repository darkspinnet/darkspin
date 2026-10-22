package gameplay

// wait settles accepted initial blast work before its world is reset or
// destroyed. The ticket continues blocking admission until finishLocked.
// Callers must not hold the registry lock while waiting.
func (e *fixtureDiscard) wait() {
	for {
		e.registry.mutex.Lock()
		batch, settlements := e.prepareLocked()
		e.registry.mutex.Unlock()
		batch.complete()
		if len(settlements) == 0 {
			return
		}
		for _, settlement := range settlements {
			<-settlement
		}
	}
}

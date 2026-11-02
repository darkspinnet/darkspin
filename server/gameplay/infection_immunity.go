package gameplay

import "time"

func (e *heroInfectionRun) applyImmunity(entry heroInfectionEntry, at time.Time) {
	e.npc.RetireInfection(entry.objectID, entry.modifierInstanceID, entry.isPrimary, at)
}

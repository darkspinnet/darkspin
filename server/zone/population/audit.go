package population

import (
	"github.com/darkspinnet/darkspin/server/sim"
)

// PopulationLocus identifies one authored point after map variant selection.
type PopulationLocus struct {
	ID      uint32
	Kind    sim.DirectorLocusKind
	Section sim.DirectorRouteSection
}

// SectionAudit separates authored points from the local population recipe.
type SectionAudit struct {
	Section                   sim.DirectorRouteSection
	Recipe                    string
	InputWandererLoci         int
	InputSpikeLoci            int
	ExcludedSpikeLoci         int
	RecipeOmittedWandererLoci int
	RecipeOmittedSpikeLoci    int
	RetainedWandererLoci      int
	RetainedSpikeLoci         int
	PendingEncounterLoci      int
}

func (e *Session) SectionAudits() []SectionAudit {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	sections := []sim.DirectorRouteSection{
		sim.DirectorRouteSectionA, sim.DirectorRouteSectionB,
		sim.DirectorRouteSectionC, sim.DirectorRouteSectionAny,
	}
	audits := make([]SectionAudit, 0, len(sections))
	retainedIDs := make(map[uint32]bool)
	for _, candidate := range e.candidates {
		for _, locusID := range candidate.sourceLocusIDs {
			retainedIDs[locusID] = true
		}
	}
	excludedIDs := make(map[uint32]bool, len(e.excludedLoci))
	for _, locus := range e.excludedLoci {
		excludedIDs[locus.ID] = true
	}
	for _, section := range sections {
		audit := SectionAudit{Section: section, Recipe: e.recipe}
		for _, locus := range e.inputLoci {
			if locus.Section != section {
				continue
			}
			switch locus.Kind {
			case sim.DirectorLocusWanderer:
				audit.InputWandererLoci++
				if retainedIDs[locus.ID] {
					audit.RetainedWandererLoci++
				} else {
					audit.RecipeOmittedWandererLoci++
				}
			case sim.DirectorLocusSpike:
				audit.InputSpikeLoci++
				if excludedIDs[locus.ID] {
					audit.ExcludedSpikeLoci++
				} else if retainedIDs[locus.ID] {
					audit.RetainedSpikeLoci++
				} else {
					audit.RecipeOmittedSpikeLoci++
				}
			}
		}
		for _, candidate := range e.candidates {
			if candidate.section == section &&
				(candidate.kind == sim.DirectorLocusSpike || candidate.isAmbush) &&
				!e.resolvedDirectorPointIDs[candidate.locusID] {
				audit.PendingEncounterLoci++
			}
		}
		audits = append(audits, audit)
	}
	return audits
}

func SectionLabel(section sim.DirectorRouteSection) string {
	switch section {
	case sim.DirectorRouteSectionA:
		return "A"
	case sim.DirectorRouteSectionB:
		return "B"
	case sim.DirectorRouteSectionC:
		return "C"
	case sim.DirectorRouteSectionAny:
		return "Any"
	default:
		return "Unknown"
	}
}

package zone

import zoneability "github.com/darkspinnet/darkspin/server/zone/ability"

func (e *Zone) TreeOfLife() *zoneability.TreeOfLifePresentationSession {
	if e == nil {
		return nil
	}
	return e.treeOfLife
}

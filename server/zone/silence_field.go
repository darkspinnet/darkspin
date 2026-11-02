package zone

import "errors"

// SilenceFieldContact is the trigger's retained membership, distinct from the
// recipient modifier. A dispelled/dead recipient is not re-admitted until exit.
type SilenceFieldContact struct {
	SourceObjectID uint32
	TargetObjectID uint32
	InstanceID     uint32
}

type silenceFieldKey struct {
	sourceObjectID uint32
	targetObjectID uint32
}

func (e *Zone) SilenceFieldContacts() []SilenceFieldContact {
	e.mu.RLock()
	defer e.mu.RUnlock()
	contacts := make([]SilenceFieldContact, 0, len(e.silenceFieldContacts))
	for _, contact := range e.silenceFieldContacts {
		contacts = append(contacts, contact)
	}
	return contacts
}

func (e *Zone) PutSilenceFieldContact(contact SilenceFieldContact) error {
	if e == nil || contact.SourceObjectID == 0 || contact.TargetObjectID == 0 {
		return errors.New("invalid silence field contact")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.silenceFieldContacts == nil {
		e.silenceFieldContacts = make(map[silenceFieldKey]SilenceFieldContact)
	}
	e.silenceFieldContacts[silenceFieldKey{contact.SourceObjectID, contact.TargetObjectID}] = contact
	return nil
}

func (e *Zone) RemoveSilenceFieldContact(contact SilenceFieldContact) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.silenceFieldContacts, silenceFieldKey{contact.SourceObjectID, contact.TargetObjectID})
}

func (e *Zone) IsNPCDestroyed(objectID uint32) bool {
	return e == nil || e.projection == nil || e.projection.IsNPCDestroyed(objectID)
}

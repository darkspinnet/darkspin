package blaze

import (
	"reflect"
	"sync"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type presenceRecord struct {
	variables []tdf.Field
	revision  uint64
}

// PresenceRegistry retains each user's last exact client-authored presence envelope.
type PresenceRegistry struct {
	mu           sync.RWMutex
	records      map[int64]presenceRecord
	nextRevision uint64
}

// NewPresenceRegistry creates an empty presence registry shared by social components.
func NewPresenceRegistry() *PresenceRegistry {
	return &PresenceRegistry{records: make(map[int64]presenceRecord)}
}

func (e *PresenceRegistry) update(userID int64, variables []tdf.Field) (presenceRecord, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	record, isFound := e.records[userID]
	if isFound && reflect.DeepEqual(record.variables, variables) {
		return record, false
	}
	e.nextRevision++
	record = presenceRecord{
		variables: append([]tdf.Field(nil), variables...),
		revision:  e.nextRevision,
	}
	e.records[userID] = record
	return record, true
}

func (e *PresenceRegistry) recordsSnapshot() map[int64]presenceRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()
	records := make(map[int64]presenceRecord, len(e.records))
	for userID, record := range e.records {
		record.variables = append([]tdf.Field(nil), record.variables...)
		records[userID] = record
	}
	return records
}

func (e *PresenceRegistry) lookup(userID int64) (presenceRecord, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	record, isFound := e.records[userID]
	record.variables = append([]tdf.Field(nil), record.variables...)
	return record, isFound
}

func presenceVariableFields(fields []tdf.Field) []tdf.Field {
	for _, field := range fields {
		if field.Label != "CVAR" || field.Value.Type != tdf.Variable {
			continue
		}
		if field.Value.VariableField == nil || field.Value.VariableField.Label != "CVAR" {
			continue
		}
		if field.Value.VariableField.Value.Type != tdf.Struct {
			continue
		}
		return []tdf.Field{field}
	}
	return nil
}

func presenceVariableDataFields(variables []tdf.Field) []tdf.Field {
	if len(variables) != 1 || variables[0].Value.VariableField == nil {
		return nil
	}
	return variables[0].Value.VariableField.Value.Fields
}

func authoritativePresenceVariableFields(
	user *sporenet.User, users []*sporenet.User, variables []tdf.Field,
) []tdf.Field {
	if user == nil || len(variables) != 1 || variables[0].Value.VariableField == nil {
		return variables
	}
	partySize := uint64(1)
	playgroupID := user.CurrentPlaygroupID()
	if playgroupID != 0 {
		partySize = 0
		for _, candidate := range users {
			if candidate != nil && candidate.CurrentPlaygroupID() == playgroupID {
				partySize++
			}
		}
		if partySize == 0 {
			partySize = 1
		}
	}
	projectedVariables := append([]tdf.Field(nil), variables...)
	variableField := *projectedVariables[0].Value.VariableField
	presenceFields := append([]tdf.Field(nil), variableField.Value.Fields...)
	replaceField(presenceFields, "GRP", tdf.IntegerValue(partySize))
	presence := user.PresenceSnapshot()
	replaceField(presenceFields, "LVL", tdf.IntegerValue(uint64(presence.Experience)))
	replaceField(presenceFields, "XTRA", tdf.IntegerValue(uint64(presence.ChainProgression+1)))
	variableField.Value.Fields = presenceFields
	projectedVariables[0].Value.VariableField = &variableField
	return projectedVariables
}

func presenceNotificationFields(user *sporenet.User, variables []tdf.Field) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("DATA", tdf.StructValue(extendedDataFieldsWithPresence(user, variables)...)),
		tdf.FieldNamed("USID", tdf.IntegerValue(uint64(user.Account.ID))),
	}
}

package sqlite

import "github.com/darkspinnet/darkspin/server/sim"

// Keep scalar checkpoint fields unchanged. A table reference alone carries
// TableIndex, including an explicit zero; graph validation stays in sim.
type objectiveRuntimePayload struct {
	ObjectiveID   uint32
	ChunkID       int64
	SHA256        string
	MemoryBudget  int
	PrivateTables []objectiveTablePayload
}

type objectiveTablePayload struct {
	Fields []objectiveFieldPayload
}

type objectiveFieldPayload struct {
	Key  objectiveScalarPayload
	Data objectiveScalarPayload
}

type objectiveScalarPayload struct {
	Kind       string
	Number     float64
	Text       string
	IsTrue     bool
	TableIndex *int `json:"TableIndex,omitempty"`
}

func encodeObjectiveRuntimes(snapshots []sim.LuaObjectiveSnapshot) []objectiveRuntimePayload {
	if snapshots == nil {
		return nil
	}
	runtimes := make([]objectiveRuntimePayload, len(snapshots))
	for runtimeIndex, snapshot := range snapshots {
		runtime := objectiveRuntimePayload{
			ObjectiveID: snapshot.ObjectiveID, ChunkID: snapshot.ChunkID,
			SHA256: snapshot.SHA256, MemoryBudget: snapshot.MemoryBudget,
		}
		if snapshot.PrivateTables != nil {
			runtime.PrivateTables = make([]objectiveTablePayload, len(snapshot.PrivateTables))
		}
		for tableIndex, table := range snapshot.PrivateTables {
			if table.Fields != nil {
				runtime.PrivateTables[tableIndex].Fields = make([]objectiveFieldPayload, len(table.Fields))
			}
			for fieldIndex, field := range table.Fields {
				runtime.PrivateTables[tableIndex].Fields[fieldIndex] = objectiveFieldPayload{
					Key: encodeObjectiveScalar(field.Key), Data: encodeObjectiveScalar(field.Data),
				}
			}
		}
		runtimes[runtimeIndex] = runtime
	}
	return runtimes
}

func encodeObjectiveScalar(field sim.LuaObjectiveScalar) objectiveScalarPayload {
	encoded := objectiveScalarPayload{Kind: field.Kind, Number: field.Number, Text: field.Text, IsTrue: field.IsTrue}
	if field.Kind == "table" {
		tableIndex := field.TableIndex
		encoded.TableIndex = &tableIndex
	}
	return encoded
}

func decodeObjectiveRuntimes(runtimes []objectiveRuntimePayload) []sim.LuaObjectiveSnapshot {
	if runtimes == nil {
		return nil
	}
	snapshots := make([]sim.LuaObjectiveSnapshot, len(runtimes))
	for runtimeIndex, runtime := range runtimes {
		snapshot := sim.LuaObjectiveSnapshot{
			ObjectiveID: runtime.ObjectiveID, ChunkID: runtime.ChunkID,
			SHA256: runtime.SHA256, MemoryBudget: runtime.MemoryBudget,
		}
		if runtime.PrivateTables != nil {
			snapshot.PrivateTables = make([]sim.LuaObjectiveTable, len(runtime.PrivateTables))
		}
		for tableIndex, table := range runtime.PrivateTables {
			if table.Fields != nil {
				snapshot.PrivateTables[tableIndex].Fields = make([]sim.LuaObjectiveField, len(table.Fields))
			}
			for fieldIndex, field := range table.Fields {
				snapshot.PrivateTables[tableIndex].Fields[fieldIndex] = sim.LuaObjectiveField{
					Key: decodeObjectiveScalar(field.Key), Data: decodeObjectiveScalar(field.Data),
				}
			}
		}
		snapshots[runtimeIndex] = snapshot
	}
	return snapshots
}

func decodeObjectiveScalar(encoded objectiveScalarPayload) sim.LuaObjectiveScalar {
	field := sim.LuaObjectiveScalar{Kind: encoded.Kind, Number: encoded.Number, Text: encoded.Text, IsTrue: encoded.IsTrue}
	if encoded.TableIndex != nil {
		field.TableIndex = *encoded.TableIndex
	}
	return field
}

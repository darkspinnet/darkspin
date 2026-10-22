package sim

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const objectiveStateTableLimit = 5
const objectiveStateFieldLimit = 64
const objectiveStateTextLimit = 4096

// LuaObjectiveSnapshot preserves bounded private data independently of UI
// tokens. Table references retain shared data identities; executable closures,
// native functions, inherited tables and cyclic graphs are unsupported.
type LuaObjectiveSnapshot struct {
	ObjectiveID   uint32
	ChunkID       int64
	SHA256        string
	MemoryBudget  int
	PrivateTables []LuaObjectiveTable
}

type LuaObjectiveTable struct {
	Fields []LuaObjectiveField
}

type LuaObjectiveField struct {
	Key  LuaObjectiveScalar
	Data LuaObjectiveScalar
}

// A snapshot without tables represents no private root, rather than an empty
// root. A nonempty snapshot starts with its root at table index zero. Table
// references are allowed only as field data, never as keys.
type LuaObjectiveScalar struct {
	Kind       string
	Number     float64
	Text       string
	IsTrue     bool
	TableIndex int
}

func (e LuaObjectiveSnapshot) Clone() LuaObjectiveSnapshot {
	e.PrivateTables = append([]LuaObjectiveTable(nil), e.PrivateTables...)
	for index := range e.PrivateTables {
		e.PrivateTables[index].Fields = append(
			[]LuaObjectiveField(nil), e.PrivateTables[index].Fields...,
		)
	}
	return e
}

func (e *LuaObjectiveRuntime) SnapshotState() (LuaObjectiveSnapshot, error) {
	if e == nil || e.compiler == nil {
		return LuaObjectiveSnapshot{}, errors.New("objective runtime unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot := LuaObjectiveSnapshot{
		ObjectiveID: e.input.ObjectiveID, ChunkID: e.input.Root.ChunkID,
		SHA256: e.input.Root.SHA256, MemoryBudget: e.compiler.memoryBudget,
	}
	if e.compiler.privateTable == nil {
		return snapshot, nil
	}
	tables := []*luaTable{e.compiler.privateTable}
	tableIndexes := map[*luaTable]int{e.compiler.privateTable: 0}
	fieldCount := 0
	for tableIndex := 0; tableIndex < len(tables); tableIndex++ {
		table := tables[tableIndex]
		if table.parent != nil {
			return LuaObjectiveSnapshot{}, errors.New("objective inherited private table")
		}
		if len(table.fields) > objectiveStateFieldLimit-fieldCount {
			return LuaObjectiveSnapshot{}, errors.New("objective private field limit")
		}
		keys := make([]luaKey, 0, len(table.fields))
		for key := range table.fields {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			if keys[left].kind != keys[right].kind {
				return keys[left].kind < keys[right].kind
			}
			if keys[left].kind == luaNumber {
				return keys[left].number < keys[right].number
			}
			if keys[left].kind == luaBoolean {
				return !keys[left].isTrue && keys[right].isTrue
			}
			return keys[left].text < keys[right].text
		})
		fields := make([]LuaObjectiveField, 0, len(keys))
		for _, key := range keys {
			fieldCount++
			if fieldCount > objectiveStateFieldLimit {
				return LuaObjectiveSnapshot{}, errors.New("objective private field limit")
			}
			keyData, err := snapshotObjectiveScalar(luaValue{
				kind: key.kind, number: key.number, text: key.text, isTrue: key.isTrue,
			})
			if err != nil {
				return LuaObjectiveSnapshot{}, fmt.Errorf("privateKey: %w", err)
			}
			field := table.fields[key]
			var data LuaObjectiveScalar
			if field.kind == luaTableValue {
				if field.table == nil {
					return LuaObjectiveSnapshot{}, errors.New("objective nil private table")
				}
				targetIndex, isFound := tableIndexes[field.table]
				if !isFound {
					if len(tables) >= objectiveStateTableLimit {
						return LuaObjectiveSnapshot{}, errors.New("objective private table limit")
					}
					targetIndex = len(tables)
					tableIndexes[field.table] = targetIndex
					tables = append(tables, field.table)
				}
				data = LuaObjectiveScalar{Kind: "table", TableIndex: targetIndex}
			} else {
				data, err = snapshotObjectiveScalar(field)
				if err != nil {
					return LuaObjectiveSnapshot{}, fmt.Errorf("privateData: %w", err)
				}
			}
			fields = append(fields, LuaObjectiveField{Key: keyData, Data: data})
		}
		snapshot.PrivateTables = append(snapshot.PrivateTables, LuaObjectiveTable{Fields: fields})
	}
	err := ValidateLuaObjectiveSnapshot(snapshot)
	if err != nil {
		return LuaObjectiveSnapshot{}, fmt.Errorf("privateSnapshot: %w", err)
	}
	return snapshot, nil
}

func snapshotObjectiveScalar(field luaValue) (LuaObjectiveScalar, error) {
	switch field.kind {
	case luaNil:
		return LuaObjectiveScalar{Kind: "nil"}, nil
	case luaBoolean:
		return LuaObjectiveScalar{Kind: "boolean", IsTrue: field.isTrue}, nil
	case luaNumber:
		if math.IsNaN(field.number) || math.IsInf(field.number, 0) {
			return LuaObjectiveScalar{}, errors.New("objective nonfinite private number")
		}
		return LuaObjectiveScalar{Kind: "number", Number: field.number}, nil
	case luaString:
		if len(field.text) > objectiveStateTextLimit {
			return LuaObjectiveScalar{}, errors.New("objective private text limit")
		}
		return LuaObjectiveScalar{Kind: "string", Text: field.text}, nil
	default:
		return LuaObjectiveScalar{}, fmt.Errorf("objective unsupported private kind: %d", field.kind)
	}
}

// RestoredState builds a candidate, leaving the live runtime unchanged on any
// decode, identity, allocation, or authored initialization failure.
func (e *LuaObjectiveRuntime) RestoredState(snapshot LuaObjectiveSnapshot) (*LuaObjectiveRuntime, error) {
	if e == nil || e.compiler == nil {
		return nil, errors.New("objective runtime unavailable")
	}
	if snapshot.ObjectiveID != e.input.ObjectiveID ||
		snapshot.ChunkID != e.input.Root.ChunkID || snapshot.SHA256 != e.input.Root.SHA256 {
		return nil, errors.New("objective private identity mismatch")
	}
	err := ValidateLuaObjectiveSnapshot(snapshot)
	if err != nil {
		return nil, fmt.Errorf("restoreSnapshot: %w", err)
	}
	candidate, initializer, err := NewLuaObjectiveRuntime(e.input)
	if err != nil {
		return nil, fmt.Errorf("restoreRuntime: %w", err)
	}
	// Initialization establishes closure/upvalue identities; its UI emissions
	// are superseded by the separately restored ObjectiveState.
	_ = initializer
	err = candidate.restorePrivateTables(snapshot)
	if err != nil {
		return nil, fmt.Errorf("restorePrivate: %w", err)
	}
	return candidate, nil
}

func (e *LuaObjectiveRuntime) restorePrivateTables(snapshot LuaObjectiveSnapshot) error {
	if snapshot.MemoryBudget < 0 || snapshot.MemoryBudget > e.compiler.memoryBudget {
		return errors.New("objective private memory budget invalid")
	}
	if len(snapshot.PrivateTables) > objectiveStateTableLimit {
		return errors.New("objective private table limit")
	}
	if (len(snapshot.PrivateTables) == 0) != (e.compiler.privateTable == nil) {
		return errors.New("objective private root mismatch")
	}
	tables := make([]*luaTable, len(snapshot.PrivateTables))
	for index := range tables {
		tables[index] = newLuaTable(nil)
	}
	if len(tables) != 0 {
		// Init callbacks may capture the private root as an upvalue. Keep that
		// root's identity while replacing its fields only in this candidate.
		tables[0] = e.compiler.privateTable
		tables[0].fields = make(map[luaKey]luaValue)
	}
	fieldCount := 0
	for tableIndex, table := range snapshot.PrivateTables {
		for fieldIndex, field := range table.Fields {
			fieldCount++
			if fieldCount > objectiveStateFieldLimit {
				return errors.New("objective private field limit")
			}
			keyData, err := restoreObjectiveScalar(field.Key)
			if err != nil {
				return fmt.Errorf("privateKey[%d:%d]: %w", tableIndex, fieldIndex, err)
			}
			if keyData.kind != luaBoolean && keyData.kind != luaNumber && keyData.kind != luaString {
				return errors.New("objective private key invalid")
			}
			key := luaKey{kind: keyData.kind, number: keyData.number, text: keyData.text, isTrue: keyData.isTrue}
			if tables[tableIndex].has(key) {
				return errors.New("objective duplicate private key")
			}
			data, err := restoreObjectiveData(field.Data, tables)
			if err != nil {
				return fmt.Errorf("privateData[%d:%d]: %w", tableIndex, fieldIndex, err)
			}
			tables[tableIndex].set(key, data)
		}
	}
	e.compiler.memoryBudget = snapshot.MemoryBudget
	return nil
}

func restoreObjectiveData(data LuaObjectiveScalar, tables []*luaTable) (luaValue, error) {
	if data.Kind == "table" {
		if data.TableIndex < 0 || data.TableIndex >= len(tables) {
			return luaValue{}, errors.New("objective private table reference invalid")
		}
		return tableLuaValue(tables[data.TableIndex]), nil
	}
	field, err := restoreObjectiveScalar(data)
	if err != nil {
		return luaValue{}, fmt.Errorf("dataScalar: %w", err)
	}
	return field, nil
}

func restoreObjectiveScalar(data LuaObjectiveScalar) (luaValue, error) {
	switch data.Kind {
	case "nil":
		return luaValue{kind: luaNil}, nil
	case "boolean":
		return luaValue{kind: luaBoolean, isTrue: data.IsTrue}, nil
	case "number":
		if math.IsNaN(data.Number) || math.IsInf(data.Number, 0) {
			return luaValue{}, errors.New("objective nonfinite private number")
		}
		return numberLuaValue(data.Number), nil
	case "string":
		if len(data.Text) > objectiveStateTextLimit {
			return luaValue{}, errors.New("objective private text limit")
		}
		return stringLuaValue(data.Text), nil
	default:
		return luaValue{}, fmt.Errorf("objective private kind invalid: %s", data.Kind)
	}
}

func ValidateLuaObjectiveSnapshot(snapshot LuaObjectiveSnapshot) error {
	if snapshot.ObjectiveID == 0 || snapshot.ChunkID <= 0 || len(snapshot.SHA256) != 64 {
		return errors.New("objective private identity invalid")
	}
	if snapshot.MemoryBudget < 0 || snapshot.MemoryBudget > defaultLuaMemoryBudget {
		return errors.New("objective private memory budget invalid")
	}
	if len(snapshot.PrivateTables) > objectiveStateTableLimit {
		return errors.New("objective private table limit")
	}
	textCount := 0
	fieldCount := 0
	for _, table := range snapshot.PrivateTables {
		fieldCount += len(table.Fields)
		if fieldCount > objectiveStateFieldLimit {
			return errors.New("objective private field limit")
		}
		keys := make(map[luaKey]struct{}, len(table.Fields))
		for index, field := range table.Fields {
			keyData, err := restoreObjectiveScalar(field.Key)
			if err != nil {
				return fmt.Errorf("snapshotKey[%d]: %w", index, err)
			}
			if keyData.kind != luaBoolean && keyData.kind != luaNumber && keyData.kind != luaString {
				return errors.New("objective private key invalid")
			}
			key := luaKey{kind: keyData.kind, number: keyData.number, text: keyData.text, isTrue: keyData.isTrue}
			_, isDuplicate := keys[key]
			if isDuplicate {
				return errors.New("objective duplicate private key")
			}
			keys[key] = struct{}{}
			if field.Data.Kind == "table" {
				if field.Data.TableIndex < 0 || field.Data.TableIndex >= len(snapshot.PrivateTables) {
					return errors.New("objective private table reference invalid")
				}
			} else {
				data, dataErr := restoreObjectiveScalar(field.Data)
				if dataErr != nil {
					return fmt.Errorf("snapshotData[%d]: %w", index, dataErr)
				}
				if data.kind == luaTableValue {
					return errors.New("objective private scalar invalid")
				}
			}
			textCount += len(field.Key.Text) + len(field.Data.Text)
			if textCount > objectiveStateTextLimit {
				return errors.New("objective private aggregate text limit")
			}
		}
	}
	if len(snapshot.PrivateTables) == 0 {
		return nil
	}
	states := make([]uint8, len(snapshot.PrivateTables))
	err := validateObjectiveTableGraph(snapshot.PrivateTables, 0, states)
	if err != nil {
		return fmt.Errorf("snapshotGraph: %w", err)
	}
	for _, state := range states {
		if state == 0 {
			return errors.New("objective unreachable private table")
		}
	}
	return nil
}

func validateObjectiveTableGraph(tables []LuaObjectiveTable, tableIndex int, states []uint8) error {
	if states[tableIndex] == 1 {
		return errors.New("objective cyclic private table")
	}
	if states[tableIndex] == 2 {
		return nil
	}
	states[tableIndex] = 1
	for _, field := range tables[tableIndex].Fields {
		if field.Data.Kind != "table" {
			continue
		}
		err := validateObjectiveTableGraph(tables, field.Data.TableIndex, states)
		if err != nil {
			return fmt.Errorf("tableGraph[%d]: %w", tableIndex, err)
		}
	}
	states[tableIndex] = 2
	return nil
}

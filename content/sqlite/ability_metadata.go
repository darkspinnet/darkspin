package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
)

// AbilityMetadata preserves authored rank operands for the native eight-slot
// definition arrays at +208/+240 and cooldownType at +412. Nil means the
// static importer could not resolve an operand; it is not a fabricated zero.
// sub_A0A3C0/sub_A09F90 write a scalar only to slot zero; retain its authored
// form as well. Inheritance and runtime expressions are not executed.
type AbilityMetadata struct {
	LuaChunkID             int64
	TableName              string
	Cooldowns              [8]*float32
	CooldownVariances      [8]*float32
	CooldownScalar         *float32
	CooldownVarianceScalar *float32
	CooldownType           *uint32
}

func writeAbilityMetadata(ctx context.Context, transaction *sql.Tx, chunks []luaChunkImport) error {
	for _, chunk := range chunks {
		inspection, err := lua51.Inspect(chunk.bytecode)
		if err != nil {
			return fmt.Errorf("abilityInspect[%d]: %w", chunk.id, err)
		}
		globals := lua51.StaticGlobals(inspection)
		tableNames := make([]string, 0)
		for name, table := range globals {
			if table.Kind == lua51.StaticTable && strings.HasPrefix(name, "nAbility_") {
				tableNames = append(tableNames, name)
			}
		}
		sort.Strings(tableNames)
		for _, tableName := range tableNames {
			table := globals[tableName]
			metadata := AbilityMetadata{LuaChunkID: chunk.id, TableName: tableName}
			metadata.Cooldowns, metadata.CooldownScalar, err = abilityRankOperands(table.Fields["cooldown"])
			if err != nil {
				return fmt.Errorf("abilityCooldown[%d:%s]: %w", chunk.id, tableName, err)
			}
			metadata.CooldownVariances, metadata.CooldownVarianceScalar, err = abilityRankOperands(table.Fields["cooldownVariance"])
			if err != nil {
				return fmt.Errorf("abilityVariance[%d:%s]: %w", chunk.id, tableName, err)
			}
			mode, isModeAuthored := table.Fields["cooldownType"]
			if isModeAuthored && mode.Kind == lua51.StaticNumber {
				if math.IsNaN(mode.Number) || math.IsInf(mode.Number, 0) || mode.Number < 0 ||
					mode.Number > math.MaxUint32 || math.Trunc(mode.Number) != mode.Number {
					return fmt.Errorf("abilityCooldownType[%d:%s]: invalid", chunk.id, tableName)
				}
				modeNumber := uint32(mode.Number)
				metadata.CooldownType = &modeNumber
			}
			encoded, marshalErr := json.Marshal(metadata)
			if marshalErr != nil {
				return fmt.Errorf("abilityMetadataMarshal: %w", marshalErr)
			}
			result, insertErr := transaction.ExecContext(ctx, `INSERT INTO ability_metadata
				(lua_chunk_id, table_name, decoded_metadata) VALUES (?, ?, ?)`, chunk.id, tableName, string(encoded))
			if insertErr != nil {
				return fmt.Errorf("abilityMetadataInsert: %w", insertErr)
			}
			count, countErr := result.RowsAffected()
			if countErr != nil {
				return fmt.Errorf("abilityMetadataCount: %w", countErr)
			}
			if count != 1 {
				return fmt.Errorf("abilityMetadataCount: got %d", count)
			}
		}
	}
	return nil
}

func abilityRankOperands(field lua51.StaticValue) ([8]*float32, *float32, error) {
	operands := [8]*float32{}
	if field.Kind == lua51.StaticNumber {
		operand := float32(field.Number)
		if math.IsNaN(float64(operand)) || math.IsInf(float64(operand), 0) {
			return operands, nil, errors.New("nonfinite scalar operand")
		}
		operands[0] = &operand
		return operands, &operand, nil
	}
	if field.Kind != lua51.StaticTable {
		return operands, nil, nil
	}
	if len(field.Entries) > len(operands) {
		return operands, nil, fmt.Errorf("rankCount: %d", len(field.Entries))
	}
	for index, entry := range field.Entries {
		if entry.Kind != lua51.StaticNumber {
			continue
		}
		operand := float32(entry.Number)
		if math.IsNaN(float64(operand)) || math.IsInf(float64(operand), 0) {
			return operands, nil, fmt.Errorf("rankOperand[%d]: nonfinite", index+1)
		}
		operands[index] = &operand
	}
	return operands, nil, nil
}

func (e *Store) AbilityMetadataEntries(ctx context.Context) ([]AbilityMetadata, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("ability metadata store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT decoded_metadata FROM ability_metadata ORDER BY lua_chunk_id, table_name`)
	if err != nil {
		return nil, fmt.Errorf("abilityMetadataQuery: %w", err)
	}
	entries := make([]AbilityMetadata, 0)
	for rows.Next() {
		var encoded string
		err = rows.Scan(&encoded)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("abilityMetadataScan: %w", errors.Join(err, closeErr))
		}
		var metadata AbilityMetadata
		err = json.Unmarshal([]byte(encoded), &metadata)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("abilityMetadataDecode: %w", errors.Join(err, closeErr))
		}
		entries = append(entries, metadata)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("abilityMetadataRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("abilityMetadataClose: %w", closeErr)
	}
	return entries, nil
}

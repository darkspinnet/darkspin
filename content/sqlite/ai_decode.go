package sqlite

import (
	"encoding/binary"
	"fmt"
)

func decodeAIDefinition(payload []byte) (AIDefinition, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 640)
	if err != nil {
		return AIDefinition{}, fmt.Errorf("definitionHeader: %w", err)
	}
	count := binary.LittleEndian.Uint32(payload[base+4:])
	start, err := cursor.reserve(count, 28)
	if err != nil {
		return AIDefinition{}, fmt.Errorf("nodeArray: %w", err)
	}
	definition := AIDefinition{Nodes: make([]AINode, 0, int(count)),
		AggroType:            binary.LittleEndian.Uint32(payload[120:]),
		PreAggroIdle:         fixedAssetString(payload, 124, 80),
		PreAggroIdle2:        fixedAssetString(payload, 204, 80),
		CombatIdle:           fixedAssetString(payload, 284, 80),
		PassiveIdle:          fixedAssetString(payload, 368, 80),
		CombatIdle2:          fixedAssetString(payload, 448, 80),
		TargetTooFar:         fixedAssetString(payload, 548, 80),
		CombatIdleCooldown:   binary.LittleEndian.Uint32(payload[364:]),
		CombatIdle2Cooldown:  binary.LittleEndian.Uint32(payload[544:]),
		TargetTooFarCooldown: binary.LittleEndian.Uint32(payload[628:]),
		IsFaceTarget:         payload[632] != 0, IsAlwaysRunAI: payload[633] != 0,
		IsRandomizeCooldowns: payload[634] != 0, UseSecondaryStart: readFloat32(payload, 636)}
	for index := range int(count) {
		node, nodeErr := decodeAINode(&cursor, start+index*28)
		if nodeErr != nil {
			return AIDefinition{}, fmt.Errorf("nodeDecode[%d]: %w", index, nodeErr)
		}
		definition.Nodes = append(definition.Nodes, node)
	}
	references := []**string{&definition.DeathAbility, &definition.DeathCondition,
		&definition.FirstAggroAbility, &definition.FirstAggroAbility2,
		&definition.FirstAlertAbility, &definition.SubsequentAggroAbility,
		&definition.PassiveAbility, &definition.CombatIdle2Condition}
	offsets := []int{20, 36, 52, 68, 84, 100, 116, 540}
	for index, offset := range offsets {
		reference, referenceErr := cursor.reference(offset)
		if referenceErr != nil {
			return AIDefinition{}, fmt.Errorf("definitionReference[%d]: %w", offset, referenceErr)
		}
		*references[index] = reference
	}
	err = cursor.finish()
	if err != nil {
		return AIDefinition{}, fmt.Errorf("definitionTail: %w", err)
	}
	return definition, nil
}

func decodeAINode(cursor *assetCursor, base int) (AINode, error) {
	phase, err := cursor.reference(base)
	if err != nil {
		return AINode{}, fmt.Errorf("nodePhase: %w", err)
	}
	condition, err := cursor.reference(base + 4)
	if err != nil {
		return AINode{}, fmt.Errorf("nodeCondition: %w", err)
	}
	count := binary.LittleEndian.Uint32(cursor.payload[base+24:])
	start, err := cursor.reserve(count, 4)
	if err != nil {
		return AINode{}, fmt.Errorf("nodeOutputs: %w", err)
	}
	node := AINode{Phase: phase, Condition: condition, Outputs: make([]uint32, 0, int(count))}
	for index := range int(count) {
		node.Outputs = append(node.Outputs, binary.LittleEndian.Uint32(cursor.payload[start+index*4:]))
	}
	return node, nil
}

func decodeAIPhase(payload []byte) (AIPhase, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 16)
	if err != nil {
		return AIPhase{}, fmt.Errorf("phaseHeader: %w", err)
	}
	count := binary.LittleEndian.Uint32(payload[base+4:])
	start, err := cursor.reserve(count, 52)
	if err != nil {
		return AIPhase{}, fmt.Errorf("gambitArray: %w", err)
	}
	phase := AIPhase{PhaseType: binary.LittleEndian.Uint32(payload[8:]),
		IsStartNode: payload[12] != 0, Gambits: make([]AIGambit, 0, int(count))}
	for index := range int(count) {
		gambit, gambitErr := decodeAIGambit(&cursor, start+index*52)
		if gambitErr != nil {
			return AIPhase{}, fmt.Errorf("gambitDecode[%d]: %w", index, gambitErr)
		}
		phase.Gambits = append(phase.Gambits, gambit)
	}
	err = cursor.finish()
	if err != nil {
		return AIPhase{}, fmt.Errorf("phaseTail: %w", err)
	}
	return phase, nil
}

func decodeAIGambit(cursor *assetCursor, base int) (AIGambit, error) {
	condition, err := cursor.reference(base + 12)
	if err != nil {
		return AIGambit{}, fmt.Errorf("gambitCondition: %w", err)
	}
	conditionProps, err := decodeAIProperties(cursor, base+16)
	if err != nil {
		return AIGambit{}, fmt.Errorf("gambitConditionProps: %w", err)
	}
	ability, err := cursor.reference(base + 36)
	if err != nil {
		return AIGambit{}, fmt.Errorf("gambitAbility: %w", err)
	}
	abilityProps, err := decodeAIProperties(cursor, base+40)
	if err != nil {
		return AIGambit{}, fmt.Errorf("gambitAbilityProps: %w", err)
	}
	return AIGambit{Condition: condition, ConditionProps: conditionProps,
		Ability: ability, AbilityProps: abilityProps, IsRandomizeCooldown: cursor.payload[base+48] != 0}, nil
}

func decodeAICondition(payload []byte) (AICondition, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 36)
	if err != nil {
		return AICondition{}, fmt.Errorf("conditionHeader: %w", err)
	}
	reference, err := cursor.reference(base + 12)
	if err != nil {
		return AICondition{}, fmt.Errorf("conditionReference: %w", err)
	}
	properties, err := decodeAIProperties(&cursor, base+16)
	if err != nil {
		return AICondition{}, fmt.Errorf("conditionProps: %w", err)
	}
	err = cursor.finish()
	if err != nil {
		return AICondition{}, fmt.Errorf("conditionTail: %w", err)
	}
	return AICondition{Condition: reference, Properties: properties,
		IsActivateOnce: payload[24] != 0, IsCheckOnSequenceEnd: payload[25] != 0,
		ActivateTime: readFloat32(payload, 28), CheckTimeInterval: readFloat32(payload, 32)}, nil
}

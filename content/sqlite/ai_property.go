package sqlite

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var aiFloatPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)

// decodeAIProperties rebuilds sub_A3FF80's runtime variants from authored
// text. Bytes at +168 are saved runtime state and are deliberately ignored.
func decodeAIProperties(cursor *assetCursor, base int) ([]AIProperty, error) {
	count := binary.LittleEndian.Uint32(cursor.payload[base+4:])
	start, err := cursor.reserve(count, 188)
	if err != nil {
		return nil, fmt.Errorf("propertyArray: %w", err)
	}
	properties := make([]AIProperty, 0, int(count))
	for index := range int(count) {
		offset := start + index*188
		property := AIProperty{Key: binary.LittleEndian.Uint32(cursor.payload[offset:]),
			Name: fixedAssetString(cursor.payload, offset+4, 80),
			Type: binary.LittleEndian.Uint32(cursor.payload[offset+84:]),
			Text: fixedAssetString(cursor.payload, offset+88, 80)}
		runtime, convertErr := convertAIProperty(property.Type, property.Text)
		if convertErr != nil {
			return nil, fmt.Errorf("propertyConvert[%d]: %w", index, convertErr)
		}
		property.Runtime = runtime
		properties = append(properties, property)
	}
	return properties, nil
}

func convertAIProperty(propertyType uint32, text string) (AIPropertyRuntime, error) {
	switch propertyType {
	case 0x68fe5f59:
		return AIPropertyRuntime{Kind: "bool", IsTrue: text == "true"}, nil
	case 0x1f886eb0:
		return AIPropertyRuntime{Kind: "int", Integer: int32(aiDecimalBits(text))}, nil
	case 0x4edcd7a9:
		number := aiFloatPattern.FindString(strings.TrimLeft(text, " \t\r\n\v\f"))
		if number == "" {
			return AIPropertyRuntime{Kind: "float"}, nil
		}
		parsed, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return AIPropertyRuntime{}, fmt.Errorf("propertyFloat: %w", err)
		}
		return AIPropertyRuntime{Kind: "float", Float: float32(parsed)}, nil
	case 0xe48967a3, 0x2e10aaae:
		bits := aiDecimalBits(text)
		if strings.HasPrefix(text, "0x") {
			hexBits, isConverted := aiHexBits(text[2:])
			if isConverted {
				bits = hexBits
			}
		}
		return AIPropertyRuntime{Kind: "uint32", Unsigned: bits}, nil
	case 0xf6c8069d:
		return AIPropertyRuntime{Kind: "string", Text: text}, nil
	default:
		return AIPropertyRuntime{Kind: "number"}, nil
	}
}

func aiDecimalBits(text string) uint32 {
	text = strings.TrimLeft(text, " \t\r\n\v\f")
	isNegative := strings.HasPrefix(text, "-")
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	bits := uint32(0)
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			break
		}
		bits = bits*10 + uint32(text[index]-'0')
	}
	if isNegative {
		bits = 0 - bits
	}
	return bits
}

func aiHexBits(text string) (uint32, bool) {
	text = strings.TrimLeft(text, " \t\r\n\v\f")
	isNegative := strings.HasPrefix(text, "-")
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		text = text[2:]
	}
	bits := uint32(0)
	isConverted := false
	for index := 0; index < len(text); index++ {
		var digit uint32
		switch {
		case text[index] >= '0' && text[index] <= '9':
			digit = uint32(text[index] - '0')
		case text[index] >= 'a' && text[index] <= 'f':
			digit = uint32(text[index]-'a') + 10
		case text[index] >= 'A' && text[index] <= 'F':
			digit = uint32(text[index]-'A') + 10
		default:
			if isNegative {
				bits = 0 - bits
			}
			return bits, isConverted
		}
		bits = bits*16 + digit
		isConverted = true
	}
	if isNegative {
		bits = 0 - bits
	}
	return bits, isConverted
}

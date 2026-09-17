package ds

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/darkspinnet/darkspin/content/render/prop"
)

var propertyTypes = map[uint16]string{
	prop.TypeUnknown: "UNKNOWN", prop.TypeBool: "BOOL", prop.TypeChar: "CHAR", prop.TypeWChar: "WCHAR",
	prop.TypeInt8: "INT8", prop.TypeUInt8: "UINT8", prop.TypeInt16: "INT16", prop.TypeUInt16: "UINT16",
	prop.TypeInt32: "INT32", prop.TypeUInt32: "UINT32", prop.TypeInt64: "INT64", prop.TypeUInt64: "UINT64",
	prop.TypeFloat: "FLOAT", prop.TypeDouble: "DOUBLE", prop.TypeString8: "STRING8", prop.TypeString16: "STRING16",
	prop.TypeKey: "KEY", prop.TypeText: "TEXT", prop.TypeVector2: "VECTOR2", prop.TypeVector3: "VECTOR3",
	prop.TypeColorRGB: "COLORRGB", prop.TypeVector4: "VECTOR4", prop.TypeColorRGBA: "COLORRGBA",
	prop.TypeTransform: "TRANSFORM", prop.TypeBBox: "BBOX",
}

func propertyTypeName(propertyType uint16) string {
	name := propertyTypes[propertyType]
	if name != "" {
		return name
	}
	return fmt.Sprintf("0x%04X", propertyType)
}

func propertyTypeCode(name string) (uint16, bool) {
	for propertyType, knownName := range propertyTypes {
		if name == knownName {
			return propertyType, true
		}
	}
	return 0, false
}

func formatPropertyItem(propertyType uint16, isArray bool, item []byte, names map[uint32]string) (string, error) {
	switch propertyType {
	case prop.TypeBool:
		if len(item) != 1 || item[0] > 1 {
			return "", fmt.Errorf("bool: %X", item)
		}
		if item[0] == 1 {
			return strconv.Quote("TRUE"), nil
		}
		return strconv.Quote("FALSE"), nil
	case prop.TypeInt8:
		return strconv.FormatInt(int64(int8(item[0])), 10), nil
	case prop.TypeUInt8, prop.TypeChar:
		return strconv.FormatUint(uint64(item[0]), 10), nil
	case prop.TypeInt16:
		return strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(item))), 10), nil
	case prop.TypeUInt16, prop.TypeWChar:
		return strconv.FormatUint(uint64(binary.BigEndian.Uint16(item)), 10), nil
	case prop.TypeInt32, prop.TypeUInt32:
		return fmt.Sprintf("0x%08X", binary.BigEndian.Uint32(item)), nil
	case prop.TypeInt64:
		return strconv.FormatInt(int64(binary.BigEndian.Uint64(item)), 10), nil
	case prop.TypeUInt64:
		return fmt.Sprintf("0x%016X", binary.BigEndian.Uint64(item)), nil
	case prop.TypeFloat:
		return strconv.FormatFloat(float64(math.Float32frombits(binary.BigEndian.Uint32(item))), 'g', -1, 32), nil
	case prop.TypeDouble:
		return strconv.FormatFloat(math.Float64frombits(binary.BigEndian.Uint64(item)), 'g', -1, 64), nil
	case prop.TypeString8:
		return strconv.Quote(string(item[4:])), nil
	case prop.TypeString16:
		return strconv.Quote(decodeUTF16(item[4:])), nil
	case prop.TypeKey:
		if len(item) < 12 {
			return "", fmt.Errorf("keySize: %d", len(item))
		}
		instanceID := binary.LittleEndian.Uint32(item[0:4])
		typeID := binary.LittleEndian.Uint32(item[4:8])
		groupID := binary.LittleEndian.Uint32(item[8:12])
		keyName := names[instanceID]
		if groupID == 0 && typeID == 0 && strings.HasPrefix(keyName, "@") {
			return strconv.Quote(keyName), nil
		}
		if groupID == 0 && typeID == 0 && keyName != "" && hashResourceName(keyName) == instanceID {
			return strconv.Quote(keyName), nil
		}
		return strconv.Quote(fmt.Sprintf("0x%08X!0x%08X.0x%08X", groupID, instanceID, typeID)), nil
	case prop.TypeVector2, prop.TypeVector3, prop.TypeColorRGB, prop.TypeVector4, prop.TypeColorRGBA, prop.TypeBBox:
		coordinateBytes := item
		if !isArray {
			switch propertyType {
			case prop.TypeVector2:
				coordinateBytes = item[:8]
			case prop.TypeVector3, prop.TypeColorRGB:
				coordinateBytes = item[:12]
			}
		}
		parts := make([]string, 0, len(coordinateBytes)/4)
		for offset := 0; offset < len(coordinateBytes); offset += 4 {
			coordinate := math.Float32frombits(binary.BigEndian.Uint32(coordinateBytes[offset:]))
			parts = append(parts, strconv.FormatFloat(float64(coordinate), 'g', -1, 32))
		}
		return strconv.Quote(strings.Join(parts, " ")), nil
	default:
		return "0x" + strings.ToUpper(hex.EncodeToString(item)), nil
	}
}

func parsePropertyItem(propertyType uint16, isArray bool, token string) ([]byte, error) {
	switch propertyType {
	case prop.TypeBool:
		if token == "TRUE" {
			return []byte{1}, nil
		}
		if token == "FALSE" {
			return []byte{0}, nil
		}
		return nil, fmt.Errorf("bool: %q", token)
	case prop.TypeInt8:
		return parseSignedProperty(token, 8)
	case prop.TypeUInt8, prop.TypeChar:
		return parseUnsignedProperty(token, 8)
	case prop.TypeInt16:
		return parseSignedProperty(token, 16)
	case prop.TypeUInt16, prop.TypeWChar:
		return parseUnsignedProperty(token, 16)
	case prop.TypeInt32:
		return parseSignedProperty(token, 32)
	case prop.TypeUInt32:
		return parseUnsignedProperty(token, 32)
	case prop.TypeInt64:
		return parseSignedProperty(token, 64)
	case prop.TypeUInt64:
		return parseUnsignedProperty(token, 64)
	case prop.TypeFloat:
		return parsePropertyFloat(token, 32)
	case prop.TypeDouble:
		return parsePropertyFloat(token, 64)
	case prop.TypeString8:
		item := make([]byte, 4+len(token))
		binary.BigEndian.PutUint32(item, uint32(len(token)))
		copy(item[4:], token)
		return item, nil
	case prop.TypeString16:
		characters := utf16.Encode([]rune(token))
		item := make([]byte, 4+len(characters)*2)
		binary.BigEndian.PutUint32(item, uint32(len(characters)))
		for characterIndex, character := range characters {
			binary.LittleEndian.PutUint16(item[4+characterIndex*2:], character)
		}
		return item, nil
	case prop.TypeKey:
		return parsePropertyKey(token, isArray)
	case prop.TypeVector2, prop.TypeVector3, prop.TypeColorRGB, prop.TypeVector4, prop.TypeColorRGBA, prop.TypeBBox:
		item, err := parsePropertyFloats(token)
		if err != nil {
			return nil, fmt.Errorf("floats: %w", err)
		}
		if !isArray {
			switch propertyType {
			case prop.TypeVector2:
				item = append(item, make([]byte, 8)...)
			case prop.TypeVector3, prop.TypeColorRGB:
				item = append(item, make([]byte, 4)...)
			}
		}
		return item, nil
	default:
		if !strings.HasPrefix(token, "0x") {
			return nil, fmt.Errorf("hex: %q", token)
		}
		item, err := hex.DecodeString(token[2:])
		if err != nil {
			return nil, fmt.Errorf("hex: %w", err)
		}
		return item, nil
	}
}

func parsePropertyFloat(token string, bitSize int) ([]byte, error) {
	coordinate, err := strconv.ParseFloat(token, bitSize)
	if err != nil {
		return nil, fmt.Errorf("float: %w", err)
	}
	item := make([]byte, bitSize/8)
	if bitSize == 32 {
		binary.BigEndian.PutUint32(item, math.Float32bits(float32(coordinate)))
	} else {
		binary.BigEndian.PutUint64(item, math.Float64bits(coordinate))
	}
	return item, nil
}

func parseSignedProperty(token string, bitSize int) ([]byte, error) {
	integer, err := strconv.ParseInt(token, 0, bitSize)
	if err != nil && strings.HasPrefix(token, "0x") {
		unsignedInteger, unsignedErr := strconv.ParseUint(token, 0, bitSize)
		if unsignedErr == nil {
			integer = int64(unsignedInteger)
			err = nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("integer: %w", err)
	}
	return encodePropertyInteger(uint64(integer), bitSize), nil
}

func parseUnsignedProperty(token string, bitSize int) ([]byte, error) {
	integer, err := strconv.ParseUint(token, 0, bitSize)
	if err != nil {
		return nil, fmt.Errorf("integer: %w", err)
	}
	return encodePropertyInteger(integer, bitSize), nil
}

func encodePropertyInteger(integer uint64, bitSize int) []byte {
	item := make([]byte, bitSize/8)
	switch bitSize {
	case 8:
		item[0] = byte(integer)
	case 16:
		binary.BigEndian.PutUint16(item, uint16(integer))
	case 32:
		binary.BigEndian.PutUint32(item, uint32(integer))
	case 64:
		binary.BigEndian.PutUint64(item, integer)
	}
	return item
}

func parsePropertyKey(token string, isArray bool) ([]byte, error) {
	if strings.HasPrefix(token, "@") {
		instanceID, err := linkedResourceInstance(token)
		if err != nil {
			return nil, fmt.Errorf("keyLink: %w", err)
		}
		itemSize := 12
		if !isArray {
			itemSize = 16
		}
		item := make([]byte, itemSize)
		binary.LittleEndian.PutUint32(item[0:4], instanceID)
		return item, nil
	}
	separator := strings.Index(token, "!")
	typeSeparator := strings.LastIndex(token, ".")
	if separator < 0 && typeSeparator < 0 && token != "" {
		itemSize := 12
		if !isArray {
			itemSize = 16
		}
		item := make([]byte, itemSize)
		binary.LittleEndian.PutUint32(item[0:4], hashResourceName(token))
		return item, nil
	}
	if separator < 0 || typeSeparator <= separator {
		return nil, fmt.Errorf("key: %q", token)
	}
	groupID, err := parseUint(token[:separator], 32)
	if err != nil {
		return nil, fmt.Errorf("keyGroup: %w", err)
	}
	instanceID, err := parseUint(token[separator+1:typeSeparator], 32)
	if err != nil {
		return nil, fmt.Errorf("keyInstance: %w", err)
	}
	typeID, err := parseUint(token[typeSeparator+1:], 32)
	if err != nil {
		return nil, fmt.Errorf("keyType: %w", err)
	}
	itemSize := 12
	if !isArray {
		itemSize = 16
	}
	item := make([]byte, itemSize)
	binary.LittleEndian.PutUint32(item[0:4], uint32(instanceID))
	binary.LittleEndian.PutUint32(item[4:8], uint32(typeID))
	binary.LittleEndian.PutUint32(item[8:12], uint32(groupID))
	return item, nil
}

func linkedResourceInstance(token string) (uint32, error) {
	linkedPath := strings.TrimPrefix(token, "@")
	baseName := strings.TrimSuffix(path.Base(linkedPath), path.Ext(linkedPath))
	if len(baseName) == 8 {
		instanceID, err := strconv.ParseUint(baseName, 16, 32)
		if err != nil {
			return 0, fmt.Errorf("instance: %w", err)
		}
		return uint32(instanceID), nil
	}
	separator := strings.LastIndex(baseName, "_")
	if separator < 0 || len(baseName)-separator-1 != 16 {
		return 0, fmt.Errorf("identity: %q", token)
	}
	instanceID, err := strconv.ParseUint(baseName[separator+1:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("instance: %w", err)
	}
	if instanceID > uint64(^uint32(0)) {
		return 0, fmt.Errorf("instance: 0x%016X exceeds uint32", instanceID)
	}
	return uint32(instanceID), nil
}

func parsePropertyFloats(token string) ([]byte, error) {
	parts := strings.Fields(token)
	item := make([]byte, len(parts)*4)
	for partIndex, part := range parts {
		coordinate, err := strconv.ParseFloat(part, 32)
		if err != nil {
			return nil, fmt.Errorf("coordinate[%d]: %w", partIndex, err)
		}
		binary.BigEndian.PutUint32(item[partIndex*4:], math.Float32bits(float32(coordinate)))
	}
	return item, nil
}

func decodeUTF16(payload []byte) string {
	characters := make([]uint16, len(payload)/2)
	for characterIndex := range characters {
		characters[characterIndex] = binary.LittleEndian.Uint16(payload[characterIndex*2:])
	}
	return string(utf16.Decode(characters))
}

func propertyItemComment(propertyType uint16, item []byte) string {
	if propertyType != prop.TypeInt32 || len(item) != 4 {
		return ""
	}
	switch binary.BigEndian.Uint32(item) {
	case 0x2399BE55:
		return " // bld"
	case 0x2B978C46:
		return " // crt"
	case 0x24682294:
		return " // vcl"
	case 0x476A98C7:
		return " // ufo"
	default:
		return ""
	}
}

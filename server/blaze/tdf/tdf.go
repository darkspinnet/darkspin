// Package tdf implements Blaze's tagged data format.
package tdf

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/darkspinnet/darkspin/server/databuffer"
)

// Type is the wire type stored in the high byte of a TDF field header.
type Type uint8

const (
	Integer Type = iota
	String
	Binary
	Struct
	List
	Map
	Union
	Variable
	ObjectType
	ObjectID
	Float
	TimeValue
	Invalid Type = 0xff
)

// Header identifies a named TDF value.
type Header struct {
	Label string
	Type  Type
}

// Field is a labeled TDF value.
type Field struct {
	Label string
	Value Value
}

// MapEntry is one typed key/value pair.
type MapEntry struct {
	Key   Value
	Value Value
}

// Value represents any TDF wire value. The member selected by Type is used.
type Value struct {
	Type           Type
	Integer        uint64
	String         string
	Binary         []byte
	Fields         []Field
	ListType       Type
	Items          []Value
	IsStub         bool
	KeyType        Type
	ValueType      Type
	Entries        []MapEntry
	ActiveMember   uint8
	VariableTypeID uint64
	VariableField  *Field
	ObjectType     [2]uint64
	ObjectID       [3]uint64
}

// CompressLabel packs up to four ASCII label characters into Blaze's 24-bit
// representation.
func CompressLabel(label string) uint32 {
	var packed uint32
	length := len(label)
	if length > 4 {
		length = 4
	}
	for index := 0; index < length; index++ {
		value := uint32(0x20 | (label[index] & 0x1f))
		packed |= value << ((3 - index) * 6)
	}
	return bits.ReverseBytes32(packed) >> 8
}

// DecompressLabel expands a Blaze label to uppercase ASCII.
func DecompressLabel(label uint32) string {
	packed := bits.ReverseBytes32(label) >> 8
	result := make([]byte, 0, 4)
	for index := 0; index < 4; index++ {
		value := byte((packed >> ((3 - index) * 6)) & 0x3f)
		if value > 0 {
			result = append(result, 0x40|(value&0x1f))
		}
	}
	return string(result)
}

// WriteHeader writes a four-byte TDF header in the legacy host-wire layout.
func WriteHeader(buffer *databuffer.Buffer, header Header) {
	value := CompressLabel(header.Label) | uint32(header.Type)<<24
	buffer.WriteUint32LE(value)
}

// ReadHeader reads a TDF field header.
func ReadHeader(buffer *databuffer.Buffer) (Header, error) {
	value, err := buffer.ReadUint32LE()
	if err != nil {
		return Header{}, fmt.Errorf("headerRead: %w", err)
	}
	return Header{
		Label: DecompressLabel(value & 0x00ffffff),
		Type:  Type(value >> 24),
	}, nil
}

// Encode writes a complete sequence of labeled TDF fields.
func Encode(buffer *databuffer.Buffer, fields []Field) error {
	for _, field := range fields {
		WriteHeader(buffer, Header{Label: field.Label, Type: field.Value.Type})
		err := encodeValue(buffer, field.Value)
		if err != nil {
			return fmt.Errorf("fieldEncode[%q]: %w", field.Label, err)
		}
	}
	return nil
}

// Decode reads fields until the end of the buffer.
func Decode(buffer *databuffer.Buffer) ([]Field, error) {
	fields := make([]Field, 0)
	for index := 0; !buffer.EOF(); index++ {
		remaining := buffer.Size() - buffer.Position()
		if remaining < 4 {
			padding, err := buffer.ReadBytes(remaining)
			if err != nil {
				return nil, fmt.Errorf("paddingRead: %w", err)
			}
			if allZero(padding) {
				return fields, nil
			}
			return nil, fmt.Errorf("fieldDecode[%d]: trailing data", index)
		}
		field, err := readField(buffer)
		if err != nil {
			return nil, fmt.Errorf("fieldDecode[%d]: %w", index, err)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func allZero(contents []byte) bool {
	for _, value := range contents {
		if value != 0 {
			return false
		}
	}
	return true
}

func readField(buffer *databuffer.Buffer) (Field, error) {
	header, err := ReadHeader(buffer)
	if err != nil {
		return Field{}, fmt.Errorf("fieldHeader: %w", err)
	}
	value, err := decodeValue(buffer, header.Type)
	if err != nil {
		return Field{}, fmt.Errorf("valueDecode[%q]: %w", header.Label, err)
	}
	return Field{Label: header.Label, Value: value}, nil
}

func encodeValue(buffer *databuffer.Buffer, value Value) error {
	switch value.Type {
	case Integer, TimeValue:
		buffer.EncodeTDFInteger(value.Integer)
	case String:
		buffer.EncodeTDFInteger(uint64(len(value.String) + 1))
		buffer.WriteBytes([]byte(value.String))
		buffer.WriteBytes([]byte{0})
	case Binary:
		buffer.EncodeTDFInteger(uint64(len(value.Binary)))
		buffer.WriteBytes(value.Binary)
	case Struct:
		err := Encode(buffer, value.Fields)
		if err != nil {
			return fmt.Errorf("structEncode: %w", err)
		}
		buffer.WriteBytes([]byte{0})
	case List:
		if len(value.Items) > 255 {
			return errors.New("list contains more than 255 items")
		}
		buffer.WriteBytes([]byte{byte(value.ListType), byte(len(value.Items))})
		if value.IsStub {
			buffer.WriteBytes([]byte{0x02})
		}
		for _, item := range value.Items {
			if item.Type != value.ListType {
				return fmt.Errorf("list item type %d does not match %d", item.Type, value.ListType)
			}
			err := encodeValue(buffer, item)
			if err != nil {
				return fmt.Errorf("listItemEncode: %w", err)
			}
		}
	case Map:
		if len(value.Entries) > 255 {
			return errors.New("map contains more than 255 entries")
		}
		buffer.WriteBytes([]byte{byte(value.KeyType), byte(value.ValueType), byte(len(value.Entries))})
		for _, entry := range value.Entries {
			if entry.Key.Type != value.KeyType || entry.Value.Type != value.ValueType {
				return errors.New("map entry type does not match map declaration")
			}
			err := encodeValue(buffer, entry.Key)
			if err != nil {
				return fmt.Errorf("mapKeyEncode: %w", err)
			}
			err = encodeValue(buffer, entry.Value)
			if err != nil {
				return fmt.Errorf("mapValueEncode: %w", err)
			}
		}
	case Union:
		buffer.WriteBytes([]byte{value.ActiveMember})
		if value.ActiveMember != 0x7f {
			err := Encode(buffer, value.Fields)
			if err != nil {
				return fmt.Errorf("unionEncode: %w", err)
			}
		}
	case Variable:
		if value.VariableField == nil {
			buffer.WriteBytes([]byte{0})
			break
		}
		buffer.WriteBytes([]byte{1})
		buffer.EncodeTDFInteger(value.VariableTypeID)
		err := Encode(buffer, []Field{*value.VariableField})
		if err != nil {
			return fmt.Errorf("variableEncode: %w", err)
		}
		buffer.WriteBytes([]byte{0})
	case ObjectType:
		buffer.EncodeTDFInteger(value.ObjectType[0])
		buffer.EncodeTDFInteger(value.ObjectType[1])
	case ObjectID:
		buffer.EncodeTDFInteger(value.ObjectID[0])
		buffer.EncodeTDFInteger(value.ObjectID[1])
		buffer.EncodeTDFInteger(value.ObjectID[2])
	default:
		return fmt.Errorf("unsupported TDF type %d", value.Type)
	}
	return nil
}

func decodeValue(buffer *databuffer.Buffer, valueType Type) (Value, error) {
	value := Value{Type: valueType}
	var err error
	switch valueType {
	case Integer, TimeValue:
		value.Integer, err = buffer.DecodeTDFInteger()
	case String:
		value.String, err = readString(buffer)
	case Binary:
		value.Binary, err = readBinary(buffer)
	case Struct:
		value.Fields, err = readStruct(buffer)
	case List:
		err = readList(buffer, &value)
	case Map:
		err = readMap(buffer, &value)
	case Union:
		err = readUnion(buffer, &value)
	case Variable:
		err = readVariable(buffer, &value)
	case ObjectType:
		value.ObjectType[0], err = buffer.DecodeTDFInteger()
		if err == nil {
			value.ObjectType[1], err = buffer.DecodeTDFInteger()
		}
	case ObjectID:
		for index := range value.ObjectID {
			value.ObjectID[index], err = buffer.DecodeTDFInteger()
			if err != nil {
				break
			}
		}
	default:
		err = fmt.Errorf("unsupported TDF type %d", valueType)
	}
	if err != nil {
		return Value{}, fmt.Errorf("typeDecode[%d]: %w", valueType, err)
	}
	return value, nil
}

func readString(buffer *databuffer.Buffer) (string, error) {
	length, err := buffer.DecodeTDFInteger()
	if err != nil {
		return "", fmt.Errorf("stringLength: %w", err)
	}
	if length == 0 {
		return "", errors.New("TDF string has zero encoded length")
	}
	contents, err := buffer.ReadBytes(int(length))
	if err != nil {
		return "", fmt.Errorf("stringPayload: %w", err)
	}
	if contents[len(contents)-1] != 0 {
		return "", errors.New("TDF string is not null terminated")
	}
	return string(contents[:len(contents)-1]), nil
}

func readBinary(buffer *databuffer.Buffer) ([]byte, error) {
	length, err := buffer.DecodeTDFInteger()
	if err != nil {
		return nil, fmt.Errorf("binaryLength: %w", err)
	}
	if length > uint64(buffer.Size()-buffer.Position()) {
		return nil, errors.New("TDF binary length exceeds packet")
	}
	contents, err := buffer.ReadBytes(int(length))
	if err != nil {
		return nil, fmt.Errorf("binaryPayload: %w", err)
	}
	return contents, nil
}

func readStruct(buffer *databuffer.Buffer) ([]Field, error) {
	fields := make([]Field, 0)
	for {
		next, err := buffer.PeekBytes(1)
		if err != nil {
			return nil, fmt.Errorf("structPeek: %w", err)
		}
		if next[0] == 0 {
			err = buffer.Skip(1)
			if err != nil {
				return nil, fmt.Errorf("structSkip: %w", err)
			}
			return fields, nil
		}
		field, err := readField(buffer)
		if err != nil {
			return nil, fmt.Errorf("structField: %w", err)
		}
		fields = append(fields, field)
	}
}

func readList(buffer *databuffer.Buffer, value *Value) error {
	header, err := buffer.ReadBytes(2)
	if err != nil {
		return fmt.Errorf("listHeader: %w", err)
	}
	value.ListType = Type(header[0])
	count := int(header[1])
	if value.ListType == Struct {
		next, peekErr := buffer.PeekBytes(1)
		if peekErr == nil && next[0] == 0x02 {
			value.IsStub = true
			err = buffer.Skip(1)
			if err != nil {
				return fmt.Errorf("listStub: %w", err)
			}
		}
	}
	value.Items = make([]Value, 0, count)
	for index := 0; index < count; index++ {
		item, decodeErr := decodeValue(buffer, value.ListType)
		if decodeErr != nil {
			return fmt.Errorf("listItem[%d]: %w", index, decodeErr)
		}
		value.Items = append(value.Items, item)
	}
	return nil
}

func readMap(buffer *databuffer.Buffer, value *Value) error {
	header, err := buffer.ReadBytes(3)
	if err != nil {
		return fmt.Errorf("mapHeader: %w", err)
	}
	value.KeyType = Type(header[0])
	value.ValueType = Type(header[1])
	count := int(header[2])
	value.Entries = make([]MapEntry, 0, count)
	for index := 0; index < count; index++ {
		key, decodeErr := decodeValue(buffer, value.KeyType)
		if decodeErr != nil {
			return fmt.Errorf("mapKey[%d]: %w", index, decodeErr)
		}
		entryValue, decodeErr := decodeValue(buffer, value.ValueType)
		if decodeErr != nil {
			return fmt.Errorf("mapValue[%d]: %w", index, decodeErr)
		}
		value.Entries = append(value.Entries, MapEntry{Key: key, Value: entryValue})
	}
	return nil
}

func readUnion(buffer *databuffer.Buffer, value *Value) error {
	active, err := buffer.ReadBytes(1)
	if err != nil {
		return fmt.Errorf("unionTag: %w", err)
	}
	value.ActiveMember = active[0]
	if value.ActiveMember == 0x7f {
		return nil
	}
	field, err := readField(buffer)
	if err != nil {
		return fmt.Errorf("unionField: %w", err)
	}
	value.Fields = []Field{field}
	return nil
}

func readVariable(buffer *databuffer.Buffer, value *Value) error {
	presence, err := buffer.ReadBytes(1)
	if err != nil {
		return fmt.Errorf("variablePresence: %w", err)
	}
	if presence[0] == 0 {
		return nil
	}
	if presence[0] != 1 {
		return fmt.Errorf("variable presence is %d", presence[0])
	}
	value.VariableTypeID, err = buffer.DecodeTDFInteger()
	if err != nil {
		return fmt.Errorf("variableTypeID: %w", err)
	}
	field, err := readField(buffer)
	if err != nil {
		return fmt.Errorf("variableField: %w", err)
	}
	terminator, err := buffer.ReadBytes(1)
	if err != nil {
		return fmt.Errorf("variableTerminator: %w", err)
	}
	if terminator[0] != 0 {
		return fmt.Errorf("variable terminator is %d", terminator[0])
	}
	value.VariableField = &field
	return nil
}

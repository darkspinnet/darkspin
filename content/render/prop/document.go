// Package prop decodes Game property-list resources into editable fields.
package prop

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	ResourceType         = 0x00B1B104
	AudioResourceType    = 0x02B9F662
	SubmixResourceType   = 0x02C9EFF2
	ModeResourceType     = 0x0497925E
	ChildrenResourceType = 0x03F51892

	TypeUnknown   = 0x0000
	TypeBool      = 0x0001
	TypeChar      = 0x0002
	TypeWChar     = 0x0003
	TypeInt8      = 0x0005
	TypeUInt8     = 0x0006
	TypeInt16     = 0x0007
	TypeUInt16    = 0x0008
	TypeInt32     = 0x0009
	TypeUInt32    = 0x000A
	TypeInt64     = 0x000B
	TypeUInt64    = 0x000C
	TypeFloat     = 0x000D
	TypeDouble    = 0x000E
	TypeString8   = 0x0012
	TypeString16  = 0x0013
	TypeKey       = 0x0020
	TypeText      = 0x0022
	TypeVector2   = 0x0030
	TypeVector3   = 0x0031
	TypeColorRGB  = 0x0032
	TypeVector4   = 0x0033
	TypeColorRGBA = 0x0034
	TypeTransform = 0x0038
	TypeBBox      = 0x0039
)

// IsResourceType reports whether a DBPF type uses property-list framing.
func IsResourceType(typeCode uint32) bool {
	return typeCode == ResourceType || typeCode == AudioResourceType || typeCode == SubmixResourceType ||
		typeCode == ModeResourceType || typeCode == ChildrenResourceType
}

// Document is one ordered property list.
type Document struct {
	Properties []Property
}

// Property retains the exact encoded bytes of every item. Semantic DSE
// formatting interprets known types without weakening reconstruction.
type Property struct {
	Name     string
	ID       uint32
	Type     uint16
	Flags    uint16
	IsArray  bool
	ItemSize uint32
	Items    [][]byte
}

// Decode strictly reads property-list framing and every format implemented by
// SporeModder-FX's property decoder.
func Decode(payload []byte) (*Document, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("countSize: got %d", len(payload))
	}
	propertyCount := binary.BigEndian.Uint32(payload[:4])
	if uint64(propertyCount) > uint64(len(payload))/8 {
		return nil, fmt.Errorf("propertyCount: %d exceeds payload", propertyCount)
	}
	offset := 4
	document := &Document{Properties: make([]Property, 0, propertyCount)}
	for ordinal := uint32(0); ordinal < propertyCount; ordinal++ {
		if offset+8 > len(payload) {
			return nil, fmt.Errorf("propertyHeader[%d]: offset %d exceeds %d", ordinal, offset, len(payload))
		}
		property := Property{
			ID:    binary.BigEndian.Uint32(payload[offset : offset+4]),
			Type:  binary.BigEndian.Uint16(payload[offset+4 : offset+6]),
			Flags: binary.BigEndian.Uint16(payload[offset+6 : offset+8]),
		}
		offset += 8
		itemCount := uint32(1)
		if property.Flags&0x30 != 0 {
			if property.Flags&0x40 != 0 {
				return nil, fmt.Errorf("propertyFlags[%d]: unsupported 0x%04X", ordinal, property.Flags)
			}
			if offset+8 > len(payload) {
				return nil, fmt.Errorf("arrayHeader[%d]: offset %d exceeds %d", ordinal, offset, len(payload))
			}
			property.IsArray = true
			itemCount = binary.BigEndian.Uint32(payload[offset : offset+4])
			property.ItemSize = binary.BigEndian.Uint32(payload[offset+4 : offset+8])
			offset += 8
		}
		property.Items = make([][]byte, itemCount)
		for itemIndex := range property.Items {
			itemSize, err := encodedItemSize(payload[offset:], property.Type, property.IsArray)
			if err != nil {
				return nil, fmt.Errorf("propertyItem[%d:%d]: %w", ordinal, itemIndex, err)
			}
			if offset+itemSize > len(payload) {
				return nil, fmt.Errorf("propertyRange[%d:%d]: need %d bytes", ordinal, itemIndex, itemSize)
			}
			property.Items[itemIndex] = append([]byte(nil), payload[offset:offset+itemSize]...)
			offset += itemSize
		}
		if property.IsArray {
			expectedSize, isFixed := fixedItemSize(property.Type)
			if isFixed && property.ItemSize != uint32(expectedSize) {
				return nil, fmt.Errorf("arrayItemSize[%d]: got %d, want %d", ordinal, property.ItemSize, expectedSize)
			}
		}
		document.Properties = append(document.Properties, property)
	}
	if offset != len(payload) {
		return nil, fmt.Errorf("trailingData: %d bytes", len(payload)-offset)
	}
	return document, nil
}

// Encode rebuilds every represented property without inferring hidden fields.
func Encode(document *Document) ([]byte, error) {
	if document == nil {
		return nil, errors.New("nil document")
	}
	if uint64(len(document.Properties)) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("propertyCount: %d exceeds uint32", len(document.Properties))
	}
	contents := &bytes.Buffer{}
	err := binary.Write(contents, binary.BigEndian, uint32(len(document.Properties)))
	if err != nil {
		return nil, fmt.Errorf("countWrite: %w", err)
	}
	for ordinal, property := range document.Properties {
		if !property.IsArray && len(property.Items) != 1 {
			return nil, fmt.Errorf("scalarCount[%d]: got %d, want 1", ordinal, len(property.Items))
		}
		fields := []any{property.ID, property.Type, property.Flags}
		if property.IsArray {
			if property.Flags&0x30 == 0 || property.Flags&0x40 != 0 {
				return nil, fmt.Errorf("arrayFlags[%d]: 0x%04X", ordinal, property.Flags)
			}
			fields = append(fields, uint32(len(property.Items)), property.ItemSize)
		}
		for _, field := range fields {
			err = binary.Write(contents, binary.BigEndian, field)
			if err != nil {
				return nil, fmt.Errorf("propertyWrite[%d]: %w", ordinal, err)
			}
		}
		for itemIndex, item := range property.Items {
			expectedSize, sizeErr := encodedItemSize(item, property.Type, property.IsArray)
			if sizeErr != nil {
				return nil, fmt.Errorf("itemSize[%d:%d]: %w", ordinal, itemIndex, sizeErr)
			}
			if len(item) != expectedSize {
				return nil, fmt.Errorf("itemSize[%d:%d]: got %d, want %d", ordinal, itemIndex, len(item), expectedSize)
			}
			_, err = contents.Write(item)
			if err != nil {
				return nil, fmt.Errorf("itemWrite[%d:%d]: %w", ordinal, itemIndex, err)
			}
		}
	}
	return contents.Bytes(), nil
}

func encodedItemSize(payload []byte, propertyType uint16, isArray bool) (int, error) {
	if propertyType == TypeString8 || propertyType == TypeString16 {
		if len(payload) < 4 {
			return 0, fmt.Errorf("stringLength: got %d bytes", len(payload))
		}
		characterCount := binary.BigEndian.Uint32(payload[:4])
		characterSize := uint64(1)
		if propertyType == TypeString16 {
			characterSize = 2
		}
		itemSize := uint64(4) + uint64(characterCount)*characterSize
		if itemSize > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("stringSize: %d exceeds int", itemSize)
		}
		return int(itemSize), nil
	}
	itemSize, isFound := fixedItemSize(propertyType)
	if !isFound {
		return 0, fmt.Errorf("propertyType: unsupported 0x%04X", propertyType)
	}
	if !isArray {
		switch propertyType {
		case TypeKey, TypeVector3, TypeColorRGB:
			itemSize += 4
		case TypeVector2:
			itemSize += 8
		}
	}
	return itemSize, nil
}

func fixedItemSize(propertyType uint16) (int, bool) {
	switch propertyType {
	case TypeBool, TypeChar, TypeInt8, TypeUInt8:
		return 1, true
	case TypeWChar, TypeInt16, TypeUInt16:
		return 2, true
	case TypeInt32, TypeUInt32, TypeFloat:
		return 4, true
	case TypeInt64, TypeUInt64, TypeDouble, TypeVector2:
		return 8, true
	case TypeKey, TypeVector3, TypeColorRGB:
		return 12, true
	case TypeUnknown, TypeVector4, TypeColorRGBA:
		return 16, true
	case TypeBBox:
		return 24, true
	case TypeTransform:
		return 56, true
	case TypeText:
		return 520, true
	default:
		return 0, false
	}
}

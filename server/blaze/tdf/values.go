package tdf

// IntegerValue constructs an integer value.
func IntegerValue(value uint64) Value {
	return Value{Type: Integer, Integer: value}
}

// StringValue constructs a string value.
func StringValue(value string) Value {
	return Value{Type: String, String: value}
}

// BinaryValue constructs a binary blob value with independent storage.
func BinaryValue(value []byte) Value {
	return Value{Type: Binary, Binary: append([]byte(nil), value...)}
}

// StructValue constructs a terminated structure value.
func StructValue(fields ...Field) Value {
	return Value{Type: Struct, Fields: fields}
}

// UnionValue constructs a union value.
func UnionValue(activeMember uint8, fields ...Field) Value {
	return Value{Type: Union, ActiveMember: activeMember, Fields: fields}
}

// ListValue constructs a homogeneous list.
func ListValue(valueType Type, values ...Value) Value {
	return Value{Type: List, ListType: valueType, Items: values}
}

// MapValue constructs a homogeneous map.
func MapValue(keyType, valueType Type, entries ...MapEntry) Value {
	return Value{Type: Map, KeyType: keyType, ValueType: valueType, Entries: entries}
}

// FieldNamed constructs a labeled field.
func FieldNamed(label string, value Value) Field {
	return Field{Label: label, Value: value}
}

// Find returns the last field with a matching label, mirroring the C++ JSON
// parser's replacement behavior for duplicate labels.
func Find(fields []Field, label string) (Value, bool) {
	for index := len(fields) - 1; index >= 0; index-- {
		if fields[index].Label == label {
			return fields[index].Value, true
		}
	}
	return Value{}, false
}

// Package lua51 decodes native Lua 5.1 bytecode. It does not execute chunks or
// provide host bindings.
package lua51

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const maximumPrototypeDepth = 128

// Chunk is the source identity and string constant inventory of one bytecode
// chunk, including all nested prototypes.
type Chunk struct {
	SourceName string
	Strings    []string
	Main       *Prototype
}

// Prototype is one decoded Lua function prototype.
type Prototype struct {
	SourceName      string
	LineDefined     uint64
	LastLineDefined uint64
	UpvalueCount    byte
	ParameterCount  byte
	IsVararg        byte
	MaxStackSize    byte
	Instructions    []Instruction
	Constants       []Constant
	Prototypes      []*Prototype
}

// ConstantKind is one Lua 5.1 constant tag.
type ConstantKind byte

const (
	ConstantNil ConstantKind = iota
	ConstantBoolean
	ConstantNumber ConstantKind = 3
	ConstantString ConstantKind = 4
)

// Constant retains one typed constant from a function prototype.
type Constant struct {
	Kind   ConstantKind
	IsTrue bool
	Number float64
	String string
}

type reader struct {
	contents         []byte
	offset           int
	order            binary.ByteOrder
	integerSize      int
	sizeTSize        int
	instructionSize  int
	numberSize       int
	isNumberIntegral bool
}

// Inspect validates and decodes a native Lua 5.1 chunk.
func Inspect(contents []byte) (*Chunk, error) {
	if len(contents) < 12 {
		return nil, errors.New("headerShort")
	}
	if string(contents[:4]) != "\x1bLua" {
		return nil, errors.New("signatureMismatch")
	}
	if contents[4] != 0x51 {
		return nil, fmt.Errorf("versionMismatch: 0x%02x", contents[4])
	}
	if contents[5] != 0 {
		return nil, fmt.Errorf("formatMismatch: %d", contents[5])
	}

	r := &reader{contents: contents, offset: 12}
	switch contents[6] {
	case 0:
		r.order = binary.BigEndian
	case 1:
		r.order = binary.LittleEndian
	default:
		return nil, fmt.Errorf("endianMismatch: %d", contents[6])
	}
	r.integerSize = int(contents[7])
	r.sizeTSize = int(contents[8])
	r.instructionSize = int(contents[9])
	r.numberSize = int(contents[10])
	r.isNumberIntegral = contents[11] != 0
	if r.integerSize != 4 && r.integerSize != 8 {
		return nil, fmt.Errorf("integerSize: %d", r.integerSize)
	}
	if r.sizeTSize != 4 && r.sizeTSize != 8 {
		return nil, fmt.Errorf("sizeTSize: %d", r.sizeTSize)
	}
	if r.instructionSize <= 0 || r.numberSize <= 0 {
		return nil, errors.New("numericSize")
	}

	chunk := &Chunk{}
	prototype, err := r.readPrototype(chunk, 0, "")
	if err != nil {
		return nil, fmt.Errorf("prototypeRead: %w", err)
	}
	chunk.Main = prototype
	return chunk, nil
}

func (r *reader) readPrototype(chunk *Chunk, depth int, parentSource string) (*Prototype, error) {
	if depth >= maximumPrototypeDepth {
		return nil, errors.New("prototypeDepth")
	}
	sourceName, err := r.readString()
	if err != nil {
		return nil, fmt.Errorf("sourceRead: %w", err)
	}
	if sourceName == "" {
		sourceName = parentSource
	}
	if depth == 0 {
		chunk.SourceName = sourceName
	}
	lineDefined, err := r.readUnsigned(r.integerSize)
	if err != nil {
		return nil, fmt.Errorf("lineDefined: %w", err)
	}
	lastLineDefined, err := r.readUnsigned(r.integerSize)
	if err != nil {
		return nil, fmt.Errorf("lastLineDefined: %w", err)
	}
	prototype := &Prototype{SourceName: sourceName, LineDefined: lineDefined, LastLineDefined: lastLineDefined}
	prototype.UpvalueCount, err = r.readByte()
	if err != nil {
		return nil, fmt.Errorf("upvalueCount: %w", err)
	}
	prototype.ParameterCount, err = r.readByte()
	if err != nil {
		return nil, fmt.Errorf("parameterCount: %w", err)
	}
	prototype.IsVararg, err = r.readByte()
	if err != nil {
		return nil, fmt.Errorf("varargRead: %w", err)
	}
	prototype.MaxStackSize, err = r.readByte()
	if err != nil {
		return nil, fmt.Errorf("stackSize: %w", err)
	}
	instructionCount, err := r.readCount()
	if err != nil {
		return nil, fmt.Errorf("instructionCount: %w", err)
	}
	if r.instructionSize != 4 {
		return nil, fmt.Errorf("instructionSize: %d", r.instructionSize)
	}
	prototype.Instructions = make([]Instruction, instructionCount)
	for index := 0; index < instructionCount; index++ {
		instruction, instructionErr := r.readUnsigned(r.instructionSize)
		if instructionErr != nil {
			return nil, fmt.Errorf("instructionRead[%d]: %w", index, instructionErr)
		}
		prototype.Instructions[index] = Instruction(instruction)
	}
	constantCount, err := r.readCount()
	if err != nil {
		return nil, fmt.Errorf("constantCount: %w", err)
	}
	prototype.Constants = make([]Constant, 0, constantCount)
	for index := 0; index < constantCount; index++ {
		constantType, typeErr := r.readByte()
		if typeErr != nil {
			return nil, fmt.Errorf("constantType[%d]: %w", index, typeErr)
		}
		constant := Constant{Kind: ConstantKind(constantType)}
		switch constantType {
		case 0:
		case 1:
			var flag byte
			flag, typeErr = r.readByte()
			constant.IsTrue = flag != 0
		case 3:
			constant.Number, typeErr = r.readNumber()
		case 4:
			constant.String, typeErr = r.readString()
			if typeErr == nil {
				chunk.Strings = append(chunk.Strings, constant.String)
			}
		default:
			return nil, fmt.Errorf("constantType[%d]: %d", index, constantType)
		}
		if typeErr != nil {
			return nil, fmt.Errorf("constantRead[%d]: %w", index, typeErr)
		}
		prototype.Constants = append(prototype.Constants, constant)
	}
	prototypeCount, err := r.readCount()
	if err != nil {
		return nil, fmt.Errorf("prototypeCount: %w", err)
	}
	prototype.Prototypes = make([]*Prototype, 0, prototypeCount)
	for index := 0; index < prototypeCount; index++ {
		var child *Prototype
		child, err = r.readPrototype(chunk, depth+1, sourceName)
		if err != nil {
			return nil, fmt.Errorf("prototype[%d]: %w", index, err)
		}
		prototype.Prototypes = append(prototype.Prototypes, child)
	}
	lineCount, err := r.readCount()
	if err != nil {
		return nil, fmt.Errorf("lineCount: %w", err)
	}
	err = r.skipProduct(lineCount, r.integerSize)
	if err != nil {
		return nil, fmt.Errorf("lineInfoSkip: %w", err)
	}
	localCount, err := r.readCount()
	if err != nil {
		return nil, fmt.Errorf("localCount: %w", err)
	}
	for index := 0; index < localCount; index++ {
		_, err = r.readString()
		if err != nil {
			return nil, fmt.Errorf("localName[%d]: %w", index, err)
		}
		err = r.skip(r.integerSize * 2)
		if err != nil {
			return nil, fmt.Errorf("localRange[%d]: %w", index, err)
		}
	}
	upvalueCount, err := r.readCount()
	if err != nil {
		return nil, fmt.Errorf("upvalueNameCount: %w", err)
	}
	for index := 0; index < upvalueCount; index++ {
		_, err = r.readString()
		if err != nil {
			return nil, fmt.Errorf("upvalueName[%d]: %w", index, err)
		}
	}
	return prototype, nil
}

func (r *reader) readNumber() (float64, error) {
	bits, err := r.readUnsigned(r.numberSize)
	if err != nil {
		return 0, fmt.Errorf("numberRead: %w", err)
	}
	switch r.numberSize {
	case 4:
		if r.isNumberIntegral {
			return float64(int32(bits)), nil
		}
		return float64(math.Float32frombits(uint32(bits))), nil
	case 8:
		if r.isNumberIntegral {
			return float64(int64(bits)), nil
		}
		return math.Float64frombits(bits), nil
	default:
		return 0, fmt.Errorf("numberSize: %d", r.numberSize)
	}
}

func (r *reader) readString() (string, error) {
	size, err := r.readUnsigned(r.sizeTSize)
	if err != nil {
		return "", fmt.Errorf("stringSize: %w", err)
	}
	if size == 0 {
		return "", nil
	}
	if size > uint64(math.MaxInt) {
		return "", errors.New("stringSizeOverflow")
	}
	end := r.offset + int(size)
	if end < r.offset || end > len(r.contents) {
		return "", errors.New("stringBounds")
	}
	text := r.contents[r.offset:end]
	r.offset = end
	if len(text) > 0 && text[len(text)-1] == 0 {
		text = text[:len(text)-1]
	}
	return string(text), nil
}

func (r *reader) readCount() (int, error) {
	count, err := r.readUnsigned(r.integerSize)
	if err != nil {
		return 0, fmt.Errorf("countRead: %w", err)
	}
	if count > uint64(math.MaxInt) {
		return 0, errors.New("countOverflow")
	}
	return int(count), nil
}

func (r *reader) readUnsigned(size int) (uint64, error) {
	end := r.offset + size
	if size <= 0 || end < r.offset || end > len(r.contents) {
		return 0, errors.New("integerBounds")
	}
	contents := r.contents[r.offset:end]
	r.offset = end
	if size == 4 {
		return uint64(r.order.Uint32(contents)), nil
	}
	if size == 8 {
		return r.order.Uint64(contents), nil
	}
	return 0, fmt.Errorf("integerWidth: %d", size)
}

func (r *reader) readByte() (byte, error) {
	if r.offset >= len(r.contents) {
		return 0, errors.New("byteBounds")
	}
	result := r.contents[r.offset]
	r.offset++
	return result, nil
}

func (r *reader) skipProduct(count, size int) error {
	if count < 0 || size < 0 {
		return errors.New("skipOverflow")
	}
	if size == 0 {
		return nil
	}
	if count > math.MaxInt/size {
		return errors.New("skipOverflow")
	}
	err := r.skip(count * size)
	if err != nil {
		return fmt.Errorf("skipSize: %w", err)
	}
	return nil
}

func (r *reader) skip(size int) error {
	end := r.offset + size
	if size < 0 || end < r.offset || end > len(r.contents) {
		return errors.New("skipBounds")
	}
	r.offset = end
	return nil
}

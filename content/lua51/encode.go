package lua51

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

type chunkABI struct {
	header      [12]byte
	order       binary.ByteOrder
	integerSize int
	sizeTSize   int
	numberSize  int
}

func readChunkABI(header []byte) (chunkABI, error) {
	if len(header) < 12 || string(header[:4]) != "\x1bLua" {
		return chunkABI{}, errors.New("invalid Lua chunk header")
	}
	if header[4] != 0x51 || header[5] != 0 {
		return chunkABI{}, errors.New("only standard Lua 5.1 chunks are supported")
	}
	if header[6] > 1 || header[7] != 4 && header[7] != 8 || header[8] != 4 && header[8] != 8 || header[9] != 4 {
		return chunkABI{}, errors.New("unsupported Lua chunk integer layout")
	}
	if header[10] != 4 && header[10] != 8 || header[11] != 0 {
		return chunkABI{}, errors.New("only floating-point Lua chunks are supported")
	}
	abi := chunkABI{order: binary.LittleEndian, integerSize: int(header[7]), sizeTSize: int(header[8]), numberSize: int(header[10])}
	copy(abi.header[:], header[:12])
	if header[6] == 0 {
		abi.order = binary.BigEndian
	}
	return abi, nil
}

// Transcoding retains every serialized field, including all debug metadata.
// Decoding through Inspect would lose the debug sections intentionally omitted
// from its lightweight analysis representation.
type chunkEncoder struct {
	reader  *reader
	target  chunkABI
	payload []byte
}

func transcodeChunk(payload []byte, target chunkABI) ([]byte, error) {
	source, err := readChunkABI(payload)
	if err != nil {
		return nil, fmt.Errorf("sourceHeader: %w", err)
	}
	encoder := chunkEncoder{
		reader:  &reader{contents: payload, offset: 12, order: source.order, integerSize: source.integerSize, sizeTSize: source.sizeTSize, instructionSize: 4, numberSize: source.numberSize},
		target:  target,
		payload: append([]byte(nil), target.header[:]...),
	}
	err = encoder.prototype(0)
	if err != nil {
		return nil, fmt.Errorf("prototypeEncode: %w", err)
	}
	if encoder.reader.offset != len(payload) {
		return nil, errors.New("trailing chunk bytes")
	}
	return encoder.payload, nil
}

func (e *chunkEncoder) unsigned(number uint64, size int) error {
	var encoded [8]byte
	if size == 4 {
		if number > math.MaxUint32 {
			return errors.New("integer does not fit target ABI")
		}
		e.target.order.PutUint32(encoded[:4], uint32(number))
	} else {
		e.target.order.PutUint64(encoded[:], number)
	}
	e.payload = append(e.payload, encoded[:size]...)
	return nil
}

func (e *chunkEncoder) integer() (uint64, error) {
	number, err := e.reader.readUnsigned(e.reader.integerSize)
	if err != nil {
		return 0, fmt.Errorf("integerRead: %w", err)
	}
	err = e.unsigned(number, e.target.integerSize)
	if err != nil {
		return 0, fmt.Errorf("integerWrite: %w", err)
	}
	return number, nil
}

func (e *chunkEncoder) count() (int, error) {
	count, err := e.integer()
	if err != nil {
		return 0, fmt.Errorf("countRead: %w", err)
	}
	// Every element below occupies at least one input byte. Bound loops before
	// converting counts to int, even for a malformed compiler result.
	if count > uint64(len(e.reader.contents)-e.reader.offset) {
		return 0, errors.New("count exceeds remaining chunk bytes")
	}
	return int(count), nil
}

func (e *chunkEncoder) text() error {
	size, err := e.reader.readUnsigned(e.reader.sizeTSize)
	if err != nil {
		return fmt.Errorf("stringSize: %w", err)
	}
	if size > uint64(len(e.reader.contents)-e.reader.offset) {
		return errors.New("string exceeds remaining chunk bytes")
	}
	err = e.unsigned(size, e.target.sizeTSize)
	if err != nil {
		return fmt.Errorf("stringLength: %w", err)
	}
	end := e.reader.offset + int(size)
	if size > 0 && e.reader.contents[end-1] != 0 {
		return errors.New("chunk string is not NUL-terminated")
	}
	e.payload = append(e.payload, e.reader.contents[e.reader.offset:end]...)
	e.reader.offset = end
	return nil
}

func (e *chunkEncoder) prototype(depth int) error {
	if depth >= maximumPrototypeDepth {
		return errors.New("prototype depth exceeded")
	}
	err := e.text()
	if err != nil {
		return fmt.Errorf("sourceName: %w", err)
	}
	for index := 0; index < 2; index++ {
		line, lineErr := e.integer()
		if lineErr != nil {
			return fmt.Errorf("sourceLine[%d]: %w", index, lineErr)
		}
		if line > math.MaxInt32 {
			return errors.New("source line exceeds Lua range")
		}
	}
	for index := 0; index < 4; index++ {
		flag, flagErr := e.reader.readByte()
		if flagErr != nil {
			return fmt.Errorf("functionFlag[%d]: %w", index, flagErr)
		}
		e.payload = append(e.payload, flag)
	}
	instructionCount, err := e.count()
	if err != nil {
		return fmt.Errorf("instructionCount: %w", err)
	}
	for index := 0; index < instructionCount; index++ {
		instruction, readErr := e.reader.readUnsigned(4)
		if readErr != nil {
			return fmt.Errorf("instructionRead[%d]: %w", index, readErr)
		}
		err = e.unsigned(instruction, 4)
		if err != nil {
			return fmt.Errorf("instructionWrite[%d]: %w", index, err)
		}
	}
	constantCount, err := e.count()
	if err != nil {
		return fmt.Errorf("constantCount: %w", err)
	}
	for index := 0; index < constantCount; index++ {
		err = e.constant()
		if err != nil {
			return fmt.Errorf("constantEncode[%d]: %w", index, err)
		}
	}
	childCount, err := e.count()
	if err != nil {
		return fmt.Errorf("childCount: %w", err)
	}
	for index := 0; index < childCount; index++ {
		err = e.prototype(depth + 1)
		if err != nil {
			return fmt.Errorf("childEncode[%d]: %w", index, err)
		}
	}
	err = e.debug()
	if err != nil {
		return fmt.Errorf("debugEncode: %w", err)
	}
	return nil
}

func (e *chunkEncoder) constant() error {
	kind, err := e.reader.readByte()
	if err != nil {
		return fmt.Errorf("kindRead: %w", err)
	}
	e.payload = append(e.payload, kind)
	switch ConstantKind(kind) {
	case ConstantNil:
		return nil
	case ConstantBoolean:
		flag, readErr := e.reader.readByte()
		if readErr != nil {
			return fmt.Errorf("booleanRead: %w", readErr)
		}
		e.payload = append(e.payload, flag)
	case ConstantNumber:
		if e.reader.numberSize == e.target.numberSize {
			// Preserve the exact representation, including signed zero and
			// NaN payloads, when only the integer ABI or byte order changes.
			bits, readErr := e.reader.readUnsigned(e.reader.numberSize)
			if readErr != nil {
				return fmt.Errorf("numberBits: %w", readErr)
			}
			err = e.unsigned(bits, e.target.numberSize)
			if err != nil {
				return fmt.Errorf("numberBitsWrite: %w", err)
			}
			return nil
		}
		number, readErr := e.reader.readNumber()
		if readErr != nil {
			return fmt.Errorf("numberRead: %w", readErr)
		}
		bits := math.Float64bits(number)
		if e.target.numberSize == 4 {
			narrowed := float32(number)
			if !math.IsInf(number, 0) && math.IsInf(float64(narrowed), 0) {
				return errors.New("number exceeds target ABI floating-point range")
			}
			bits = uint64(math.Float32bits(narrowed))
		}
		err = e.unsigned(bits, e.target.numberSize)
		if err != nil {
			return fmt.Errorf("numberWrite: %w", err)
		}
	case ConstantString:
		err = e.text()
		if err != nil {
			return fmt.Errorf("stringWrite: %w", err)
		}
	default:
		return fmt.Errorf("unsupported constant kind %d", kind)
	}
	return nil
}

func (e *chunkEncoder) debug() error {
	lineCount, err := e.count()
	if err != nil {
		return fmt.Errorf("lineCount: %w", err)
	}
	for index := 0; index < lineCount; index++ {
		line, lineErr := e.integer()
		if lineErr != nil {
			return fmt.Errorf("lineEncode[%d]: %w", index, lineErr)
		}
		if line > math.MaxInt32 {
			return errors.New("debug line exceeds Lua range")
		}
	}
	localCount, err := e.count()
	if err != nil {
		return fmt.Errorf("localCount: %w", err)
	}
	for index := 0; index < localCount; index++ {
		err = e.text()
		if err != nil {
			return fmt.Errorf("localName[%d]: %w", index, err)
		}
		for boundary := 0; boundary < 2; boundary++ {
			pc, rangeErr := e.integer()
			if rangeErr != nil {
				return fmt.Errorf("localRange[%d]: %w", boundary, rangeErr)
			}
			if pc > math.MaxInt32 {
				return errors.New("local range exceeds Lua instruction limit")
			}
		}
	}
	upvalueCount, err := e.count()
	if err != nil {
		return fmt.Errorf("upvalueCount: %w", err)
	}
	for index := 0; index < upvalueCount; index++ {
		err = e.text()
		if err != nil {
			return fmt.Errorf("upvalueName[%d]: %w", index, err)
		}
	}
	return nil
}

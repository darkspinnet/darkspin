package rw4

import (
	"encoding/binary"
	"fmt"
)

const (
	materialFlagModelToWorld       = 0x00000001
	materialFlagModelToWorldObject = 0x00000002
	materialFlagShaderData         = 0x00000008
	materialFlagColor              = 0x00000010
	materialFlagAmbient            = 0x00000020
	materialFlagBooleans           = 0x00008000
	materialFlagVertexDescription  = 0x00100000
	materialFlag3RenderStates      = 0x00020000
	materialFlag3TextureSlots      = 0x0001FFFF
)

func decodeCompiledState(payload []byte) (CompiledState, error) {
	r := materialReader{payload: payload}
	err := r.skip(4)
	if err != nil {
		return CompiledState{}, fmt.Errorf("size: %w", err)
	}
	_, err = r.uint32()
	if err != nil {
		return CompiledState{}, fmt.Errorf("primitiveType: %w", err)
	}
	flags1, err := r.uint32()
	if err != nil {
		return CompiledState{}, fmt.Errorf("flags1: %w", err)
	}
	_, err = r.uint32()
	if err != nil {
		return CompiledState{}, fmt.Errorf("flags2: %w", err)
	}
	flags3, err := r.uint32()
	if err != nil {
		return CompiledState{}, fmt.Errorf("flags3: %w", err)
	}
	field14, err := r.uint32()
	if err != nil {
		return CompiledState{}, fmt.Errorf("field14: %w", err)
	}
	err = r.skip(8)
	if err != nil {
		return CompiledState{}, fmt.Errorf("renderer: %w", err)
	}
	if flags1&materialFlagModelToWorld != 0 {
		modelSize := 64
		if flags1&materialFlagModelToWorldObject != 0 {
			modelSize = 4
		}
		err = r.skip(modelSize)
		if err != nil {
			return CompiledState{}, fmt.Errorf("modelToWorld: %w", err)
		}
	}
	if flags1&materialFlagVertexDescription != 0 {
		err = r.vertexDescription()
		if err != nil {
			return CompiledState{}, fmt.Errorf("vertexDescription: %w", err)
		}
	}
	if flags1&materialFlagShaderData != 0 {
		err = r.shaderData()
		if err != nil {
			return CompiledState{}, fmt.Errorf("shaderData: %w", err)
		}
	}
	if flags1&materialFlagColor != 0 {
		err = r.skip(16)
		if err != nil {
			return CompiledState{}, fmt.Errorf("materialColor: %w", err)
		}
	}
	if flags1&materialFlagAmbient != 0 {
		err = r.skip(12)
		if err != nil {
			return CompiledState{}, fmt.Errorf("ambientColor: %w", err)
		}
	}
	for bit := uint32(6); bit < 14; bit++ {
		if flags1&(1<<bit) == 0 {
			continue
		}
		err = r.skip(4)
		if err != nil {
			return CompiledState{}, fmt.Errorf("unknownFloat[%d]: %w", bit, err)
		}
	}
	if flags1&materialFlagBooleans != 0 {
		err = r.skip(17)
		if err != nil {
			return CompiledState{}, fmt.Errorf("booleans: %w", err)
		}
	}
	optionalSizes := []struct {
		flag uint32
		size int
	}{
		{flag: 0x00010000, size: 4},
		{flag: 0x00020000, size: 12},
		{flag: 0x00040000, size: 4},
		{flag: 0x00080000, size: 4},
	}
	for _, optional := range optionalSizes {
		if flags1&optional.flag == 0 {
			continue
		}
		err = r.skip(optional.size)
		if err != nil {
			return CompiledState{}, fmt.Errorf("optional[0x%X]: %w", optional.flag, err)
		}
	}
	fieldSizes := []struct {
		flag uint32
		size int
	}{
		{flag: 0x00020000, size: 28},
		{flag: 0x00040000, size: 44},
		{flag: 0x00080000, size: 44},
	}
	for _, optional := range fieldSizes {
		if field14&optional.flag == 0 {
			continue
		}
		err = r.skip(optional.size)
		if err != nil {
			return CompiledState{}, fmt.Errorf("field14Data[0x%X]: %w", optional.flag, err)
		}
	}
	if flags3&materialFlag3RenderStates != 0 {
		err = r.stateGroups()
		if err != nil {
			return CompiledState{}, fmt.Errorf("renderStates: %w", err)
		}
	}
	_, err = r.uint32()
	if err != nil {
		return CompiledState{}, fmt.Errorf("paletteRef: %w", err)
	}
	compiledState := CompiledState{Raw: append([]byte(nil), payload...)}
	if flags3&materialFlag3TextureSlots == 0 {
		return compiledState, nil
	}
	for {
		samplerIndex, readErr := r.int32()
		if readErr != nil {
			return CompiledState{}, fmt.Errorf("samplerIndex: %w", readErr)
		}
		if samplerIndex == -1 {
			break
		}
		rasterRef, readErr := r.uint32()
		if readErr != nil {
			return CompiledState{}, fmt.Errorf("rasterRef[%d]: %w", len(compiledState.TextureSlots), readErr)
		}
		stageMask, readErr := r.uint32()
		if readErr != nil {
			return CompiledState{}, fmt.Errorf("stageMask[%d]: %w", len(compiledState.TextureSlots), readErr)
		}
		if stageMask != 0 {
			readErr = r.states()
			if readErr != nil {
				return CompiledState{}, fmt.Errorf("stageStates[%d]: %w", len(compiledState.TextureSlots), readErr)
			}
		}
		samplerMask, readErr := r.uint32()
		if readErr != nil {
			return CompiledState{}, fmt.Errorf("samplerMask[%d]: %w", len(compiledState.TextureSlots), readErr)
		}
		if samplerMask != 0 {
			readErr = r.states()
			if readErr != nil {
				return CompiledState{}, fmt.Errorf("samplerStates[%d]: %w", len(compiledState.TextureSlots), readErr)
			}
		}
		compiledState.TextureSlots = append(compiledState.TextureSlots, TextureSlot{
			SamplerIndex: uint32(samplerIndex),
			RasterRef:    rasterRef,
		})
	}
	return compiledState, nil
}

type materialReader struct {
	payload []byte
	offset  int
}

func (e *materialReader) skip(size int) error {
	if size < 0 || e.offset > len(e.payload)-size {
		return fmt.Errorf("range: %d:%d exceeds %d", e.offset, e.offset+size, len(e.payload))
	}
	e.offset += size
	return nil
}

func (e *materialReader) uint32() (uint32, error) {
	if e.offset > len(e.payload)-4 {
		return 0, fmt.Errorf("range: %d:%d exceeds %d", e.offset, e.offset+4, len(e.payload))
	}
	number := binary.LittleEndian.Uint32(e.payload[e.offset : e.offset+4])
	e.offset += 4
	return number, nil
}

func (e *materialReader) int32() (int32, error) {
	number, err := e.uint32()
	if err != nil {
		return 0, fmt.Errorf("uint32: %w", err)
	}
	return int32(number), nil
}

func (e *materialReader) int16() (int16, error) {
	if e.offset > len(e.payload)-2 {
		return 0, fmt.Errorf("range: %d:%d exceeds %d", e.offset, e.offset+2, len(e.payload))
	}
	number := int16(binary.LittleEndian.Uint16(e.payload[e.offset : e.offset+2]))
	e.offset += 2
	return number, nil
}

func (e *materialReader) vertexDescription() error {
	if e.offset > len(e.payload)-24 {
		return fmt.Errorf("headerRange: %d exceeds %d", e.offset, len(e.payload))
	}
	count := int(binary.LittleEndian.Uint16(e.payload[e.offset+12 : e.offset+14]))
	return e.skip(24 + count*12)
}

func (e *materialReader) shaderData() error {
	index, err := e.int16()
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	for index != 0 {
		if index > 0 {
			offset, readErr := e.int16()
			if readErr != nil {
				return fmt.Errorf("offset: %w", readErr)
			}
			length, readErr := e.uint32()
			if readErr != nil {
				return fmt.Errorf("length: %w", readErr)
			}
			if offset < 0 {
				return fmt.Errorf("offset: %d is negative", offset)
			}
			readErr = e.skip(int(offset) + int(length))
			if readErr != nil {
				return fmt.Errorf("data: %w", readErr)
			}
			if length == 0 {
				readErr = e.skip(4)
				if readErr != nil {
					return fmt.Errorf("emptyData: %w", readErr)
				}
			}
		}
		index, err = e.int16()
		if err != nil {
			return fmt.Errorf("nextIndex: %w", err)
		}
	}
	err = e.skip(6)
	if err != nil {
		return fmt.Errorf("terminator: %w", err)
	}
	return nil
}

func (e *materialReader) stateGroups() error {
	group, err := e.int32()
	if err != nil {
		return fmt.Errorf("group: %w", err)
	}
	for group != -1 {
		err = e.states()
		if err != nil {
			return fmt.Errorf("group[%d]: %w", group, err)
		}
		group, err = e.int32()
		if err != nil {
			return fmt.Errorf("nextGroup: %w", err)
		}
	}
	return nil
}

func (e *materialReader) states() error {
	state, err := e.int32()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	for state != -1 {
		_, err = e.uint32()
		if err != nil {
			return fmt.Errorf("state[%d]: %w", state, err)
		}
		state, err = e.int32()
		if err != nil {
			return fmt.Errorf("nextState: %w", err)
		}
	}
	return nil
}

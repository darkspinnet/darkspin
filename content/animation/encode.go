package animation

import (
	"encoding/binary"
	"fmt"
	"math"
)

type encoder struct {
	payload     []byte
	relocations []uint32
}

func Encode(document *Document) ([]byte, error) {
	if document == nil {
		return nil, fmt.Errorf("document: nil")
	}
	if len(document.Source) >= 256 {
		return nil, fmt.Errorf("sourceLength: %d", len(document.Source))
	}
	e := &encoder{payload: make([]byte, 0x200)}
	e.putUint32(0, Magic)
	formatVersion := document.FormatVersion
	if formatVersion == 0 {
		formatVersion = Version
	}
	e.putUint32(8, formatVersion)
	copy(e.payload[0x0C:0x10C], document.Source)
	e.putUint32(0x110, document.ResourceID)
	e.putUint32(0x118, document.HeaderFlags)
	e.putFloat32(0x11C, document.FrameStep)
	e.putFloat32(0x120, document.Length)
	e.putUint32(0x154, document.Predicate.Flags)
	e.putUint32(0x158, document.Predicate.State)

	channelOffsetsOffset := e.allocate(len(document.Channels) * 4)
	e.relocations = append(e.relocations, 0x140, 0x148)
	for channelIndex, channel := range document.Channels {
		channelOffset, err := e.writeChannel(channel)
		if err != nil {
			return nil, fmt.Errorf("channel[%d]: %w", channelIndex, err)
		}
		e.putUint32(channelOffsetsOffset+channelIndex*4, uint32(channelOffset))
		e.relocations = append(e.relocations, uint32(channelOffsetsOffset+channelIndex*4))
	}

	relocationOffset := len(e.payload)
	eventRelocationCount := 0
	for _, event := range document.Events {
		if event.ID != 0 {
			eventRelocationCount++
		}
	}
	eventOffset := relocationOffset + (len(e.relocations)+eventRelocationCount)*4
	nameOffset := eventOffset + len(document.Events)*0x60
	for eventIndex, event := range document.Events {
		if event.ID != 0 {
			e.relocations = append(e.relocations, uint32(eventOffset+eventIndex*0x60+0x40))
		}
	}
	relocationOffset = len(e.payload)
	e.allocate(len(e.relocations) * 4)
	for index, relocation := range e.relocations {
		e.putUint32(relocationOffset+index*4, relocation)
	}
	if len(e.payload) != eventOffset {
		return nil, fmt.Errorf("eventOffset: got %d, want %d", len(e.payload), eventOffset)
	}
	for eventIndex, event := range document.Events {
		eventNameOffset := uint32(0)
		if event.ID != 0 {
			eventNameOffset = uint32(nameOffset)
			nameOffset += len(event.Name) + 1
		}
		err := e.writeEvent(event, eventNameOffset)
		if err != nil {
			return nil, fmt.Errorf("event[%d]: %w", eventIndex, err)
		}
	}
	for _, event := range document.Events {
		if event.ID == 0 {
			continue
		}
		e.payload = append(e.payload, event.Name...)
		e.payload = append(e.payload, 0)
	}
	e.putUint32(4, uint32(len(e.payload)))
	e.putUint32(0x13C, uint32(len(document.Events)))
	e.putUint32(0x140, uint32(eventOffset))
	e.putUint32(0x144, uint32(len(document.Channels)))
	e.putUint32(0x148, uint32(channelOffsetsOffset))
	e.putUint32(0x14C, uint32(len(e.relocations)))
	e.putUint32(0x150, uint32(relocationOffset))
	return e.payload, nil
}

func (e *encoder) writeChannel(channel Channel) (int, error) {
	if len(channel.Name) >= 0x80 {
		return 0, fmt.Errorf("nameLength: %d", len(channel.Name))
	}
	for componentIndex, component := range channel.Components {
		if uint64(len(component.Keyframes)) != uint64(channel.KeyframeCount) {
			return 0, fmt.Errorf("component[%d]Frames: got %d, want %d", componentIndex, len(component.Keyframes), channel.KeyframeCount)
		}
	}
	channelOffset := e.allocate(0xE4)
	e.putUint32(channelOffset, 0x4E414843)
	e.relocations = append(e.relocations, uint32(channelOffset+4), uint32(channelOffset+0xD8), uint32(channelOffset+0xE0))
	copy(e.payload[channelOffset+8:channelOffset+0x88], channel.Name)
	e.putUint32(channelOffset+0x88, channel.MovementFlags)
	e.writeSelector(channelOffset+0x8C, channel.PrimarySelector)
	e.writeSelector(channelOffset+0x9C, channel.SecondarySelector)
	e.putUint32(channelOffset+0xAC, channel.BindFlags)
	e.putUint32(channelOffset+0xD4, channel.KeyframeCount)
	e.putUint32(channelOffset+0xDC, uint32(len(channel.Components)))
	componentOffset := e.allocate(len(channel.Components) * 32)
	e.putUint32(channelOffset+0xE0, uint32(componentOffset))
	componentOffsets := make([]uint32, len(channel.Components))
	stride := uint32(0)
	for index, component := range channel.Components {
		componentOffsets[index] = stride
		size, err := keyframeSize(component.Type())
		if err != nil {
			return 0, fmt.Errorf("component[%d]: %w", index, err)
		}
		stride += size
	}
	keyframeOffset := len(e.payload)
	e.putUint32(channelOffset+0xD8, uint32(keyframeOffset))
	for frameIndex := uint32(0); frameIndex < channel.KeyframeCount; frameIndex++ {
		for componentIndex, component := range channel.Components {
			err := e.writeKeyframe(component.Keyframes[frameIndex], component.Type())
			if err != nil {
				return 0, fmt.Errorf("component[%d]Frame[%d]: %w", componentIndex, frameIndex, err)
			}
		}
	}
	for index, component := range channel.Components {
		offset := componentOffset + index*32
		e.putUint32(offset, component.Flags)
		e.putUint32(offset+4, component.ID)
		e.putUint32(offset+8, componentOffsets[index])
		e.putUint32(offset+12, stride)
		e.putUint32(offset+16, component.Index)
	}
	return channelOffset, nil
}

func (e *encoder) writeSelector(offset int, selector Selector) {
	e.putUint32(offset, selector.Flags)
	copy(e.payload[offset+4:offset+8], selector.Capability)
	e.putUint32(offset+8, selector.Field8)
	e.putUint32(offset+12, selector.FieldC)
}

func (e *encoder) writeEvent(event Event, nameOffset uint32) error {
	offset := e.allocate(0x60)
	e.putUint32(offset, event.Flags)
	for index, selector := range event.Selectors {
		selectorOffset := offset + 4 + index*12
		e.putUint32(selectorOffset, selector.Flags)
		e.putUint32(selectorOffset+4, selector.SelectFlags)
		copy(e.payload[selectorOffset+8:selectorOffset+12], selector.Capability)
	}
	if event.Archetype == "default" {
		e.putUint32(offset+0x34, ^uint32(0))
	} else {
		copy(e.payload[offset+0x34:offset+0x38], event.Archetype)
	}
	e.putUint32(offset+0x38, event.EventGroup)
	e.putUint32(offset+0x3C, event.ID)
	e.putUint32(offset+0x40, nameOffset)
	e.putUint32(offset+0x44, event.Parameter0)
	e.putFloat32(offset+0x48, event.MaxSqrDist)
	e.putUint32(offset+0x4C, event.Parameter1)
	e.putUint32(offset+0x50, event.Predicate.Flags)
	e.putUint32(offset+0x54, event.Predicate.State)
	return nil
}

func (e *encoder) writeKeyframe(keyframe Keyframe, componentType uint32) error {
	size, err := keyframeSize(componentType)
	if err != nil {
		return err
	}
	offset := e.allocate(int(size))
	switch componentType {
	case 0:
		e.putUint32(offset, uint32(keyframe.Time))
		binary.LittleEndian.PutUint16(e.payload[offset+8:offset+10], keyframe.EventStart)
		e.payload[offset+10] = keyframe.EventCount
		e.putUint32(offset+12, keyframe.Flags)
	case 1:
		for index, coordinate := range keyframe.Position {
			e.putFloat32(offset+index*4, coordinate)
		}
		e.putFloat32(offset+12, keyframe.Weight)
		return e.writeInterpolators(offset+16, keyframe.Interpolators, 4)
	case 2:
		for index, coordinate := range keyframe.Rotation {
			e.putFloat32(offset+index*4, coordinate)
		}
		e.putFloat32(offset+16, keyframe.Weight)
		e.payload[offset+28] = keyframe.NextMode
		e.payload[offset+29] = keyframe.PreviousMode
		return e.writeInterpolators(offset+0x54, keyframe.Interpolators, 1)
	case 3:
		e.putFloat32(offset, keyframe.Scalar)
		e.putFloat32(offset+4, keyframe.Weight)
		return e.writeInterpolators(offset+8, keyframe.Interpolators, 2)
	}
	return nil
}

func (e *encoder) writeInterpolators(offset int, interpolators []Interpolator, count int) error {
	if len(interpolators) != count {
		return fmt.Errorf("interpolators: got %d, want %d", len(interpolators), count)
	}
	for index, interpolator := range interpolators {
		interpolatorOffset := offset + index*16
		e.putFloat32(interpolatorOffset, interpolator.NextFactor)
		e.putFloat32(interpolatorOffset+4, interpolator.PreviousFactor)
		e.payload[interpolatorOffset+8] = interpolator.NextMode
		e.payload[interpolatorOffset+9] = interpolator.PreviousMode
	}
	return nil
}

func (e *encoder) allocate(size int) int {
	offset := len(e.payload)
	e.payload = append(e.payload, make([]byte, size)...)
	return offset
}

func (e *encoder) putUint32(offset int, number uint32) {
	binary.LittleEndian.PutUint32(e.payload[offset:offset+4], number)
}

func (e *encoder) putFloat32(offset int, number float32) {
	e.putUint32(offset, math.Float32bits(number))
}

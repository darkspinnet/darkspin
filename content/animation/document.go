// Package animation decodes and rebuilds Game compiled ANIM resources.
package animation

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	ResourceType = 0xEE17C6AD
	Magic        = 0x4D494E41
	Version      = 0x1B
	minVersion   = 0x14
)

type Document struct {
	FormatVersion uint32
	Source        string
	ResourceID    uint32
	HeaderFlags   uint32
	FrameStep     float32
	Length        float32
	Predicate     Predicate
	Events        []Event
	Channels      []Channel
}

type Predicate struct {
	Flags uint32
	State uint32
}

type Selector struct {
	Flags      uint32
	Capability string
	Field8     uint32
	FieldC     uint32
}

type Channel struct {
	Name              string
	MovementFlags     uint32
	PrimarySelector   Selector
	SecondarySelector Selector
	BindFlags         uint32
	KeyframeCount     uint32
	Components        []Component
}

type Component struct {
	Flags     uint32
	ID        uint32
	Index     uint32
	Keyframes []Keyframe
}

func (e Component) Type() uint32 { return e.Flags & 0xF }

type Interpolator struct {
	NextFactor     float32
	PreviousFactor float32
	NextMode       uint8
	PreviousMode   uint8
}

type Keyframe struct {
	Time          int32
	EventStart    uint16
	EventCount    uint8
	Flags         uint32
	Position      [3]float32
	Rotation      [4]float32
	Scalar        float32
	Weight        float32
	NextMode      uint8
	PreviousMode  uint8
	Interpolators []Interpolator
}

type EventSelector struct {
	Flags       uint32
	SelectFlags uint32
	Capability  string
}

type Event struct {
	Flags      uint32
	Selectors  [4]EventSelector
	Archetype  string
	EventGroup uint32
	ID         uint32
	Name       string
	Parameter0 uint32
	MaxSqrDist float32
	Parameter1 uint32
	Predicate  Predicate
}

func Decode(payload []byte) (*Document, error) {
	if len(payload) < 0x200 {
		return nil, fmt.Errorf("headerSize: got %d", len(payload))
	}
	if binary.LittleEndian.Uint32(payload) != Magic {
		return nil, errors.New("headerMagic: not ANIM")
	}
	fileSize := binary.LittleEndian.Uint32(payload[4:8])
	if fileSize != uint32(len(payload)) {
		return nil, fmt.Errorf("fileSize: got %d, want %d", fileSize, len(payload))
	}
	formatVersion := binary.LittleEndian.Uint32(payload[8:12])
	if formatVersion < minVersion || formatVersion > Version {
		return nil, fmt.Errorf("formatVersion: 0x%X", formatVersion)
	}
	document := &Document{
		FormatVersion: formatVersion,
		Source:        fixedString(payload[0x0C:0x10C]),
		ResourceID:    uint32At(payload, 0x110),
		HeaderFlags:   uint32At(payload, 0x118),
		FrameStep:     float32At(payload, 0x11C),
		Length:        float32At(payload, 0x120),
		Predicate:     Predicate{Flags: uint32At(payload, 0x154), State: uint32At(payload, 0x158)},
	}
	eventCount := uint32At(payload, 0x13C)
	eventOffset := uint32At(payload, 0x140)
	channelCount := uint32At(payload, 0x144)
	channelOffsets := uint32At(payload, 0x148)
	if err := boundedArray(payload, eventOffset, eventCount, 0x60, "events"); err != nil {
		return nil, err
	}
	if err := boundedArray(payload, channelOffsets, channelCount, 4, "channelOffsets"); err != nil {
		return nil, err
	}
	document.Events = make([]Event, eventCount)
	for index := range document.Events {
		offset := int(eventOffset) + index*0x60
		event, err := decodeEvent(payload, offset)
		if err != nil {
			return nil, fmt.Errorf("event[%d]: %w", index, err)
		}
		document.Events[index] = event
	}
	document.Channels = make([]Channel, channelCount)
	for index := range document.Channels {
		offset := uint32At(payload, int(channelOffsets)+index*4)
		channel, err := decodeChannel(payload, offset)
		if err != nil {
			return nil, fmt.Errorf("channel[%d]: %w", index, err)
		}
		document.Channels[index] = channel
	}
	return document, nil
}

func decodeChannel(payload []byte, channelOffset uint32) (Channel, error) {
	if err := boundedArray(payload, channelOffset, 1, 0xE4, "header"); err != nil {
		return Channel{}, err
	}
	offset := int(channelOffset)
	if uint32At(payload, offset) != 0x4E414843 {
		return Channel{}, errors.New("magic: not CHAN")
	}
	channel := Channel{
		Name:          fixedString(payload[offset+8 : offset+0x88]),
		MovementFlags: uint32At(payload, offset+0x88),
		PrimarySelector: Selector{Flags: uint32At(payload, offset+0x8C), Capability: fourCC(payload[offset+0x90 : offset+0x94]),
			Field8: uint32At(payload, offset+0x94), FieldC: uint32At(payload, offset+0x98)},
		SecondarySelector: Selector{Flags: uint32At(payload, offset+0x9C), Capability: fourCC(payload[offset+0xA0 : offset+0xA4]),
			Field8: uint32At(payload, offset+0xA4), FieldC: uint32At(payload, offset+0xA8)},
		BindFlags:     uint32At(payload, offset+0xAC),
		KeyframeCount: uint32At(payload, offset+0xD4),
	}
	keyframeOffset := uint32At(payload, offset+0xD8)
	componentCount := uint32At(payload, offset+0xDC)
	componentOffset := uint32At(payload, offset+0xE0)
	if err := boundedArray(payload, componentOffset, componentCount, 32, "components"); err != nil {
		return Channel{}, err
	}
	channel.Components = make([]Component, componentCount)
	for index := range channel.Components {
		metadataOffset := int(componentOffset) + index*32
		component := Component{Flags: uint32At(payload, metadataOffset), ID: uint32At(payload, metadataOffset+4), Index: uint32At(payload, metadataOffset+16)}
		componentDataOffset := uint32At(payload, metadataOffset+8)
		componentStride := uint32At(payload, metadataOffset+12)
		componentSize, err := keyframeSize(component.Type())
		if err != nil {
			return Channel{}, fmt.Errorf("component[%d]: %w", index, err)
		}
		if componentStride < componentDataOffset+componentSize {
			return Channel{}, fmt.Errorf("component[%d]Stride: %d smaller than offset %d plus size %d", index, componentStride, componentDataOffset, componentSize)
		}
		component.Keyframes = make([]Keyframe, channel.KeyframeCount)
		for frameIndex := range component.Keyframes {
			frameOffset := uint64(keyframeOffset) + uint64(componentStride)*uint64(frameIndex) + uint64(componentDataOffset)
			if frameOffset+uint64(componentSize) > uint64(len(payload)) {
				return Channel{}, fmt.Errorf("component[%d]Frame[%d]: exceeds payload", index, frameIndex)
			}
			component.Keyframes[frameIndex] = decodeKeyframe(payload[int(frameOffset):int(frameOffset)+int(componentSize)], component.Type())
		}
		channel.Components[index] = component
	}
	return channel, nil
}

func decodeEvent(payload []byte, offset int) (Event, error) {
	event := Event{Flags: uint32At(payload, offset)}
	for index := range event.Selectors {
		selectorOffset := offset + 4 + index*12
		event.Selectors[index] = EventSelector{Flags: uint32At(payload, selectorOffset), SelectFlags: uint32At(payload, selectorOffset+4), Capability: fourCC(payload[selectorOffset+8 : selectorOffset+12])}
	}
	archetype := uint32At(payload, offset+0x34)
	if archetype == ^uint32(0) {
		event.Archetype = "default"
	} else if archetype != 0 {
		event.Archetype = fourCC(payload[offset+0x34 : offset+0x38])
	}
	event.EventGroup = uint32At(payload, offset+0x38)
	event.ID = uint32At(payload, offset+0x3C)
	nameOffset := uint32At(payload, offset+0x40)
	if event.ID != 0 {
		name, err := cString(payload, nameOffset)
		if err != nil {
			return Event{}, fmt.Errorf("name: %w", err)
		}
		event.Name = name
	}
	event.Parameter0 = uint32At(payload, offset+0x44)
	event.MaxSqrDist = float32At(payload, offset+0x48)
	event.Parameter1 = uint32At(payload, offset+0x4C)
	event.Predicate = Predicate{Flags: uint32At(payload, offset+0x50), State: uint32At(payload, offset+0x54)}
	return event, nil
}

func decodeKeyframe(payload []byte, componentType uint32) Keyframe {
	keyframe := Keyframe{}
	switch componentType {
	case 0:
		keyframe.Time = int32(uint32At(payload, 0))
		keyframe.EventStart = binary.LittleEndian.Uint16(payload[8:10])
		keyframe.EventCount = payload[10]
		keyframe.Flags = uint32At(payload, 12)
	case 1:
		for index := range keyframe.Position {
			keyframe.Position[index] = float32At(payload, index*4)
		}
		keyframe.Weight = float32At(payload, 12)
		keyframe.Interpolators = decodeInterpolators(payload[16:], 4)
	case 2:
		for index := range keyframe.Rotation {
			keyframe.Rotation[index] = float32At(payload, index*4)
		}
		keyframe.Weight = float32At(payload, 16)
		keyframe.NextMode = payload[28]
		keyframe.PreviousMode = payload[29]
		keyframe.Interpolators = decodeInterpolators(payload[0x54:], 1)
	case 3:
		keyframe.Scalar = float32At(payload, 0)
		keyframe.Weight = float32At(payload, 4)
		keyframe.Interpolators = decodeInterpolators(payload[8:], 2)
	}
	return keyframe
}

func decodeInterpolators(payload []byte, count int) []Interpolator {
	interpolators := make([]Interpolator, count)
	for index := range interpolators {
		offset := index * 16
		interpolators[index] = Interpolator{NextFactor: float32At(payload, offset), PreviousFactor: float32At(payload, offset+4), NextMode: payload[offset+8], PreviousMode: payload[offset+9]}
	}
	return interpolators
}

func keyframeSize(componentType uint32) (uint32, error) {
	switch componentType {
	case 0:
		return 20, nil
	case 1:
		return 80, nil
	case 2:
		return 100, nil
	case 3:
		return 40, nil
	default:
		return 0, fmt.Errorf("type: %d", componentType)
	}
}

func boundedArray(payload []byte, offset, count, size uint32, name string) error {
	end := uint64(offset) + uint64(count)*uint64(size)
	if uint64(offset) > uint64(len(payload)) || end > uint64(len(payload)) {
		return fmt.Errorf("%sRange: %d:%d exceeds %d", name, offset, end, len(payload))
	}
	return nil
}

func uint32At(payload []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(payload[offset : offset+4])
}
func float32At(payload []byte, offset int) float32 {
	return math.Float32frombits(uint32At(payload, offset))
}

func fixedString(payload []byte) string {
	end := len(payload)
	if index := strings.IndexByte(string(payload), 0); index >= 0 {
		end = index
	}
	return string(payload[:end])
}

func fourCC(payload []byte) string { return strings.TrimRight(string(payload), "\x00") }

func cString(payload []byte, offset uint32) (string, error) {
	if offset >= uint32(len(payload)) {
		return "", fmt.Errorf("offset: %d exceeds %d", offset, len(payload))
	}
	end := int(offset)
	for end < len(payload) && payload[end] != 0 {
		end++
	}
	if end == len(payload) {
		return "", errors.New("terminator: missing")
	}
	return string(payload[offset:end]), nil
}

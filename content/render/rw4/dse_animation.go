package rw4

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func (e *Document) writeDSEKeyframeAnimation(writer *bufio.Writer, section Section) error {
	animation := e.KeyframeAnimations[section.Ordinal]
	_, err := fmt.Fprintf(writer, "\t\t\tSKELETONID 0x%08X\n\t\t\tFIELDC 0x%08X\n\t\t\tFIELD1C 0x%08X\n\t\t\tLENGTH %s\n\t\t\tFIELD24 %d\n\t\t\tFLAGS 0x%08X\n\t\t\tNUMCHANNELS %d\n", animation.SkeletonID, animation.FieldC, animation.Field1C, strconv.FormatFloat(float64(animation.Length), 'g', -1, 32), animation.Field24, animation.Flags, len(animation.Channels))
	for channelIndex, channel := range animation.Channels {
		if err != nil {
			break
		}
		declaration := animationChannelDeclaration(channel.Components)
		_, err = fmt.Fprintf(writer, "\t\t\t\t%s %q\n\t\t\t\t\tNAMEID 0x%08X\n\t\t\t\t\tNUMKEYFRAMES %d\n", declaration, fmt.Sprintf("bone%d", channelIndex), channel.NameID, len(channel.Keyframes))
		for frameIndex, frame := range channel.Keyframes {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(writer, "\t\t\t\t\t\tKEYFRAME %q\n\t\t\t\t\t\t\tTIME %s\n", fmt.Sprintf("keyframe%d", frameIndex), strconv.FormatFloat(float64(frame.Time), 'g', -1, 32))
			if channel.Components == animationBlendFactor {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t\tFACTOR %s\n", strconv.FormatFloat(float64(frame.Factor), 'g', -1, 32))
				continue
			}
			_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t\tROTATION %q\n\t\t\t\t\t\t\tTRANSLATION %q\n", float32ArrayText(frame.Rotation[:]), float32ArrayText(frame.Translation[:]))
			if channel.Components == animationLocRotScale && err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t\tSCALE %q\n\t\t\t\t\t\t\tFIELD28 0x%08X\n", float32ArrayText(frame.Scale[:]), frame.Field28)
			}
		}
	}
	return dseWriteError("keyframeAnimation", err)
}

func animationChannelDeclaration(components uint32) string {
	switch components {
	case animationBlendFactor:
		return "BLENDFACTORCHANNEL"
	case animationLocRot:
		return "LOCROTCHANNEL"
	case animationLocRotScale:
		return "LOCROTSCALECHANNEL"
	default:
		return "UNKNOWNCHANNEL"
	}
}

func float32ArrayText(numbers []float32) string {
	text := ""
	for index, number := range numbers {
		if index != 0 {
			text += " "
		}
		text += strconv.FormatFloat(float64(number), 'g', -1, 32)
	}
	return text
}

func readDSEKeyframeAnimation(parser *rw4Parser, section Section) ([]byte, error) {
	skeletonID, err := parser.uint32("SKELETONID")
	fieldC, err := parser.nextUint32("FIELDC", err)
	field1C, err := parser.nextUint32("FIELD1C", err)
	lengthFields, err := parser.nextProperty("LENGTH", err)
	field24, err := parser.nextUint32("FIELD24", err)
	flags, err := parser.nextUint32("FLAGS", err)
	channelCount, err := parser.nextCount("NUMCHANNELS", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	length, err := strconv.ParseFloat(lengthFields[0], 32)
	if err != nil {
		return nil, fmt.Errorf("length: %w", err)
	}
	animation := KeyframeAnimation{SkeletonID: skeletonID, FieldC: fieldC, Field1C: field1C, Length: float32(length), Field24: field24, Flags: flags, Channels: make([]AnimationChannel, channelCount)}
	for channelIndex := range animation.Channels {
		declarationFields, readErr := parser.next()
		if readErr != nil || len(declarationFields) != 2 || declarationFields[1] != fmt.Sprintf("bone%d", channelIndex) {
			return nil, fmt.Errorf("channel[%d]: %v: %v", channelIndex, declarationFields, readErr)
		}
		declaration := declarationFields[0]
		components, poseSize := animationDeclarationComponents(declaration)
		if poseSize == 0 {
			return nil, fmt.Errorf("channel[%d]Declaration: %q", channelIndex, declaration)
		}
		nameID, readErr := parser.uint32("NAMEID")
		frameCount, readErr := parser.nextCount("NUMKEYFRAMES", readErr)
		if readErr != nil {
			return nil, fmt.Errorf("channel[%d]Header: %w", channelIndex, readErr)
		}
		channel := AnimationChannel{NameID: nameID, Components: components, PoseSize: poseSize, Keyframes: make([]AnimationKeyframe, frameCount)}
		for frameIndex := range channel.Keyframes {
			frameName, frameErr := parser.property("KEYFRAME", 1)
			if frameErr != nil || frameName[0] != fmt.Sprintf("keyframe%d", frameIndex) {
				return nil, fmt.Errorf("channel[%d]Frame[%d]: %v: %v", channelIndex, frameIndex, frameName, frameErr)
			}
			timeFields, frameErr := parser.property("TIME", 1)
			if frameErr != nil {
				return nil, fmt.Errorf("channel[%d]Frame[%d]Time: %w", channelIndex, frameIndex, frameErr)
			}
			frameTime, frameErr := strconv.ParseFloat(timeFields[0], 32)
			if frameErr != nil {
				return nil, fmt.Errorf("channel[%d]Frame[%d]TimeParse: %w", channelIndex, frameIndex, frameErr)
			}
			frame := AnimationKeyframe{Time: float32(frameTime), Rotation: [4]float32{0, 0, 0, 1}, Scale: [3]float32{1, 1, 1}}
			if components == animationBlendFactor {
				factorFields, factorErr := parser.property("FACTOR", 1)
				if factorErr != nil {
					return nil, fmt.Errorf("channel[%d]Frame[%d]Factor: %w", channelIndex, frameIndex, factorErr)
				}
				factor, factorErr := strconv.ParseFloat(factorFields[0], 32)
				if factorErr != nil {
					return nil, fmt.Errorf("channel[%d]Frame[%d]FactorParse: %w", channelIndex, frameIndex, factorErr)
				}
				frame.Factor = float32(factor)
			} else {
				rotationFields, fieldErr := parser.property("ROTATION", 1)
				translationFields, fieldErr := parser.nextProperty("TRANSLATION", fieldErr)
				if fieldErr != nil {
					return nil, fmt.Errorf("channel[%d]Frame[%d]Transform: %w", channelIndex, frameIndex, fieldErr)
				}
				fieldErr = parseFloat32Array(frame.Rotation[:], rotationFields[0])
				if fieldErr == nil {
					fieldErr = parseFloat32Array(frame.Translation[:], translationFields[0])
				}
				if components == animationLocRotScale {
					scaleFields, scaleErr := parser.property("SCALE", 1)
					field28, scaleErr := parser.nextUint32("FIELD28", scaleErr)
					if scaleErr == nil {
						scaleErr = parseFloat32Array(frame.Scale[:], scaleFields[0])
					}
					if scaleErr != nil {
						return nil, fmt.Errorf("channel[%d]Frame[%d]Scale: %w", channelIndex, frameIndex, scaleErr)
					}
					frame.Field28 = field28
				}
				if fieldErr != nil {
					return nil, fmt.Errorf("channel[%d]Frame[%d]TransformParse: %w", channelIndex, frameIndex, fieldErr)
				}
			}
			channel.Keyframes[frameIndex] = frame
		}
		animation.Channels[channelIndex] = channel
	}
	return encodeKeyframeAnimation(section, animation)
}

func animationDeclarationComponents(declaration string) (uint32, uint32) {
	switch declaration {
	case "BLENDFACTORCHANNEL":
		return animationBlendFactor, 8
	case "LOCROTCHANNEL":
		return animationLocRot, 32
	case "LOCROTSCALECHANNEL":
		return animationLocRotScale, 48
	default:
		return 0, 0
	}
}

func parseFloat32Array(numbers []float32, text string) error {
	fields := strings.Fields(text)
	if len(fields) != len(numbers) {
		return fmt.Errorf("componentCount: got %d, want %d", len(fields), len(numbers))
	}
	for index, field := range fields {
		number, err := strconv.ParseFloat(field, 32)
		if err != nil {
			return fmt.Errorf("component[%d]: %w", index, err)
		}
		numbers[index] = float32(number)
	}
	return nil
}

func encodeKeyframeAnimation(section Section, animation KeyframeAnimation) ([]byte, error) {
	if uint64(section.Size) > uint64(math.MaxInt) || section.Size < 48 {
		return nil, fmt.Errorf("sectionSize: invalid animation size %d", section.Size)
	}
	if section.Offset > math.MaxUint32-section.Size {
		return nil, fmt.Errorf("sectionEnd: offset %d size %d exceed uint32", section.Offset, section.Size)
	}
	if uint64(len(animation.Channels)) > (uint64(section.Size)-48)/16 {
		return nil, fmt.Errorf("channelCount: %d exceeds section size", len(animation.Channels))
	}
	nameOffset := 48
	infoOffset := nameOffset + len(animation.Channels)*4
	dataOffset := infoOffset + len(animation.Channels)*12
	required := dataOffset
	for _, channel := range animation.Channels {
		if channel.PoseSize != 8 && channel.PoseSize != 32 && channel.PoseSize != 48 {
			return nil, fmt.Errorf("poseSize: invalid size %d", channel.PoseSize)
		}
		if uint64(len(channel.Keyframes)) > (uint64(section.Size)-uint64(required))/uint64(channel.PoseSize) {
			return nil, fmt.Errorf("frameSize: keyframes exceed section size %d", section.Size)
		}
		required += len(channel.Keyframes) * int(channel.PoseSize)
	}
	payload := make([]byte, section.Size)
	binary.LittleEndian.PutUint32(payload[0:4], section.Offset+uint32(nameOffset))
	binary.LittleEndian.PutUint32(payload[4:8], uint32(len(animation.Channels)))
	binary.LittleEndian.PutUint32(payload[8:12], animation.SkeletonID)
	binary.LittleEndian.PutUint32(payload[12:16], animation.FieldC)
	binary.LittleEndian.PutUint32(payload[16:20], section.Offset+uint32(dataOffset))
	binary.LittleEndian.PutUint32(payload[20:24], section.Offset+section.Size)
	binary.LittleEndian.PutUint32(payload[24:28], uint32(len(animation.Channels)))
	binary.LittleEndian.PutUint32(payload[28:32], animation.Field1C)
	binary.LittleEndian.PutUint32(payload[32:36], math.Float32bits(animation.Length))
	binary.LittleEndian.PutUint32(payload[36:40], animation.Field24)
	binary.LittleEndian.PutUint32(payload[40:44], animation.Flags)
	binary.LittleEndian.PutUint32(payload[44:48], section.Offset+uint32(infoOffset))
	position := dataOffset
	for channelIndex, channel := range animation.Channels {
		binary.LittleEndian.PutUint32(payload[nameOffset+channelIndex*4:nameOffset+channelIndex*4+4], channel.NameID)
		info := infoOffset + channelIndex*12
		binary.LittleEndian.PutUint32(payload[info:info+4], uint32(position))
		binary.LittleEndian.PutUint32(payload[info+4:info+8], channel.PoseSize)
		binary.LittleEndian.PutUint32(payload[info+8:info+12], channel.Components)
		for _, frame := range channel.Keyframes {
			writeAnimationFrame(payload[position:position+int(channel.PoseSize)], channel.Components, frame)
			position += int(channel.PoseSize)
		}
	}
	return payload, nil
}

func writeAnimationFrame(payload []byte, components uint32, frame AnimationKeyframe) {
	if components == animationBlendFactor {
		binary.LittleEndian.PutUint32(payload[0:4], math.Float32bits(frame.Factor))
		binary.LittleEndian.PutUint32(payload[4:8], math.Float32bits(frame.Time))
		return
	}
	for index, number := range frame.Rotation {
		binary.LittleEndian.PutUint32(payload[index*4:index*4+4], math.Float32bits(number))
	}
	for index, number := range frame.Translation {
		binary.LittleEndian.PutUint32(payload[16+index*4:20+index*4], math.Float32bits(number))
	}
	if components == animationLocRot {
		binary.LittleEndian.PutUint32(payload[28:32], math.Float32bits(frame.Time))
		return
	}
	for index, number := range frame.Scale {
		binary.LittleEndian.PutUint32(payload[28+index*4:32+index*4], math.Float32bits(number))
	}
	binary.LittleEndian.PutUint32(payload[40:44], frame.Field28)
	binary.LittleEndian.PutUint32(payload[44:48], math.Float32bits(frame.Time))
}

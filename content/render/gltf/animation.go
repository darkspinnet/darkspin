package gltf

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	compiled "github.com/darkspinnet/darkspin/content/animation"
)

func WriteAnimationPath(destinationPath string, source *compiled.Document) error {
	if source == nil || len(source.Channels) == 0 {
		return errors.New("animationMissing")
	}
	if !strings.EqualFold(filepath.Ext(destinationPath), ".gltf") {
		return errors.New("destinationExtension: want .gltf")
	}
	binaryPath := strings.TrimSuffix(destinationPath, filepath.Ext(destinationPath)) + ".bin"
	for _, outputPath := range []string{destinationPath, binaryPath} {
		_, err := os.Stat(outputPath)
		if err == nil {
			return fmt.Errorf("destinationExists: %q", outputPath)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("destinationStat: %w", err)
		}
	}
	document, contents, err := buildAnimation(filepath.Base(binaryPath), source)
	if err != nil {
		return fmt.Errorf("documentBuild: %w", err)
	}
	jsonContents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonMarshal: %w", err)
	}
	jsonContents = append(jsonContents, '\n')
	err = writeExclusive(binaryPath, contents)
	if err != nil {
		return fmt.Errorf("bufferWrite: %w", err)
	}
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.Remove(binaryPath)
		}
	}()
	err = writeExclusive(destinationPath, jsonContents)
	if err != nil {
		return fmt.Errorf("jsonWrite: %w", err)
	}
	isComplete = true
	return nil
}

func buildAnimation(binaryName string, source *compiled.Document) (document, []byte, error) {
	document := document{Asset: asset{Version: "2.0", Generator: "darkrun gltf prototype"}, Scene: 0, Scenes: []scene{{Nodes: make([]int, len(source.Channels))}}}
	for channelIndex, channel := range source.Channels {
		document.Scenes[0].Nodes[channelIndex] = channelIndex
		document.Nodes = append(document.Nodes, node{Name: channel.Name})
	}
	clip := animationDef{Name: animationName(source), Extras: animationExtras(source)}
	contents := &bytes.Buffer{}
	for channelIndex, channel := range source.Channels {
		times := animationTimes(source, channel)
		if len(times) == 0 {
			continue
		}
		timeAccessor, err := appendScalars(contents, &document, times)
		if err != nil {
			return document, nil, fmt.Errorf("channel[%d]Times: %w", channelIndex, err)
		}
		for componentIndex, component := range channel.Components {
			switch component.Type() {
			case 1:
				positions := make([][3]float32, len(component.Keyframes))
				for frameIndex, keyframe := range component.Keyframes {
					positions[frameIndex] = keyframe.Position
				}
				outputAccessor, appendErr := appendAnimationFloat3(contents, &document, positions)
				if appendErr != nil {
					return document, nil, fmt.Errorf("channel[%d]Position: %w", channelIndex, appendErr)
				}
				appendAnimationCurve(&clip, channelIndex, timeAccessor, outputAccessor, "translation", component)
			case 2:
				rotations := make([][4]float32, len(component.Keyframes))
				for frameIndex, keyframe := range component.Keyframes {
					rotations[frameIndex] = normalizedQuaternion(keyframe.Rotation)
				}
				outputAccessor, appendErr := appendAnimationFloat4(contents, &document, rotations)
				if appendErr != nil {
					return document, nil, fmt.Errorf("channel[%d]Rotation: %w", channelIndex, appendErr)
				}
				appendAnimationCurve(&clip, channelIndex, timeAccessor, outputAccessor, "rotation", component)
			case 0, 3:
			default:
				return document, nil, fmt.Errorf("channel[%d]Component[%d]: unsupported type %d", channelIndex, componentIndex, component.Type())
			}
		}
	}
	if len(clip.Channels) == 0 {
		return document, nil, errors.New("curveMissing")
	}
	document.Animations = []animationDef{clip}
	document.Buffers = []buffer{{URI: binaryName, ByteLength: contents.Len()}}
	return document, contents.Bytes(), nil
}

func appendAnimationCurve(clip *animationDef, nodeIndex, inputAccessor, outputAccessor int, path string, component compiled.Component) {
	samplerIndex := len(clip.Samplers)
	clip.Samplers = append(clip.Samplers, animationSampler{Input: inputAccessor, Interpolation: "LINEAR", Output: outputAccessor})
	clip.Channels = append(clip.Channels, animationChannel{Sampler: samplerIndex, Target: animationTarget{Node: nodeIndex, Path: path}, Extras: map[string]any{"componentFlags": fmt.Sprintf("0x%08X", component.Flags), "componentId": fmt.Sprintf("0x%08X", component.ID), "componentIndex": component.Index}})
}

func animationTimes(source *compiled.Document, channel compiled.Channel) []float32 {
	for _, component := range channel.Components {
		if component.Type() != 0 || len(component.Keyframes) != int(channel.KeyframeCount) {
			continue
		}
		times := make([]float32, len(component.Keyframes))
		for frameIndex, keyframe := range component.Keyframes {
			times[frameIndex] = float32(keyframe.Time) * source.FrameStep
		}
		return times
	}
	times := make([]float32, channel.KeyframeCount)
	for frameIndex := range times {
		times[frameIndex] = float32(frameIndex) * source.FrameStep
	}
	return times
}

func animationName(source *compiled.Document) string {
	name := strings.TrimSuffix(filepath.Base(filepath.ToSlash(source.Source)), filepath.Ext(source.Source))
	if name == "" {
		return fmt.Sprintf("animation_%08x", source.ResourceID)
	}
	return name
}

func animationExtras(source *compiled.Document) map[string]any {
	events := make([]map[string]any, len(source.Events))
	for eventIndex, event := range source.Events {
		events[eventIndex] = map[string]any{"name": event.Name, "id": fmt.Sprintf("0x%08X", event.ID), "flags": fmt.Sprintf("0x%08X", event.Flags), "parameter0": fmt.Sprintf("0x%08X", event.Parameter0), "parameter1": fmt.Sprintf("0x%08X", event.Parameter1)}
	}
	return map[string]any{"source": source.Source, "resourceId": fmt.Sprintf("0x%08X", source.ResourceID), "formatVersion": source.FormatVersion, "frameStep": source.FrameStep, "lengthFrames": source.Length, "events": events, "channelBinding": "procedural Game selectors retained as channel names and extras; retargeting requires creature rig capabilities"}
}

func appendScalars(contents *bytes.Buffer, document *document, numbers []float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, err
	}
	start := contents.Len()
	minimum, maximum := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for _, number := range numbers {
		err = binary.Write(contents, binary.LittleEndian, number)
		if err != nil {
			return 0, fmt.Errorf("numberWrite: %w", err)
		}
		if number < minimum {
			minimum = number
		}
		if number > maximum {
			maximum = number
		}
	}
	return addAccessor(document, start, contents.Len()-start, 0, componentFloat, len(numbers), "SCALAR", []float32{minimum}, []float32{maximum}), nil
}

func appendAnimationFloat3(contents *bytes.Buffer, document *document, tuples [][3]float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, err
	}
	start := contents.Len()
	for _, tuple := range tuples {
		for _, number := range tuple {
			err = binary.Write(contents, binary.LittleEndian, number)
			if err != nil {
				return 0, fmt.Errorf("numberWrite: %w", err)
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, 0, componentFloat, len(tuples), "VEC3", nil, nil), nil
}

func appendAnimationFloat4(contents *bytes.Buffer, document *document, tuples [][4]float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, err
	}
	start := contents.Len()
	for _, tuple := range tuples {
		for _, number := range tuple {
			err = binary.Write(contents, binary.LittleEndian, number)
			if err != nil {
				return 0, fmt.Errorf("numberWrite: %w", err)
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, 0, componentFloat, len(tuples), "VEC4", nil, nil), nil
}

func normalizedQuaternion(rotation [4]float32) [4]float32 {
	length := float32(math.Sqrt(float64(rotation[0]*rotation[0] + rotation[1]*rotation[1] + rotation[2]*rotation[2] + rotation[3]*rotation[3])))
	if length == 0 {
		return [4]float32{0, 0, 0, 1}
	}
	for index := range rotation {
		rotation[index] /= length
	}
	return rotation
}

package rw4

import (
	"fmt"
	"math"
	"sort"
)

const (
	animationBlendFactor = 0x100
	animationLocRot      = 0x101
	animationLocRotScale = 0x601
)

func decodeAnimationMap(payload []byte) (AnimationMap, error) {
	if len(payload) < 8 {
		return AnimationMap{}, fmt.Errorf("size: got %d", len(payload))
	}
	count := readUint32(payload, 4)
	if len(payload) != 8+int(count)*8 {
		return AnimationMap{}, fmt.Errorf("size: got %d, want %d", len(payload), 8+int(count)*8)
	}
	animationMap := AnimationMap{SelfReference: readUint32(payload, 0), Entries: make([]AnimationMapEntry, count)}
	for index := range animationMap.Entries {
		offset := 8 + index*8
		animationMap.Entries[index] = AnimationMapEntry{ID: readUint32(payload, offset), ResourceRef: readUint32(payload, offset+4)}
	}
	return animationMap, nil
}

func decodeKeyframeAnimation(section Section) (KeyframeAnimation, error) {
	payload := section.Data
	if len(payload) < 48 {
		return KeyframeAnimation{}, fmt.Errorf("size: got %d", len(payload))
	}
	count := readUint32(payload, 4)
	if readUint32(payload, 24) != count {
		return KeyframeAnimation{}, fmt.Errorf("repeatedCount: got %d, want %d", readUint32(payload, 24), count)
	}
	localPointer := func(pointer uint32, name string, size uint64) (int, error) {
		if pointer < section.Offset {
			return 0, fmt.Errorf("%sPointer: %d precedes %d", name, pointer, section.Offset)
		}
		offset := uint64(pointer - section.Offset)
		if offset+size > uint64(len(payload)) {
			return 0, fmt.Errorf("%sRange: %d:%d exceeds %d", name, offset, offset+size, len(payload))
		}
		return int(offset), nil
	}
	nameOffset, err := localPointer(readUint32(payload, 0), "name", uint64(count)*4)
	if err != nil {
		return KeyframeAnimation{}, err
	}
	infoOffset, err := localPointer(readUint32(payload, 44), "info", uint64(count)*12)
	if err != nil {
		return KeyframeAnimation{}, err
	}
	paddingEnd, err := localPointer(readUint32(payload, 20), "paddingEnd", 0)
	if err != nil {
		return KeyframeAnimation{}, err
	}
	animation := KeyframeAnimation{
		SkeletonID: readUint32(payload, 8), FieldC: readUint32(payload, 12), Field1C: readUint32(payload, 28),
		Length: float32At(payload, 32), Field24: readUint32(payload, 36), Flags: readUint32(payload, 40),
		Channels: make([]AnimationChannel, count),
	}
	positions := make([]int, count)
	for index := range animation.Channels {
		info := infoOffset + index*12
		position := int(int32(readUint32(payload, info)))
		if position < 0 || position > len(payload) {
			return KeyframeAnimation{}, fmt.Errorf("channel[%d]Position: %d", index, position)
		}
		poseSize := readUint32(payload, info+4)
		components := readUint32(payload, info+8)
		expectedSize := animationPoseSize(components)
		if expectedSize == 0 || poseSize != expectedSize {
			return KeyframeAnimation{}, fmt.Errorf("channel[%d]Pose: components 0x%X size %d", index, components, poseSize)
		}
		positions[index] = position
		animation.Channels[index] = AnimationChannel{NameID: readUint32(payload, nameOffset+index*4), Components: components, PoseSize: poseSize}
	}
	for index := range animation.Channels {
		end := paddingEnd
		if index+1 < len(positions) {
			end = positions[index+1]
		}
		if end < positions[index] {
			return KeyframeAnimation{}, fmt.Errorf("channel[%d]Range: %d:%d", index, positions[index], end)
		}
		animation.Channels[index].Keyframes, err = decodeAnimationFrames(payload, positions[index], end, animation.Channels[index])
		if err != nil {
			return KeyframeAnimation{}, fmt.Errorf("channel[%d]: %w", index, err)
		}
	}
	return animation, nil
}

func animationPoseSize(components uint32) uint32 {
	switch components {
	case animationBlendFactor:
		return 8
	case animationLocRot:
		return 32
	case animationLocRotScale:
		return 48
	default:
		return 0
	}
}

func decodeAnimationFrames(payload []byte, start, end int, channel AnimationChannel) ([]AnimationKeyframe, error) {
	poseSize := int(channel.PoseSize)
	frames := make([]AnimationKeyframe, 0)
	lastTime := float32(-1)
	for offset := start; offset+poseSize <= end; offset += poseSize {
		frame := AnimationKeyframe{Rotation: [4]float32{0, 0, 0, 1}, Scale: [3]float32{1, 1, 1}}
		switch channel.Components {
		case animationBlendFactor:
			frame.Factor = float32At(payload, offset)
			frame.Time = float32At(payload, offset+4)
		case animationLocRot, animationLocRotScale:
			for index := range frame.Rotation {
				frame.Rotation[index] = float32At(payload, offset+index*4)
			}
			for index := range frame.Translation {
				frame.Translation[index] = float32At(payload, offset+16+index*4)
			}
			if channel.Components == animationLocRotScale {
				for index := range frame.Scale {
					frame.Scale[index] = float32At(payload, offset+28+index*4)
				}
				frame.Field28 = readUint32(payload, offset+40)
				frame.Time = float32At(payload, offset+44)
			} else {
				frame.Time = float32At(payload, offset+28)
			}
		}
		if math.IsNaN(float64(frame.Time)) || math.IsInf(float64(frame.Time), 0) || frame.Time < lastTime {
			break
		}
		lastTime = frame.Time
		frames = append(frames, frame)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("keyframeMissing: range %d:%d", start, end)
	}
	return frames, nil
}

type RigAnimation struct {
	ID       uint32
	Length   float32
	Times    []float32
	Channels [][]RigTransform
}

type RigTransform struct {
	Translation [3]float32
	Rotation    [4]float32
	Scale       [3]float32
}

func (e *Document) rigAnimations(rig *Rig) ([]RigAnimation, error) {
	idsByOrdinal := make(map[int]uint32)
	for _, animationMap := range e.AnimationMaps {
		for _, entry := range animationMap.Entries {
			ordinal, err := e.directOrdinal(entry.ResourceRef)
			if err != nil {
				return nil, fmt.Errorf("mapReference: %w", err)
			}
			idsByOrdinal[ordinal] = entry.ID
		}
	}
	ordinals := make([]int, 0, len(e.KeyframeAnimations))
	for ordinal := range e.KeyframeAnimations {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	animations := make([]RigAnimation, 0, len(ordinals))
	for _, ordinal := range ordinals {
		animation := e.KeyframeAnimations[ordinal]
		if animation.SkeletonID != rig.Skeleton.ID || len(animation.Channels) != len(rig.Skeleton.Names) {
			continue
		}
		projected, err := projectRigAnimation(idsByOrdinal[ordinal], animation, rig)
		if err != nil {
			// Morph-factor animations remain authoritative in DSE but cannot target
			// glTF joints. Keep the cohesive mesh/skin instead of discarding it.
			continue
		}
		animations = append(animations, projected)
	}
	return animations, nil
}

func projectRigAnimation(id uint32, animation KeyframeAnimation, rig *Rig) (RigAnimation, error) {
	timeSet := make(map[uint32]float32)
	for _, channel := range animation.Channels {
		if channel.Components == animationBlendFactor {
			return RigAnimation{}, fmt.Errorf("blendFactor: skeletal rig contains morph channel")
		}
		for _, frame := range channel.Keyframes {
			timeSet[math.Float32bits(frame.Time)] = frame.Time
		}
	}
	times := make([]float32, 0, len(timeSet))
	for _, frameTime := range timeSet {
		times = append(times, frameTime)
	}
	sort.Slice(times, func(left, right int) bool { return times[left] < times[right] })
	projected := RigAnimation{ID: id, Length: animation.Length, Times: times, Channels: make([][]RigTransform, len(animation.Channels))}
	for channelIndex := range projected.Channels {
		projected.Channels[channelIndex] = make([]RigTransform, len(times))
	}
	heads := rigHeads(rig.RestPoses)
	for timeIndex, frameTime := range times {
		globals, err := animationGlobals(animation, rig, heads, frameTime)
		if err != nil {
			return RigAnimation{}, fmt.Errorf("time[%d]: %w", timeIndex, err)
		}
		for boneIndex, global := range globals {
			local := global
			parent := rig.Skeleton.Parents[boneIndex]
			if parent >= 0 {
				parentInverse, inverseErr := inverseAffine(globals[parent])
				if inverseErr != nil {
					return RigAnimation{}, fmt.Errorf("parent[%d]: %w", boneIndex, inverseErr)
				}
				local = multiplyMatrix(parentInverse, global)
			}
			projected.Channels[boneIndex][timeIndex], err = decomposeTransform(local)
			if err != nil {
				return RigAnimation{}, fmt.Errorf("bone[%d]: %w", boneIndex, err)
			}
		}
	}
	return projected, nil
}

func rigHeads(poses []BonePose) [][3]float32 {
	heads := make([][3]float32, len(poses))
	for index, pose := range poses {
		for row := 0; row < 3; row++ {
			heads[index][row] = -(pose.Rotation[row*3]*pose.Translation[0] + pose.Rotation[row*3+1]*pose.Translation[1] + pose.Rotation[row*3+2]*pose.Translation[2])
		}
	}
	return heads
}

func animationGlobals(animation KeyframeAnimation, rig *Rig, heads [][3]float32, frameTime float32) ([][16]float32, error) {
	globals := make([][16]float32, len(animation.Channels))
	branches := make([][3][16]float32, 0)
	parentRotation := identityMatrix()
	parentLocation := [3]float32{}
	parentScale := [3]float32{1, 1, 1}
	for boneIndex, channel := range animation.Channels {
		pose := interpolateFrame(channel.Keyframes, frameTime)
		rotation := quaternionMatrix(pose.Rotation)
		for row := 0; row < 3; row++ {
			for column := 0; column < 3; column++ {
				rotation[row*4+column] *= pose.Scale[column] / parentScale[column]
			}
		}
		matrix := multiplyMatrix(parentRotation, rotation)
		location := transformPoint3(parentRotation, pose.Translation)
		for index := range location {
			location[index] += parentLocation[index]
		}
		skinRotation := bonePoseMatrix(rig.RestPoses[boneIndex])
		skinInverse, err := inverseAffine(skinRotation)
		if err != nil {
			return nil, fmt.Errorf("skinInverse[%d]: %w", boneIndex, err)
		}
		delta := multiplyMatrix(matrix, skinInverse)
		skinTranslation := transformVector3(matrix, rig.RestPoses[boneIndex].Translation)
		for index := 0; index < 3; index++ {
			delta[index*4+3] = location[index] + skinTranslation[index]
		}
		global := multiplyMatrix(delta, translationMatrix(heads[boneIndex]))
		globals[boneIndex] = global
		switch rig.Skeleton.Flags[boneIndex] {
		case 0:
			parentRotation, parentLocation, parentScale = matrix, location, pose.Scale
		case 1:
			if len(branches) != 0 {
				branch := branches[len(branches)-1]
				branches = branches[:len(branches)-1]
				parentRotation, parentLocation = branch[0], [3]float32{branch[1][3], branch[1][7], branch[1][11]}
				parentScale = [3]float32{branch[2][0], branch[2][5], branch[2][10]}
			}
		case 2:
			branches = append(branches, [3][16]float32{parentRotation, translationMatrix(parentLocation), scaleMatrix(parentScale)})
			parentRotation, parentLocation, parentScale = matrix, location, pose.Scale
		case 3:
		default:
			return nil, fmt.Errorf("boneFlag[%d]: %d", boneIndex, rig.Skeleton.Flags[boneIndex])
		}
	}
	return globals, nil
}

func interpolateFrame(frames []AnimationKeyframe, frameTime float32) AnimationKeyframe {
	if frameTime <= frames[0].Time {
		return frames[0]
	}
	for index := 1; index < len(frames); index++ {
		if frameTime > frames[index].Time {
			continue
		}
		left, right := frames[index-1], frames[index]
		amount := (frameTime - left.Time) / (right.Time - left.Time)
		result := left
		for component := range result.Translation {
			result.Translation[component] += (right.Translation[component] - left.Translation[component]) * amount
			result.Scale[component] += (right.Scale[component] - left.Scale[component]) * amount
		}
		result.Rotation = slerp(left.Rotation, right.Rotation, amount)
		result.Time = frameTime
		return result
	}
	return frames[len(frames)-1]
}

func slerp(left, right [4]float32, amount float32) [4]float32 {
	dot := left[0]*right[0] + left[1]*right[1] + left[2]*right[2] + left[3]*right[3]
	if dot < 0 {
		for index := range right {
			right[index] = -right[index]
		}
		dot = -dot
	}
	if dot > 0.9995 {
		result := [4]float32{}
		for index := range result {
			result[index] = left[index] + amount*(right[index]-left[index])
		}
		return normalizeQuaternion(result)
	}
	theta := float32(math.Acos(float64(dot)))
	sine := float32(math.Sin(float64(theta)))
	leftWeight := float32(math.Sin(float64((1-amount)*theta))) / sine
	rightWeight := float32(math.Sin(float64(amount*theta))) / sine
	return [4]float32{left[0]*leftWeight + right[0]*rightWeight, left[1]*leftWeight + right[1]*rightWeight, left[2]*leftWeight + right[2]*rightWeight, left[3]*leftWeight + right[3]*rightWeight}
}

func normalizeQuaternion(rotation [4]float32) [4]float32 {
	length := float32(math.Sqrt(float64(rotation[0]*rotation[0] + rotation[1]*rotation[1] + rotation[2]*rotation[2] + rotation[3]*rotation[3])))
	if length == 0 {
		return [4]float32{0, 0, 0, 1}
	}
	for index := range rotation {
		rotation[index] /= length
	}
	return rotation
}

func identityMatrix() [16]float32 { return [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }
func translationMatrix(translation [3]float32) [16]float32 {
	matrix := identityMatrix()
	matrix[3], matrix[7], matrix[11] = translation[0], translation[1], translation[2]
	return matrix
}
func scaleMatrix(scale [3]float32) [16]float32 {
	matrix := identityMatrix()
	matrix[0], matrix[5], matrix[10] = scale[0], scale[1], scale[2]
	return matrix
}
func bonePoseMatrix(pose BonePose) [16]float32 {
	matrix := identityMatrix()
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			matrix[row*4+column] = pose.Rotation[row*3+column]
		}
	}
	return matrix
}
func quaternionMatrix(rotation [4]float32) [16]float32 {
	q := normalizeQuaternion(rotation)
	x, y, z, w := q[0], q[1], q[2], q[3]
	return [16]float32{1 - 2*(y*y+z*z), 2 * (x*y - z*w), 2 * (x*z + y*w), 0, 2 * (x*y + z*w), 1 - 2*(x*x+z*z), 2 * (y*z - x*w), 0, 2 * (x*z - y*w), 2 * (y*z + x*w), 1 - 2*(x*x+y*y), 0, 0, 0, 0, 1}
}
func transformPoint3(matrix [16]float32, point [3]float32) [3]float32 {
	result := transformVector3(matrix, point)
	result[0] += matrix[3]
	result[1] += matrix[7]
	result[2] += matrix[11]
	return result
}
func transformVector3(matrix [16]float32, vector [3]float32) [3]float32 {
	return [3]float32{matrix[0]*vector[0] + matrix[1]*vector[1] + matrix[2]*vector[2], matrix[4]*vector[0] + matrix[5]*vector[1] + matrix[6]*vector[2], matrix[8]*vector[0] + matrix[9]*vector[1] + matrix[10]*vector[2]}
}

func inverseAffine(matrix [16]float32) ([16]float32, error) {
	a, b, c, d, e, f, g, h, i := matrix[0], matrix[1], matrix[2], matrix[4], matrix[5], matrix[6], matrix[8], matrix[9], matrix[10]
	determinant := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if math.Abs(float64(determinant)) < 1e-8 {
		return [16]float32{}, fmt.Errorf("singular")
	}
	inv := [16]float32{(e*i - f*h) / determinant, (c*h - b*i) / determinant, (b*f - c*e) / determinant, 0, (f*g - d*i) / determinant, (a*i - c*g) / determinant, (c*d - a*f) / determinant, 0, (d*h - e*g) / determinant, (b*g - a*h) / determinant, (a*e - b*d) / determinant, 0, 0, 0, 0, 1}
	t := [3]float32{matrix[3], matrix[7], matrix[11]}
	it := transformVector3(inv, t)
	inv[3], inv[7], inv[11] = -it[0], -it[1], -it[2]
	return inv, nil
}
func multiplyMatrix(left, right [16]float32) [16]float32 {
	result := [16]float32{}
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			for index := 0; index < 4; index++ {
				result[row*4+column] += left[row*4+index] * right[index*4+column]
			}
		}
	}
	return result
}

func decomposeTransform(matrix [16]float32) (RigTransform, error) {
	result := RigTransform{Translation: [3]float32{matrix[3], matrix[7], matrix[11]}}
	for column := 0; column < 3; column++ {
		result.Scale[column] = float32(math.Sqrt(float64(matrix[column]*matrix[column] + matrix[4+column]*matrix[4+column] + matrix[8+column]*matrix[8+column])))
		if result.Scale[column] == 0 {
			return RigTransform{}, fmt.Errorf("zeroScale[%d]", column)
		}
	}
	r00, r01, r02 := matrix[0]/result.Scale[0], matrix[1]/result.Scale[1], matrix[2]/result.Scale[2]
	r10, r11, r12 := matrix[4]/result.Scale[0], matrix[5]/result.Scale[1], matrix[6]/result.Scale[2]
	r20, r21, r22 := matrix[8]/result.Scale[0], matrix[9]/result.Scale[1], matrix[10]/result.Scale[2]
	trace := r00 + r11 + r22
	if trace > 0 {
		s := float32(math.Sqrt(float64(trace+1))) * 2
		result.Rotation = [4]float32{(r21 - r12) / s, (r02 - r20) / s, (r10 - r01) / s, s / 4}
	} else if r00 > r11 && r00 > r22 {
		s := float32(math.Sqrt(float64(1+r00-r11-r22))) * 2
		result.Rotation = [4]float32{s / 4, (r01 + r10) / s, (r02 + r20) / s, (r21 - r12) / s}
	} else if r11 > r22 {
		s := float32(math.Sqrt(float64(1+r11-r00-r22))) * 2
		result.Rotation = [4]float32{(r01 + r10) / s, s / 4, (r12 + r21) / s, (r02 - r20) / s}
	} else {
		s := float32(math.Sqrt(float64(1+r22-r00-r11))) * 2
		result.Rotation = [4]float32{(r02 + r20) / s, (r12 + r21) / s, s / 4, (r10 - r01) / s}
	}
	result.Rotation = normalizeQuaternion(result.Rotation)
	return result, nil
}

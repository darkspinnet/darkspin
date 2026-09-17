// Package gltf projects decoded Game render primitives into glTF 2.0.
package gltf

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/render/rw4"
)

const (
	componentFloat         = 5126
	componentUnsignedInt   = 5125
	componentUnsignedShort = 5123
	targetArrayBuffer      = 34962
	targetElementBuffer    = 34963
	primitiveTriangleMode  = 4
)

type document struct {
	Asset       asset          `json:"asset"`
	Scene       int            `json:"scene"`
	Scenes      []scene        `json:"scenes"`
	Nodes       []node         `json:"nodes"`
	Meshes      []mesh         `json:"meshes"`
	Buffers     []buffer       `json:"buffers"`
	BufferViews []bufferView   `json:"bufferViews"`
	Accessors   []accessor     `json:"accessors"`
	Images      []imageDef     `json:"images,omitempty"`
	Textures    []texture      `json:"textures,omitempty"`
	Materials   []material     `json:"materials,omitempty"`
	Skins       []skin         `json:"skins,omitempty"`
	Animations  []animationDef `json:"animations,omitempty"`
}

type asset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
}

type scene struct {
	Nodes []int `json:"nodes"`
}

type node struct {
	Name        string    `json:"name"`
	Mesh        *int      `json:"mesh,omitempty"`
	Skin        *int      `json:"skin,omitempty"`
	Children    []int     `json:"children,omitempty"`
	Matrix      []float32 `json:"matrix,omitempty"`
	Translation []float32 `json:"translation,omitempty"`
	Rotation    []float32 `json:"rotation,omitempty"`
	Scale       []float32 `json:"scale,omitempty"`
}

type skin struct {
	Name                string `json:"name"`
	InverseBindMatrices int    `json:"inverseBindMatrices"`
	Skeleton            *int   `json:"skeleton,omitempty"`
	Joints              []int  `json:"joints"`
}

type animationDef struct {
	Name     string             `json:"name"`
	Samplers []animationSampler `json:"samplers"`
	Channels []animationChannel `json:"channels"`
	Extras   map[string]any     `json:"extras,omitempty"`
}

type animationSampler struct {
	Input         int    `json:"input"`
	Interpolation string `json:"interpolation"`
	Output        int    `json:"output"`
}

type animationChannel struct {
	Sampler int             `json:"sampler"`
	Target  animationTarget `json:"target"`
	Extras  map[string]any  `json:"extras,omitempty"`
}

type animationTarget struct {
	Node int    `json:"node"`
	Path string `json:"path"`
}

type mesh struct {
	Name       string      `json:"name"`
	Primitives []primitive `json:"primitives"`
}

type primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Mode       int            `json:"mode"`
	Material   *int           `json:"material,omitempty"`
}

type imageDef struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

type texture struct {
	Name   string `json:"name"`
	Source int    `json:"source"`
}

type textureInfo struct {
	Index int `json:"index"`
}

type material struct {
	Name                 string               `json:"name"`
	PBRMetallicRoughness pbrMetallicRoughness `json:"pbrMetallicRoughness"`
	NormalTexture        *textureInfo         `json:"normalTexture,omitempty"`
}

type pbrMetallicRoughness struct {
	BaseColorTexture *textureInfo `json:"baseColorTexture,omitempty"`
	MetallicFactor   float32      `json:"metallicFactor"`
	RoughnessFactor  float32      `json:"roughnessFactor"`
}

type imageOutput struct {
	path     string
	contents []byte
}

type buffer struct {
	URI        string `json:"uri"`
	ByteLength int    `json:"byteLength"`
}

type bufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

type accessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Minimum       []float32 `json:"min,omitempty"`
	Maximum       []float32 `json:"max,omitempty"`
}

// WritePath writes a glTF JSON file and its external binary buffer. glTF is a
// deliberately one-way projection; exact RW4 data remains the responsibility
// of the DS conversion directory.
func WritePath(destinationPath string, primitives []rw4.Primitive) error {
	if len(primitives) == 0 {
		return errors.New("primitiveMissing")
	}
	if !strings.EqualFold(filepath.Ext(destinationPath), ".gltf") {
		return errors.New("destinationExtension: want .gltf")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	binaryPath := strings.TrimSuffix(destinationPath, filepath.Ext(destinationPath)) + ".bin"
	_, err = os.Stat(binaryPath)
	if err == nil {
		return fmt.Errorf("bufferExists: %q", binaryPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("bufferStat: %w", err)
	}
	imagePrefix := strings.TrimSuffix(filepath.Base(destinationPath), filepath.Ext(destinationPath))
	document, binaryContents, imageOutputs, err := build(filepath.Base(binaryPath), imagePrefix, primitives)
	if err != nil {
		return fmt.Errorf("documentBuild: %w", err)
	}
	jsonContents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonMarshal: %w", err)
	}
	jsonContents = append(jsonContents, '\n')
	writtenPaths := make([]string, 0, len(imageOutputs)+1)
	isComplete := false
	defer func() {
		if !isComplete {
			for _, writtenPath := range writtenPaths {
				_ = os.Remove(writtenPath)
			}
		}
	}()
	for imageIndex, output := range imageOutputs {
		outputPath := filepath.Join(filepath.Dir(destinationPath), output.path)
		err = writeExclusive(outputPath, output.contents)
		if err != nil {
			return fmt.Errorf("imageWrite[%d]: %w", imageIndex, err)
		}
		writtenPaths = append(writtenPaths, outputPath)
	}
	err = writeExclusive(binaryPath, binaryContents)
	if err != nil {
		return fmt.Errorf("bufferWrite: %w", err)
	}
	writtenPaths = append(writtenPaths, binaryPath)
	err = writeExclusive(destinationPath, jsonContents)
	if err != nil {
		return fmt.Errorf("jsonWrite: %w", err)
	}
	isComplete = true
	return nil
}

func build(binaryName, imagePrefix string, primitives []rw4.Primitive) (document, []byte, []imageOutput, error) {
	document := document{
		Asset:  asset{Version: "2.0", Generator: "darkrun gltf prototype"},
		Scene:  0,
		Scenes: []scene{{Nodes: make([]int, 0, len(primitives))}},
	}
	binaryContents := &bytes.Buffer{}
	imageOutputs := make([]imageOutput, 0)
	for primitiveIndex, renderPrimitive := range primitives {
		if len(renderPrimitive.Positions) == 0 || len(renderPrimitive.Indices) == 0 {
			return document, nil, nil, fmt.Errorf("primitive[%d]: missing positions or indices", primitiveIndex)
		}
		attributes := make(map[string]int, 3)
		positionAccessor, err := appendFloat3(binaryContents, &document, renderPrimitive.Positions, targetArrayBuffer, true)
		if err != nil {
			return document, nil, nil, fmt.Errorf("position[%d]: %w", primitiveIndex, err)
		}
		attributes["POSITION"] = positionAccessor
		if len(renderPrimitive.Normals) != 0 {
			if len(renderPrimitive.Normals) != len(renderPrimitive.Positions) {
				return document, nil, nil, fmt.Errorf("normalCount[%d]: got %d, want %d", primitiveIndex, len(renderPrimitive.Normals), len(renderPrimitive.Positions))
			}
			normalAccessor, appendErr := appendFloat3(binaryContents, &document, renderPrimitive.Normals, targetArrayBuffer, false)
			if appendErr != nil {
				return document, nil, nil, fmt.Errorf("normal[%d]: %w", primitiveIndex, appendErr)
			}
			attributes["NORMAL"] = normalAccessor
		}
		if len(renderPrimitive.TexCoords) != 0 {
			if len(renderPrimitive.TexCoords) != len(renderPrimitive.Positions) {
				return document, nil, nil, fmt.Errorf("texCoordCount[%d]: got %d, want %d", primitiveIndex, len(renderPrimitive.TexCoords), len(renderPrimitive.Positions))
			}
			texCoordAccessor, appendErr := appendFloat2(binaryContents, &document, renderPrimitive.TexCoords)
			if appendErr != nil {
				return document, nil, nil, fmt.Errorf("texCoord[%d]: %w", primitiveIndex, appendErr)
			}
			attributes["TEXCOORD_0"] = texCoordAccessor
		}
		indexAccessor, err := appendIndices(binaryContents, &document, renderPrimitive.Indices)
		if err != nil {
			return document, nil, nil, fmt.Errorf("index[%d]: %w", primitiveIndex, err)
		}
		materialIndex, materialImages, err := appendMaterial(&document, imagePrefix, primitiveIndex, renderPrimitive.Material)
		if err != nil {
			return document, nil, nil, fmt.Errorf("material[%d]: %w", primitiveIndex, err)
		}
		imageOutputs = append(imageOutputs, materialImages...)
		document.Meshes = append(document.Meshes, mesh{
			Name: renderPrimitive.Name,
			Primitives: []primitive{{
				Attributes: attributes,
				Indices:    indexAccessor,
				Mode:       primitiveTriangleMode,
				Material:   materialIndex,
			}},
		})
		meshIndex := primitiveIndex
		document.Nodes = append(document.Nodes, node{Name: renderPrimitive.Name, Mesh: &meshIndex})
		document.Scenes[0].Nodes = append(document.Scenes[0].Nodes, primitiveIndex)
	}
	err := appendRig(binaryContents, &document, primitives)
	if err != nil {
		return document, nil, nil, fmt.Errorf("rig: %w", err)
	}
	document.Buffers = []buffer{{URI: binaryName, ByteLength: binaryContents.Len()}}
	return document, binaryContents.Bytes(), imageOutputs, nil
}

func appendRig(contents *bytes.Buffer, document *document, primitives []rw4.Primitive) error {
	var rig *rw4.Rig
	for primitiveIndex := range primitives {
		if primitives[primitiveIndex].Rig != nil && len(primitives[primitiveIndex].Joints0) != 0 {
			rig = primitives[primitiveIndex].Rig
			break
		}
	}
	if rig == nil {
		return nil
	}
	if len(rig.Skeleton.Names) == 0 || len(rig.BindMatrices) != len(rig.Skeleton.Names) {
		return fmt.Errorf("boneCount: names %d, matrices %d", len(rig.Skeleton.Names), len(rig.BindMatrices))
	}
	jointBase := len(document.Nodes)
	jointNodes := make([]int, len(rig.Skeleton.Names))
	heads := rigHeads(rig.RestPoses)
	globalMatrices := make([][16]float32, len(heads))
	inverseBindMatrices := make([][12]float32, len(heads))
	for boneIndex, head := range heads {
		globalMatrices[boneIndex] = translationMatrix(head)
		inverseBindMatrices[boneIndex] = [12]float32{1, 0, 0, -head[0], 0, 1, 0, -head[1], 0, 0, 1, -head[2]}
	}
	rootIndex := -1
	for boneIndex, nameID := range rig.Skeleton.Names {
		localMatrix := globalMatrices[boneIndex]
		parent := rig.Skeleton.Parents[boneIndex]
		if parent >= 0 {
			parentInverse := affineMatrix(rig.BindMatrices[parent])
			localMatrix = multiplyMatrix(parentInverse, globalMatrices[boneIndex])
		} else if rootIndex < 0 {
			rootIndex = jointBase + boneIndex
		}
		jointNodes[boneIndex] = jointBase + boneIndex
		transform, err := decomposeMatrix(localMatrix)
		if err != nil {
			return fmt.Errorf("bone[%d]Decompose: %w", boneIndex, err)
		}
		document.Nodes = append(document.Nodes, node{Name: fmt.Sprintf("bone_%08x", nameID), Translation: transform.Translation, Rotation: transform.Rotation, Scale: transform.Scale})
	}
	for boneIndex, parent := range rig.Skeleton.Parents {
		if parent < 0 {
			document.Scenes[0].Nodes = append(document.Scenes[0].Nodes, jointBase+boneIndex)
			continue
		}
		parentNode := jointBase + int(parent)
		document.Nodes[parentNode].Children = append(document.Nodes[parentNode].Children, jointBase+boneIndex)
	}
	inverseBindAccessor, err := appendMatrices(contents, document, inverseBindMatrices)
	if err != nil {
		return fmt.Errorf("inverseBind: %w", err)
	}
	skinIndex := len(document.Skins)
	var root *int
	if rootIndex >= 0 {
		root = &rootIndex
	}
	document.Skins = append(document.Skins, skin{Name: fmt.Sprintf("skeleton_%08x", rig.Skeleton.ID), InverseBindMatrices: inverseBindAccessor, Skeleton: root, Joints: jointNodes})
	for primitiveIndex := range primitives {
		primitive := &primitives[primitiveIndex]
		if primitive.Rig == nil || len(primitive.Joints0) == 0 {
			continue
		}
		if len(primitive.Joints0) != len(primitive.Positions) || len(primitive.Weights0) != len(primitive.Positions) {
			return fmt.Errorf("primitive[%d]SkinCount", primitiveIndex)
		}
		joints0, appendErr := validJoints(primitive.Joints0, primitive.Weights0, len(rig.Skeleton.Names))
		if appendErr != nil {
			return fmt.Errorf("primitive[%d]Joints0: %w", primitiveIndex, appendErr)
		}
		jointAccessor, appendErr := appendUnsignedShort4(contents, document, joints0)
		if appendErr != nil {
			return fmt.Errorf("primitive[%d]Joints0: %w", primitiveIndex, appendErr)
		}
		weightAccessor, appendErr := appendFloat4(contents, document, primitive.Weights0)
		if appendErr != nil {
			return fmt.Errorf("primitive[%d]Weights0: %w", primitiveIndex, appendErr)
		}
		document.Meshes[primitiveIndex].Primitives[0].Attributes["JOINTS_0"] = jointAccessor
		document.Meshes[primitiveIndex].Primitives[0].Attributes["WEIGHTS_0"] = weightAccessor
		if len(primitive.Joints1) != 0 {
			joints1, validateErr := validJoints(primitive.Joints1, primitive.Weights1, len(rig.Skeleton.Names))
			if validateErr != nil {
				return fmt.Errorf("primitive[%d]Joints1: %w", primitiveIndex, validateErr)
			}
			jointAccessor, appendErr = appendUnsignedShort4(contents, document, joints1)
			if appendErr != nil {
				return fmt.Errorf("primitive[%d]Joints1: %w", primitiveIndex, appendErr)
			}
			weightAccessor, appendErr = appendFloat4(contents, document, primitive.Weights1)
			if appendErr != nil {
				return fmt.Errorf("primitive[%d]Weights1: %w", primitiveIndex, appendErr)
			}
			document.Meshes[primitiveIndex].Primitives[0].Attributes["JOINTS_1"] = jointAccessor
			document.Meshes[primitiveIndex].Primitives[0].Attributes["WEIGHTS_1"] = weightAccessor
		}
		document.Nodes[primitiveIndex].Skin = &skinIndex
	}
	err = appendRigAnimations(contents, document, rig, jointBase)
	if err != nil {
		return fmt.Errorf("animation: %w", err)
	}
	return nil
}

type nodeTransform struct {
	Translation []float32
	Rotation    []float32
	Scale       []float32
}

func rigHeads(poses []rw4.BonePose) [][3]float32 {
	heads := make([][3]float32, len(poses))
	for boneIndex, pose := range poses {
		for row := 0; row < 3; row++ {
			heads[boneIndex][row] = -(pose.Rotation[row*3]*pose.Translation[0] + pose.Rotation[row*3+1]*pose.Translation[1] + pose.Rotation[row*3+2]*pose.Translation[2])
		}
	}
	return heads
}

func translationMatrix(translation [3]float32) [16]float32 {
	return [16]float32{1, 0, 0, translation[0], 0, 1, 0, translation[1], 0, 0, 1, translation[2], 0, 0, 0, 1}
}

func decomposeMatrix(matrix [16]float32) (nodeTransform, error) {
	translation := []float32{matrix[3], matrix[7], matrix[11]}
	scale := make([]float32, 3)
	for column := 0; column < 3; column++ {
		scale[column] = float32(math.Sqrt(float64(matrix[column]*matrix[column] + matrix[4+column]*matrix[4+column] + matrix[8+column]*matrix[8+column])))
		if scale[column] == 0 {
			return nodeTransform{}, fmt.Errorf("zeroScale[%d]", column)
		}
	}
	r00, r01, r02 := matrix[0]/scale[0], matrix[1]/scale[1], matrix[2]/scale[2]
	r10, r11, r12 := matrix[4]/scale[0], matrix[5]/scale[1], matrix[6]/scale[2]
	r20, r21, r22 := matrix[8]/scale[0], matrix[9]/scale[1], matrix[10]/scale[2]
	rotation := make([]float32, 4)
	trace := r00 + r11 + r22
	if trace > 0 {
		s := float32(math.Sqrt(float64(trace+1))) * 2
		rotation = []float32{(r21 - r12) / s, (r02 - r20) / s, (r10 - r01) / s, s / 4}
	} else if r00 > r11 && r00 > r22 {
		s := float32(math.Sqrt(float64(1+r00-r11-r22))) * 2
		rotation = []float32{s / 4, (r01 + r10) / s, (r02 + r20) / s, (r21 - r12) / s}
	} else if r11 > r22 {
		s := float32(math.Sqrt(float64(1+r11-r00-r22))) * 2
		rotation = []float32{(r01 + r10) / s, s / 4, (r12 + r21) / s, (r02 - r20) / s}
	} else {
		s := float32(math.Sqrt(float64(1+r22-r00-r11))) * 2
		rotation = []float32{(r02 + r20) / s, (r12 + r21) / s, s / 4, (r10 - r01) / s}
	}
	return nodeTransform{Translation: translation, Rotation: rotation, Scale: scale}, nil
}

func appendRigAnimations(contents *bytes.Buffer, document *document, rig *rw4.Rig, jointBase int) error {
	for animationIndex, renderAnimation := range rig.Animations {
		animation := animationDef{Name: fmt.Sprintf("animation_%08x", renderAnimation.ID), Extras: map[string]any{"source": "RW4 embedded keyframe animation", "lengthSeconds": renderAnimation.Length}}
		for boneIndex, transforms := range renderAnimation.Channels {
			if len(transforms) != len(renderAnimation.Times) {
				return fmt.Errorf("animation[%d]Bone[%d]Count", animationIndex, boneIndex)
			}
			timeAccessor, err := appendRigScalars(contents, document, renderAnimation.Times)
			if err != nil {
				return fmt.Errorf("animation[%d]Bone[%d]Time: %w", animationIndex, boneIndex, err)
			}
			translations := make([][3]float32, len(transforms))
			rotations := make([][4]float32, len(transforms))
			scales := make([][3]float32, len(transforms))
			for frameIndex, transform := range transforms {
				translations[frameIndex], rotations[frameIndex], scales[frameIndex] = transform.Translation, transform.Rotation, transform.Scale
			}
			translationAccessor, err := appendFloat3(contents, document, translations, 0, false)
			if err != nil {
				return fmt.Errorf("translation: %w", err)
			}
			rotationAccessor, err := appendFloat4(contents, document, rotations)
			if err != nil {
				return fmt.Errorf("rotation: %w", err)
			}
			scaleAccessor, err := appendFloat3(contents, document, scales, 0, false)
			if err != nil {
				return fmt.Errorf("scale: %w", err)
			}
			for _, output := range []struct {
				path     string
				accessor int
			}{{"translation", translationAccessor}, {"rotation", rotationAccessor}, {"scale", scaleAccessor}} {
				samplerIndex := len(animation.Samplers)
				animation.Samplers = append(animation.Samplers, animationSampler{Input: timeAccessor, Interpolation: "LINEAR", Output: output.accessor})
				animation.Channels = append(animation.Channels, animationChannel{Sampler: samplerIndex, Target: animationTarget{Node: jointBase + boneIndex, Path: output.path}})
			}
		}
		document.Animations = append(document.Animations, animation)
	}
	return nil
}

func appendRigScalars(contents *bytes.Buffer, document *document, numbers []float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	for _, number := range numbers {
		err = binary.Write(contents, binary.LittleEndian, number)
		if err != nil {
			return 0, fmt.Errorf("numberWrite: %w", err)
		}
	}
	minimum, maximum := []float32(nil), []float32(nil)
	if len(numbers) != 0 {
		minimum, maximum = []float32{numbers[0]}, []float32{numbers[len(numbers)-1]}
	}
	return addAccessor(document, start, contents.Len()-start, 0, componentFloat, len(numbers), "SCALAR", minimum, maximum), nil
}

func validJoints(joints [][4]uint16, weights [][4]float32, boneCount int) ([][4]uint16, error) {
	if len(joints) != len(weights) {
		return nil, fmt.Errorf("streamCount: joints %d, weights %d", len(joints), len(weights))
	}
	validated := append([][4]uint16(nil), joints...)
	for vertexIndex := range validated {
		for influenceIndex := range validated[vertexIndex] {
			if int(validated[vertexIndex][influenceIndex]) < boneCount {
				continue
			}
			if weights[vertexIndex][influenceIndex] != 0 {
				return nil, fmt.Errorf("vertex[%d]Influence[%d]: joint %d exceeds %d with weight %g", vertexIndex, influenceIndex, validated[vertexIndex][influenceIndex], boneCount, weights[vertexIndex][influenceIndex])
			}
			validated[vertexIndex][influenceIndex] = 0
		}
	}
	return validated, nil
}

func appendUnsignedShort4(contents *bytes.Buffer, document *document, tuples [][4]uint16) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	for _, tuple := range tuples {
		for _, component := range tuple {
			err = binary.Write(contents, binary.LittleEndian, component)
			if err != nil {
				return 0, fmt.Errorf("componentWrite: %w", err)
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, targetArrayBuffer, componentUnsignedShort, len(tuples), "VEC4", nil, nil), nil
}

func appendFloat4(contents *bytes.Buffer, document *document, tuples [][4]float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	for _, tuple := range tuples {
		for _, component := range tuple {
			err = binary.Write(contents, binary.LittleEndian, component)
			if err != nil {
				return 0, fmt.Errorf("componentWrite: %w", err)
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, targetArrayBuffer, componentFloat, len(tuples), "VEC4", nil, nil), nil
}

func appendMatrices(contents *bytes.Buffer, document *document, matrices [][12]float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	for _, matrix := range matrices {
		for _, component := range columnMajor(affineMatrix(matrix)) {
			err = binary.Write(contents, binary.LittleEndian, component)
			if err != nil {
				return 0, fmt.Errorf("componentWrite: %w", err)
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, 0, componentFloat, len(matrices), "MAT4", nil, nil), nil
}

func affineMatrix(matrix [12]float32) [16]float32 {
	return [16]float32{matrix[0], matrix[1], matrix[2], matrix[3], matrix[4], matrix[5], matrix[6], matrix[7], matrix[8], matrix[9], matrix[10], matrix[11], 0, 0, 0, 1}
}

func inverseAffine(matrix [16]float32) ([16]float32, error) {
	a, b, c := matrix[0], matrix[1], matrix[2]
	d, e, f := matrix[4], matrix[5], matrix[6]
	g, h, i := matrix[8], matrix[9], matrix[10]
	determinant := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if math.Abs(float64(determinant)) < 1e-8 {
		return [16]float32{}, errors.New("singular")
	}
	inverseDeterminant := 1 / determinant
	result := [16]float32{
		(e*i - f*h) * inverseDeterminant, (c*h - b*i) * inverseDeterminant, (b*f - c*e) * inverseDeterminant, 0,
		(f*g - d*i) * inverseDeterminant, (a*i - c*g) * inverseDeterminant, (c*d - a*f) * inverseDeterminant, 0,
		(d*h - e*g) * inverseDeterminant, (b*g - a*h) * inverseDeterminant, (a*e - b*d) * inverseDeterminant, 0,
		0, 0, 0, 1,
	}
	tx, ty, tz := matrix[3], matrix[7], matrix[11]
	result[3] = -(result[0]*tx + result[1]*ty + result[2]*tz)
	result[7] = -(result[4]*tx + result[5]*ty + result[6]*tz)
	result[11] = -(result[8]*tx + result[9]*ty + result[10]*tz)
	return result, nil
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

func columnMajor(matrix [16]float32) []float32 {
	result := make([]float32, 16)
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			result[column*4+row] = matrix[row*4+column]
		}
	}
	return result
}

func appendMaterial(document *document, imagePrefix string, primitiveIndex int, renderMaterial *rw4.Material) (*int, []imageOutput, error) {
	if renderMaterial == nil {
		return nil, nil, nil
	}
	materialDef := material{
		Name: renderMaterial.Name,
		PBRMetallicRoughness: pbrMetallicRoughness{
			MetallicFactor:  0,
			RoughnessFactor: 1,
		},
	}
	imageOutputs := make([]imageOutput, 0, 2)
	if renderMaterial.BaseColor != nil {
		textureIndex, output, err := appendTexture(document, imagePrefix, primitiveIndex, "base-color", renderMaterial.BaseColor)
		if err != nil {
			return nil, nil, fmt.Errorf("baseColor: %w", err)
		}
		materialDef.PBRMetallicRoughness.BaseColorTexture = &textureInfo{Index: textureIndex}
		imageOutputs = append(imageOutputs, output)
	}
	if renderMaterial.Normal != nil {
		textureIndex, output, err := appendTexture(document, imagePrefix, primitiveIndex, "normal", renderMaterial.Normal)
		if err != nil {
			return nil, nil, fmt.Errorf("normal: %w", err)
		}
		materialDef.NormalTexture = &textureInfo{Index: textureIndex}
		imageOutputs = append(imageOutputs, output)
	}
	materialIndex := len(document.Materials)
	document.Materials = append(document.Materials, materialDef)
	return &materialIndex, imageOutputs, nil
}

func appendTexture(document *document, imagePrefix string, primitiveIndex int, role string, renderImage *rw4.TextureImage) (int, imageOutput, error) {
	if len(renderImage.RGBA) != renderImage.Width*renderImage.Height*4 {
		return 0, imageOutput{}, fmt.Errorf("pixelSize: got %d, want %d", len(renderImage.RGBA), renderImage.Width*renderImage.Height*4)
	}
	contents := &bytes.Buffer{}
	pixels := &image.NRGBA{
		Pix:    renderImage.RGBA,
		Stride: renderImage.Width * 4,
		Rect:   image.Rect(0, 0, renderImage.Width, renderImage.Height),
	}
	err := png.Encode(contents, pixels)
	if err != nil {
		return 0, imageOutput{}, fmt.Errorf("pngEncode: %w", err)
	}
	imageName := fmt.Sprintf("%s-%d-%s.png", imagePrefix, primitiveIndex, role)
	imageIndex := len(document.Images)
	document.Images = append(document.Images, imageDef{Name: renderImage.Name, URI: imageName})
	textureIndex := len(document.Textures)
	document.Textures = append(document.Textures, texture{Name: renderImage.Name, Source: imageIndex})
	return textureIndex, imageOutput{path: imageName, contents: contents.Bytes()}, nil
}

func appendFloat3(contents *bytes.Buffer, document *document, tuples [][3]float32, target int, isPosition bool) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	minimum := []float32(nil)
	maximum := []float32(nil)
	if isPosition {
		minimum = []float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
		maximum = []float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	}
	for _, tuple := range tuples {
		for componentIndex, component := range tuple {
			err = binary.Write(contents, binary.LittleEndian, component)
			if err != nil {
				return 0, fmt.Errorf("componentWrite: %w", err)
			}
			if isPosition && component < minimum[componentIndex] {
				minimum[componentIndex] = component
			}
			if isPosition && component > maximum[componentIndex] {
				maximum[componentIndex] = component
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, target, componentFloat, len(tuples), "VEC3", minimum, maximum), nil
}

func appendFloat2(contents *bytes.Buffer, document *document, tuples [][2]float32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	for _, tuple := range tuples {
		for _, component := range tuple {
			err = binary.Write(contents, binary.LittleEndian, component)
			if err != nil {
				return 0, fmt.Errorf("componentWrite: %w", err)
			}
		}
	}
	return addAccessor(document, start, contents.Len()-start, targetArrayBuffer, componentFloat, len(tuples), "VEC2", nil, nil), nil
}

func appendIndices(contents *bytes.Buffer, document *document, indices []uint32) (int, error) {
	err := align(contents)
	if err != nil {
		return 0, fmt.Errorf("align: %w", err)
	}
	start := contents.Len()
	for _, index := range indices {
		err = binary.Write(contents, binary.LittleEndian, index)
		if err != nil {
			return 0, fmt.Errorf("indexWrite: %w", err)
		}
	}
	return addAccessor(document, start, contents.Len()-start, targetElementBuffer, componentUnsignedInt, len(indices), "SCALAR", nil, nil), nil
}

func addAccessor(document *document, offset, length, target, componentType, count int, accessorType string, minimum, maximum []float32) int {
	viewIndex := len(document.BufferViews)
	document.BufferViews = append(document.BufferViews, bufferView{Buffer: 0, ByteOffset: offset, ByteLength: length, Target: target})
	accessorIndex := len(document.Accessors)
	document.Accessors = append(document.Accessors, accessor{
		BufferView:    viewIndex,
		ComponentType: componentType,
		Count:         count,
		Type:          accessorType,
		Minimum:       minimum,
		Maximum:       maximum,
	})
	return accessorIndex
}

func align(contents *bytes.Buffer) error {
	for contents.Len()%4 != 0 {
		err := contents.WriteByte(0)
		if err != nil {
			return fmt.Errorf("paddingWrite: %w", err)
		}
	}
	return nil
}

func writeExclusive(destinationPath string, contents []byte) error {
	w, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("destinationCreate: %w", err)
	}
	_, err = w.Write(contents)
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("destinationWrite: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("destinationClose: %w", closeErr)
	}
	return nil
}

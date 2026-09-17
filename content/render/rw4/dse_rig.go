package rw4

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
)

func (e *Document) writeDSESkeleton(writer *bufio.Writer, section Section) error {
	skeleton := e.Skeletons[section.Ordinal]
	_, err := fmt.Fprintf(writer, "\t\t\tSKELETONID 0x%08X\n\t\t\tNUMBONES %d\n", skeleton.ID, len(skeleton.Names))
	for boneIndex := range skeleton.Names {
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\t\tBONE %q\n\t\t\t\t\tNAMEID 0x%08X\n\t\t\t\t\tFLAGS 0x%08X\n", fmt.Sprintf("bone%d", boneIndex), skeleton.Names[boneIndex], skeleton.Flags[boneIndex])
		}
		if err != nil {
			break
		}
		if skeleton.Parents[boneIndex] < 0 {
			_, err = fmt.Fprintln(writer, "\t\t\t\t\tPARENT? NULL")
		} else {
			_, err = fmt.Fprintf(writer, "\t\t\t\t\tPARENT? %q\n", fmt.Sprintf("bone%d", skeleton.Parents[boneIndex]))
		}
	}
	return dseWriteError("skeleton", err)
}

func (e *Document) writeDSEAnimationSkin(writer *bufio.Writer, section Section) error {
	skin := e.AnimationSkins[section.Ordinal]
	_, err := fmt.Fprintf(writer, "\t\t\tFIELD8 0x%08X\n\t\t\tFIELDC 0x%08X\n\t\t\tNUMPOSES %d\n", skin.Field8, skin.FieldC, len(skin.Poses))
	for poseIndex, pose := range skin.Poses {
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\t\tPOSE %q\n", fmt.Sprintf("bone%d", poseIndex))
		}
		for row := 0; row < 3 && err == nil; row++ {
			_, err = fmt.Fprintf(writer, "\t\t\t\t\tROTATIONROW %d %s %s %s\n", row, dseFloat(pose.Rotation[row*3]), dseFloat(pose.Rotation[row*3+1]), dseFloat(pose.Rotation[row*3+2]))
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\t\t\tTRANSLATION %s %s %s\n", dseFloat(pose.Translation[0]), dseFloat(pose.Translation[1]), dseFloat(pose.Translation[2]))
		}
	}
	return dseWriteError("animationSkin", err)
}

func (e *Document) writeDSESkinMatrixBuffer(writer *bufio.Writer, section Section) error {
	buffer := e.SkinMatrices[section.Ordinal]
	_, err := fmt.Fprintf(writer, "\t\t\tFIELD8 0x%08X\n\t\t\tFIELDC 0x%08X\n\t\t\tNUMMATRICES %d\n", buffer.Field8, buffer.FieldC, len(buffer.Matrices))
	for matrixIndex, matrix := range buffer.Matrices {
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\t\tMATRIX %q\n", fmt.Sprintf("bone%d", matrixIndex))
		}
		for row := 0; row < 3 && err == nil; row++ {
			_, err = fmt.Fprintf(writer, "\t\t\t\t\tROW %d %s %s %s %s\n", row, dseFloat(matrix[row*4]), dseFloat(matrix[row*4+1]), dseFloat(matrix[row*4+2]), dseFloat(matrix[row*4+3]))
		}
	}
	return dseWriteError("skinMatrix", err)
}

func readDSESkeleton(parser *rw4Parser, section Section) ([]byte, error) {
	skeletonID, err := parser.uint32("SKELETONID")
	boneCount, err := parser.nextCount("NUMBONES", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	names := make([]uint32, boneCount)
	flags := make([]uint32, boneCount)
	parents := make([]int32, boneCount)
	for boneIndex := 0; boneIndex < boneCount; boneIndex++ {
		fields, readErr := parser.property("BONE", 1)
		if readErr != nil || fields[0] != fmt.Sprintf("bone%d", boneIndex) {
			return nil, fmt.Errorf("bone[%d]: %v: %v", boneIndex, fields, readErr)
		}
		names[boneIndex], readErr = parser.uint32("NAMEID")
		flags[boneIndex], readErr = parser.nextUint32("FLAGS", readErr)
		parent, parentErr := parser.property("PARENT?", 1)
		if readErr != nil {
			return nil, fmt.Errorf("bone[%d]Fields: %w", boneIndex, readErr)
		}
		if parentErr != nil {
			return nil, fmt.Errorf("bone[%d]Parent: %w", boneIndex, parentErr)
		}
		parents[boneIndex] = -1
		if parent[0] != "NULL" {
			_, scanErr := fmt.Sscanf(parent[0], "bone%d", &parents[boneIndex])
			if scanErr != nil || parents[boneIndex] < 0 || parents[boneIndex] >= int32(boneCount) {
				return nil, fmt.Errorf("bone[%d]Parent: %q", boneIndex, parent[0])
			}
		}
	}
	payload := make([]byte, 24+boneCount*12)
	nameOffset := 24
	flagOffset := nameOffset + boneCount*4
	parentOffset := flagOffset + boneCount*4
	binary.LittleEndian.PutUint32(payload[0:4], section.Offset+uint32(flagOffset))
	binary.LittleEndian.PutUint32(payload[4:8], section.Offset+uint32(parentOffset))
	binary.LittleEndian.PutUint32(payload[8:12], section.Offset+uint32(nameOffset))
	binary.LittleEndian.PutUint32(payload[12:16], uint32(boneCount))
	binary.LittleEndian.PutUint32(payload[16:20], skeletonID)
	binary.LittleEndian.PutUint32(payload[20:24], uint32(boneCount))
	for boneIndex := 0; boneIndex < boneCount; boneIndex++ {
		binary.LittleEndian.PutUint32(payload[nameOffset+boneIndex*4:], names[boneIndex])
		binary.LittleEndian.PutUint32(payload[flagOffset+boneIndex*4:], flags[boneIndex])
		binary.LittleEndian.PutUint32(payload[parentOffset+boneIndex*4:], uint32(parents[boneIndex]))
	}
	return payload, nil
}

func readDSEAnimationSkin(parser *rw4Parser, section Section) ([]byte, error) {
	field8, err := parser.uint32("FIELD8")
	fieldC, err := parser.nextUint32("FIELDC", err)
	poseCount, err := parser.nextCount("NUMPOSES", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	payload := make([]byte, 16+poseCount*64)
	binary.LittleEndian.PutUint32(payload[0:4], section.Offset+16)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(poseCount))
	binary.LittleEndian.PutUint32(payload[8:12], field8)
	binary.LittleEndian.PutUint32(payload[12:16], fieldC)
	for poseIndex := 0; poseIndex < poseCount; poseIndex++ {
		fields, readErr := parser.property("POSE", 1)
		if readErr != nil || fields[0] != fmt.Sprintf("bone%d", poseIndex) {
			return nil, fmt.Errorf("pose[%d]: %v: %v", poseIndex, fields, readErr)
		}
		base := 16 + poseIndex*64
		for row := 0; row < 3; row++ {
			coordinates, rowErr := parser.property("ROTATIONROW", 4)
			if rowErr != nil || coordinates[0] != fmt.Sprint(row) {
				return nil, fmt.Errorf("pose[%d]Row[%d]: %v: %v", poseIndex, row, coordinates, rowErr)
			}
			for column := 0; column < 3; column++ {
				number, parseErr := parseDSEFloat(coordinates[column+1])
				if parseErr != nil {
					return nil, fmt.Errorf("pose[%d]Row[%d]Column[%d]: %w", poseIndex, row, column, parseErr)
				}
				binary.LittleEndian.PutUint32(payload[base+row*16+column*4:], math.Float32bits(number))
			}
		}
		translation, translationErr := parser.property("TRANSLATION", 3)
		if translationErr != nil {
			return nil, fmt.Errorf("pose[%d]Translation: %w", poseIndex, translationErr)
		}
		for coordinate := 0; coordinate < 3; coordinate++ {
			number, parseErr := parseDSEFloat(translation[coordinate])
			if parseErr != nil {
				return nil, fmt.Errorf("pose[%d]Translation[%d]: %w", poseIndex, coordinate, parseErr)
			}
			binary.LittleEndian.PutUint32(payload[base+48+coordinate*4:], math.Float32bits(number))
		}
	}
	return payload, nil
}

func readDSESkinMatrixBuffer(parser *rw4Parser, section Section) ([]byte, error) {
	field8, err := parser.uint32("FIELD8")
	fieldC, err := parser.nextUint32("FIELDC", err)
	matrixCount, err := parser.nextCount("NUMMATRICES", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	payload := make([]byte, 16+matrixCount*48)
	binary.LittleEndian.PutUint32(payload[0:4], section.Offset+16)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(matrixCount))
	binary.LittleEndian.PutUint32(payload[8:12], field8)
	binary.LittleEndian.PutUint32(payload[12:16], fieldC)
	for matrixIndex := 0; matrixIndex < matrixCount; matrixIndex++ {
		fields, readErr := parser.property("MATRIX", 1)
		if readErr != nil || fields[0] != fmt.Sprintf("bone%d", matrixIndex) {
			return nil, fmt.Errorf("matrix[%d]: %v: %v", matrixIndex, fields, readErr)
		}
		for row := 0; row < 3; row++ {
			coordinates, rowErr := parser.property("ROW", 5)
			if rowErr != nil || coordinates[0] != fmt.Sprint(row) {
				return nil, fmt.Errorf("matrix[%d]Row[%d]: %v: %v", matrixIndex, row, coordinates, rowErr)
			}
			for column := 0; column < 4; column++ {
				number, parseErr := parseDSEFloat(coordinates[column+1])
				if parseErr != nil {
					return nil, fmt.Errorf("matrix[%d]Row[%d]Column[%d]: %w", matrixIndex, row, column, parseErr)
				}
				binary.LittleEndian.PutUint32(payload[16+matrixIndex*48+row*16+column*4:], math.Float32bits(number))
			}
		}
	}
	return payload, nil
}

func dseFloat(number float32) string { return fmt.Sprintf("%.9g", number) }
func parseDSEFloat(text string) (float32, error) {
	var number float32
	_, err := fmt.Sscan(text, &number)
	return number, err
}

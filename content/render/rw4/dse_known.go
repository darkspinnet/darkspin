package rw4

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func writeDSEBoundingBox(writer *bufio.Writer, payload []byte) error {
	if len(payload) != 32 {
		return fmt.Errorf("boundingBoxSize: got %d, want 32", len(payload))
	}
	_, err := fmt.Fprintf(writer, "\t\t\tMIN %q\n\t\t\tFIELD0C 0x%08X\n\t\t\tMAX %q\n\t\t\tFIELD1C 0x%08X\n",
		float32Text(payload[0:12]), binary.LittleEndian.Uint32(payload[12:16]),
		float32Text(payload[16:28]), binary.LittleEndian.Uint32(payload[28:32]))
	return dseWriteError("boundingBox", err)
}

func readDSEBoundingBox(parser *rw4Parser) ([]byte, error) {
	minimum, err := parser.property("MIN", 1)
	field0C, err := parser.nextUint32("FIELD0C", err)
	maximum, err := parser.nextProperty("MAX", err)
	field1C, err := parser.nextUint32("FIELD1C", err)
	if err != nil {
		return nil, fmt.Errorf("fields: %w", err)
	}
	payload := make([]byte, 32)
	err = parseFloat32Text(payload[0:12], minimum[0], 3)
	if err != nil {
		return nil, fmt.Errorf("minimum: %w", err)
	}
	binary.LittleEndian.PutUint32(payload[12:16], field0C)
	err = parseFloat32Text(payload[16:28], maximum[0], 3)
	if err != nil {
		return nil, fmt.Errorf("maximum: %w", err)
	}
	binary.LittleEndian.PutUint32(payload[28:32], field1C)
	return payload, nil
}

func writeDSESkinsInK(writer *bufio.Writer, payload []byte) error {
	if len(payload) != 20 {
		return fmt.Errorf("skinBindingSize: got %d, want 20", len(payload))
	}
	_, err := fmt.Fprintf(writer, "\t\t\tOBJECT? %s\n\t\t\tFUNCTION 0x%08X\n\t\t\tMATRIXBUFFER? %s\n\t\t\tSKELETON? %s\n\t\t\tANIMATIONSKIN? %s\n",
		referenceTag(binary.LittleEndian.Uint32(payload[0:4])), binary.LittleEndian.Uint32(payload[4:8]),
		referenceTag(binary.LittleEndian.Uint32(payload[8:12])), referenceTag(binary.LittleEndian.Uint32(payload[12:16])),
		referenceTag(binary.LittleEndian.Uint32(payload[16:20])))
	return dseWriteError("skinBinding", err)
}

func readDSESkinsInK(parser *rw4Parser) ([]byte, error) {
	objectRef, err := parser.reference("OBJECT?")
	function, err := parser.nextUint32("FUNCTION", err)
	matrixRef, err := parser.nextReference("MATRIXBUFFER?", err)
	skeletonRef, err := parser.nextReference("SKELETON?", err)
	animationRef, err := parser.nextReference("ANIMATIONSKIN?", err)
	if err != nil {
		return nil, fmt.Errorf("fields: %w", err)
	}
	return uint32Payload(objectRef, function, matrixRef, skeletonRef, animationRef), nil
}

func writeDSEMorphHandle(writer *bufio.Writer, payload []byte) error {
	if len(payload) != 64 {
		return fmt.Errorf("morphHandleSize: got %d, want 64", len(payload))
	}
	_, err := fmt.Fprintf(writer, "\t\t\tID 0x%08X\n\t\t\tFIELD04 0x%08X\n\t\t\tSTARTPOSITION %q\n\t\t\tENDPOSITION %q\n\t\t\tDEFAULTTIME %s\n\t\t\tANIMATION? %s\n",
		binary.LittleEndian.Uint32(payload[0:4]), binary.LittleEndian.Uint32(payload[4:8]),
		float64Text(payload[8:32]), float64Text(payload[32:56]),
		strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[56:60]))), 'g', -1, 32),
		referenceTag(binary.LittleEndian.Uint32(payload[60:64])))
	return dseWriteError("morphHandle", err)
}

func readDSEMorphHandle(parser *rw4Parser) ([]byte, error) {
	id, err := parser.uint32("ID")
	field04, err := parser.nextUint32("FIELD04", err)
	start, err := parser.nextProperty("STARTPOSITION", err)
	end, err := parser.nextProperty("ENDPOSITION", err)
	defaultTime, err := parser.nextProperty("DEFAULTTIME", err)
	animationRef, err := parser.nextReference("ANIMATION?", err)
	if err != nil {
		return nil, fmt.Errorf("fields: %w", err)
	}
	payload := make([]byte, 64)
	binary.LittleEndian.PutUint32(payload[0:4], id)
	binary.LittleEndian.PutUint32(payload[4:8], field04)
	err = parseFloat64Text(payload[8:32], start[0], 3)
	if err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	err = parseFloat64Text(payload[32:56], end[0], 3)
	if err != nil {
		return nil, fmt.Errorf("end: %w", err)
	}
	number, err := strconv.ParseFloat(defaultTime[0], 32)
	if err != nil {
		return nil, fmt.Errorf("defaultTime: %w", err)
	}
	binary.LittleEndian.PutUint32(payload[56:60], math.Float32bits(float32(number)))
	binary.LittleEndian.PutUint32(payload[60:64], animationRef)
	return payload, nil
}

func writeDSEAnimations(writer *bufio.Writer, payload []byte) error {
	if len(payload) < 8 {
		return fmt.Errorf("animationsSize: got %d, want at least 8", len(payload))
	}
	count := binary.LittleEndian.Uint32(payload[4:8])
	if uint64(count) > uint64(len(payload))/8 || len(payload) != 8+int(count)*8 {
		return fmt.Errorf("animationsCount: %d does not fit %d bytes", count, len(payload))
	}
	_, err := fmt.Fprintf(writer, "\t\t\tSELFREFERENCE 0x%08X\n\t\t\tNUMANIMATIONS %d\n", binary.LittleEndian.Uint32(payload[0:4]), count)
	for animationIndex := 0; animationIndex < int(count) && err == nil; animationIndex++ {
		offset := 8 + animationIndex*8
		_, err = fmt.Fprintf(writer, "\t\t\t\tANIMATION %q\n\t\t\t\t\tID 0x%08X\n\t\t\t\t\tRESOURCE? %s\n",
			fmt.Sprintf("animation%d", animationIndex), binary.LittleEndian.Uint32(payload[offset:offset+4]),
			referenceTag(binary.LittleEndian.Uint32(payload[offset+4:offset+8])))
	}
	return dseWriteError("animations", err)
}

func readDSEAnimations(parser *rw4Parser) ([]byte, error) {
	selfReference, err := parser.uint32("SELFREFERENCE")
	count, err := parser.nextCount("NUMANIMATIONS", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	payload := make([]byte, 8+count*8)
	binary.LittleEndian.PutUint32(payload[0:4], selfReference)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(count))
	for animationIndex := 0; animationIndex < count; animationIndex++ {
		name, readErr := parser.property("ANIMATION", 1)
		if readErr != nil || name[0] != fmt.Sprintf("animation%d", animationIndex) {
			return nil, fmt.Errorf("animation[%d]: %v: %v", animationIndex, name, readErr)
		}
		id, readErr := parser.uint32("ID")
		resourceRef, readErr := parser.nextReference("RESOURCE?", readErr)
		if readErr != nil {
			return nil, fmt.Errorf("animationFields[%d]: %w", animationIndex, readErr)
		}
		offset := 8 + animationIndex*8
		binary.LittleEndian.PutUint32(payload[offset:offset+4], id)
		binary.LittleEndian.PutUint32(payload[offset+4:offset+8], resourceRef)
	}
	return payload, nil
}

func float32Text(payload []byte) string {
	components := make([]string, len(payload)/4)
	for index := range components {
		bits := binary.LittleEndian.Uint32(payload[index*4 : index*4+4])
		components[index] = strconv.FormatFloat(float64(math.Float32frombits(bits)), 'g', -1, 32)
	}
	return strings.Join(components, " ")
}

func float64Text(payload []byte) string {
	components := make([]string, len(payload)/8)
	for index := range components {
		bits := binary.LittleEndian.Uint64(payload[index*8 : index*8+8])
		components[index] = strconv.FormatFloat(math.Float64frombits(bits), 'g', -1, 64)
	}
	return strings.Join(components, " ")
}

func parseFloat32Text(payload []byte, text string, count int) error {
	components := strings.Fields(text)
	if len(components) != count || len(payload) != count*4 {
		return fmt.Errorf("componentCount: got %d, want %d", len(components), count)
	}
	for index, component := range components {
		number, err := strconv.ParseFloat(component, 32)
		if err != nil {
			return fmt.Errorf("component[%d]: %w", index, err)
		}
		binary.LittleEndian.PutUint32(payload[index*4:index*4+4], math.Float32bits(float32(number)))
	}
	return nil
}

func parseFloat64Text(payload []byte, text string, count int) error {
	components := strings.Fields(text)
	if len(components) != count || len(payload) != count*8 {
		return fmt.Errorf("componentCount: got %d, want %d", len(components), count)
	}
	for index, component := range components {
		number, err := strconv.ParseFloat(component, 64)
		if err != nil {
			return fmt.Errorf("component[%d]: %w", index, err)
		}
		binary.LittleEndian.PutUint64(payload[index*8:index*8+8], math.Float64bits(number))
	}
	return nil
}

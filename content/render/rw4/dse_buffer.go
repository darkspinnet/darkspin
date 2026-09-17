package rw4

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type vertexData struct {
	bufferOrdinal int
	buffer        VertexBuffer
	description   VertexDescription
	keys          []string
}

type indexData struct {
	bufferOrdinal int
	buffer        IndexBuffer
	indexSize     int
}

func (e *Document) vertexDataInfo(baseOrdinal int) (vertexData, bool) {
	ordinals := make([]int, 0, len(e.VertexBuffers))
	for ordinal := range e.VertexBuffers {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	var info vertexData
	isFound := false
	for _, ordinal := range ordinals {
		buffer := e.VertexBuffers[ordinal]
		dataOrdinal, err := e.directOrdinal(buffer.DataRef)
		if err != nil || dataOrdinal != baseOrdinal {
			continue
		}
		if isFound {
			return vertexData{}, false
		}
		descriptionOrdinal, err := e.directOrdinal(buffer.DescriptionRef)
		if err != nil {
			return vertexData{}, false
		}
		description, isDescriptionFound := e.VertexFormats[descriptionOrdinal]
		if !isDescriptionFound || buffer.VertexSize == 0 || buffer.VertexSize != uint32(description.VertexSize) {
			return vertexData{}, false
		}
		section := e.Sections[baseOrdinal]
		if uint64(buffer.VertexCount)*uint64(buffer.VertexSize) != uint64(section.Size) {
			return vertexData{}, false
		}
		keys, isSupported := vertexKeys(description)
		if !isSupported {
			return vertexData{}, false
		}
		info = vertexData{bufferOrdinal: ordinal, buffer: buffer, description: description, keys: keys}
		isFound = true
	}
	return info, isFound
}

func (e *Document) indexDataInfo(baseOrdinal int) (indexData, bool) {
	ordinals := make([]int, 0, len(e.IndexBuffers))
	for ordinal := range e.IndexBuffers {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	var info indexData
	isFound := false
	for _, ordinal := range ordinals {
		buffer := e.IndexBuffers[ordinal]
		dataOrdinal, err := e.directOrdinal(buffer.DataRef)
		if err != nil || dataOrdinal != baseOrdinal {
			continue
		}
		if isFound {
			return indexData{}, false
		}
		indexSize := 0
		switch buffer.Format {
		case formatIndex16:
			indexSize = 2
		case formatIndex32:
			indexSize = 4
		default:
			return indexData{}, false
		}
		section := e.Sections[baseOrdinal]
		if uint64(buffer.PrimitiveCount)*uint64(indexSize) != uint64(section.Size) {
			return indexData{}, false
		}
		info = indexData{bufferOrdinal: ordinal, buffer: buffer, indexSize: indexSize}
		isFound = true
	}
	return info, isFound
}

func vertexKeys(description VertexDescription) ([]string, bool) {
	coverage := make([]bool, int(description.VertexSize))
	keys := make([]string, len(description.Elements))
	usedKeys := make(map[string]struct{}, len(description.Elements))
	for elementIndex, element := range description.Elements {
		if element.Stream != 0 {
			return nil, false
		}
		size := vertexElementSize(element.DataType)
		if size == 0 || int(element.Offset)+size > len(coverage) {
			return nil, false
		}
		for offset := int(element.Offset); offset < int(element.Offset)+size; offset++ {
			if coverage[offset] {
				return nil, false
			}
			coverage[offset] = true
		}
		key := fmt.Sprintf("ELEMENT%d", elementIndex)
		switch element.TypeCode {
		case usagePosition:
			key = "POSITION"
		case usageNormal:
			key = "NORMAL"
		case usageTexCoord0:
			key = "TEXCOORD"
		}
		if _, isUsed := usedKeys[key]; isUsed {
			key = fmt.Sprintf("ELEMENT%d", elementIndex)
		}
		usedKeys[key] = struct{}{}
		keys[elementIndex] = key
	}
	for _, isCovered := range coverage {
		if !isCovered {
			return nil, false
		}
	}
	return keys, true
}

func vertexElementShape(dataType uint8) (int, int) {
	switch dataType {
	case dataFloat2:
		return 8, 2
	case dataFloat3:
		return 12, 3
	case dataUByte4, dataUByte4N:
		return 4, 4
	default:
		return 0, 0
	}
}

func vertexElementSize(dataType uint8) int {
	size, componentCount := vertexElementShape(dataType)
	if componentCount == 0 {
		return 0
	}
	return size
}

func (e *Document) writeDSEBuffer(writer *bufio.Writer, section Section) error {
	vertexInfo, isVertexData := e.vertexDataInfo(section.Ordinal)
	if isVertexData {
		return writeDSEVertexData(writer, section.Data, vertexInfo)
	}
	indexInfo, isIndexData := e.indexDataInfo(section.Ordinal)
	if isIndexData {
		return writeDSEIndexData(writer, section.Data, indexInfo)
	}
	return nil
}

func writeDSEVertexData(writer *bufio.Writer, payload []byte, info vertexData) error {
	_, err := fmt.Fprintf(writer, "\t\t\tVERTEXBUFFER %q\n\t\t\tVERTEXSIZE %d\n\t\t\tNUMELEMENTS %d\n", sectionTag(info.bufferOrdinal), info.buffer.VertexSize, len(info.description.Elements))
	for elementIndex, element := range info.description.Elements {
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\t\tELEMENT %q\n\t\t\t\t\tKEY %q\n\t\t\t\t\tOFFSET %d\n\t\t\t\t\tDATATYPE %q\n", fmt.Sprintf("element%d", elementIndex), info.keys[elementIndex], element.Offset, vertexDataTypeName(element.DataType))
		}
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tNUMVERTICES %d\n", info.buffer.VertexCount)
	}
	for vertexIndex := 0; vertexIndex < int(info.buffer.VertexCount) && err == nil; vertexIndex++ {
		_, err = fmt.Fprintf(writer, "\t\t\t\tVERTEX %q\n", fmt.Sprintf("vertex%d", vertexIndex))
		for elementIndex, element := range info.description.Elements {
			if err != nil {
				break
			}
			base := vertexIndex*int(info.buffer.VertexSize) + int(element.Offset)
			components, readErr := vertexComponents(payload, base, element.DataType)
			if readErr != nil {
				return fmt.Errorf("vertex[%d]Element[%d]: %w", vertexIndex, elementIndex, readErr)
			}
			_, err = fmt.Fprintf(writer, "\t\t\t\t\t%s %q\n", info.keys[elementIndex], strings.Join(components, " "))
		}
	}
	return dseWriteError("vertexData", err)
}

func vertexComponents(payload []byte, offset int, dataType uint8) ([]string, error) {
	size, componentCount := vertexElementShape(dataType)
	if size == 0 || offset < 0 || offset > len(payload)-size {
		return nil, fmt.Errorf("range: %d:%d exceeds %d", offset, offset+size, len(payload))
	}
	components := make([]string, componentCount)
	if dataType == dataFloat2 || dataType == dataFloat3 {
		for componentIndex := range components {
			bits := binary.LittleEndian.Uint32(payload[offset+componentIndex*4 : offset+componentIndex*4+4])
			components[componentIndex] = strconv.FormatFloat(float64(math.Float32frombits(bits)), 'g', -1, 32)
		}
		return components, nil
	}
	for componentIndex := range components {
		components[componentIndex] = strconv.FormatUint(uint64(payload[offset+componentIndex]), 10)
	}
	return components, nil
}

func writeDSEIndexData(writer *bufio.Writer, payload []byte, info indexData) error {
	_, err := fmt.Fprintf(writer, "\t\t\tINDEXBUFFER %q\n\t\t\tFORMAT %q\n\t\t\tNUMINDICES %d\n", sectionTag(info.bufferOrdinal), indexFormatName(info.buffer.Format), info.buffer.PrimitiveCount)
	for index := 0; index < int(info.buffer.PrimitiveCount) && err == nil; index++ {
		offset := index * info.indexSize
		number := uint32(binary.LittleEndian.Uint16(payload[offset : offset+2]))
		if info.indexSize == 4 {
			number = binary.LittleEndian.Uint32(payload[offset : offset+4])
		}
		_, err = fmt.Fprintf(writer, "\t\t\t\tINDEX %d\n", number)
	}
	return dseWriteError("indexData", err)
}

func readDSEBuffer(parser *rw4Parser, kind string) ([]byte, error) {
	switch kind {
	case "VERTEXDATA":
		return readDSEVertexData(parser)
	case "INDEXDATA":
		return readDSEIndexData(parser)
	case "BASERESOURCE":
		return nil, nil
	default:
		return nil, fmt.Errorf("kind: %q", kind)
	}
}

type dseVertexElement struct {
	key      string
	offset   int
	dataType uint8
}

func readDSEVertexData(parser *rw4Parser) ([]byte, error) {
	_, err := parser.reference("VERTEXBUFFER")
	vertexSize, err := parser.nextCount("VERTEXSIZE", err)
	elementCount, err := parser.nextCount("NUMELEMENTS", err)
	if err != nil || vertexSize <= 0 {
		return nil, fmt.Errorf("header: %v", err)
	}
	elements := make([]dseVertexElement, elementCount)
	coverage := make([]bool, vertexSize)
	for elementIndex := range elements {
		name, readErr := parser.property("ELEMENT", 1)
		if readErr != nil || name[0] != fmt.Sprintf("element%d", elementIndex) {
			return nil, fmt.Errorf("element[%d]: %v: %v", elementIndex, name, readErr)
		}
		key, readErr := parser.property("KEY", 1)
		offset, readErr := parser.nextCount("OFFSET", readErr)
		dataType, readErr := parser.nextSymbol("DATATYPE", readErr, map[string]uint32{"FLOAT2": dataFloat2, "FLOAT3": dataFloat3, "UBYTE4": dataUByte4, "UBYTE4N": dataUByte4N})
		if readErr != nil {
			return nil, fmt.Errorf("elementFields[%d]: %w", elementIndex, readErr)
		}
		if dataType > math.MaxUint8 {
			return nil, fmt.Errorf("elementType[%d]: %d exceeds uint8", elementIndex, dataType)
		}
		size := vertexElementSize(uint8(dataType))
		if size == 0 || offset < 0 || offset > vertexSize || size > vertexSize-offset {
			return nil, fmt.Errorf("elementRange[%d]: offset %d size %d exceeds stride %d", elementIndex, offset, size, vertexSize)
		}
		for byteOffset := offset; byteOffset < offset+size; byteOffset++ {
			if coverage[byteOffset] {
				return nil, fmt.Errorf("elementOverlap[%d]: byte %d", elementIndex, byteOffset)
			}
			coverage[byteOffset] = true
		}
		elements[elementIndex] = dseVertexElement{key: key[0], offset: offset, dataType: uint8(dataType)}
	}
	for byteOffset, isCovered := range coverage {
		if !isCovered {
			return nil, fmt.Errorf("elementGap: byte %d", byteOffset)
		}
	}
	vertexCount, err := parser.count("NUMVERTICES")
	if err != nil {
		return nil, fmt.Errorf("vertexCount: %w", err)
	}
	if vertexCount > math.MaxInt/vertexSize {
		return nil, fmt.Errorf("vertexSize: %d vertices of %d bytes exceed int", vertexCount, vertexSize)
	}
	payload := make([]byte, vertexCount*vertexSize)
	for vertexIndex := 0; vertexIndex < vertexCount; vertexIndex++ {
		name, readErr := parser.property("VERTEX", 1)
		if readErr != nil || name[0] != fmt.Sprintf("vertex%d", vertexIndex) {
			return nil, fmt.Errorf("vertex[%d]: %v: %v", vertexIndex, name, readErr)
		}
		for elementIndex, element := range elements {
			components, readErr := parser.property(element.key, 1)
			if readErr != nil {
				return nil, fmt.Errorf("vertex[%d]Element[%d]: %w", vertexIndex, elementIndex, readErr)
			}
			base := vertexIndex*vertexSize + element.offset
			readErr = encodeVertexComponents(payload, base, element.dataType, strings.Fields(components[0]))
			if readErr != nil {
				return nil, fmt.Errorf("vertex[%d]Element[%d]: %w", vertexIndex, elementIndex, readErr)
			}
		}
	}
	return payload, nil
}

func encodeVertexComponents(payload []byte, offset int, dataType uint8, components []string) error {
	_, componentCount := vertexElementShape(dataType)
	if len(components) != componentCount {
		return fmt.Errorf("componentCount: got %d, want %d", len(components), componentCount)
	}
	if dataType == dataFloat2 || dataType == dataFloat3 {
		for componentIndex, component := range components {
			number, err := strconv.ParseFloat(component, 32)
			if err != nil {
				return fmt.Errorf("component[%d]: %w", componentIndex, err)
			}
			binary.LittleEndian.PutUint32(payload[offset+componentIndex*4:offset+componentIndex*4+4], math.Float32bits(float32(number)))
		}
		return nil
	}
	for componentIndex, component := range components {
		number, err := strconv.ParseUint(component, 0, 8)
		if err != nil {
			return fmt.Errorf("component[%d]: %w", componentIndex, err)
		}
		payload[offset+componentIndex] = byte(number)
	}
	return nil
}

func readDSEIndexData(parser *rw4Parser) ([]byte, error) {
	_, err := parser.reference("INDEXBUFFER")
	format, err := parser.nextSymbol("FORMAT", err, map[string]uint32{"UINT16": formatIndex16, "UINT32": formatIndex32})
	indexCount, err := parser.nextCount("NUMINDICES", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	indexSize := 2
	if format == formatIndex32 {
		indexSize = 4
	} else if format != formatIndex16 {
		return nil, fmt.Errorf("format: %d", format)
	}
	payload := make([]byte, indexCount*indexSize)
	for index := 0; index < indexCount; index++ {
		number, readErr := parser.uint32("INDEX")
		if readErr != nil {
			return nil, fmt.Errorf("index[%d]: %w", index, readErr)
		}
		if indexSize == 2 {
			if number > 0xFFFF {
				return nil, fmt.Errorf("index[%d]: %d exceeds uint16", index, number)
			}
			binary.LittleEndian.PutUint16(payload[index*2:index*2+2], uint16(number))
		} else {
			binary.LittleEndian.PutUint32(payload[index*4:index*4+4], number)
		}
	}
	return payload, nil
}

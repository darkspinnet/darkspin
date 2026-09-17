package gmsh

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const dseRowSize = 32

// WriteDSE writes every GMSH field as editable ASCII. Proven position and
// index streams use semantic numeric rows; unclassified bounded fields use
// decimal bytes so no geometry remains hidden in hexadecimal.
func WriteDSE(w io.Writer, identity string, document *Document) error {
	if document == nil {
		return errors.New("nil document")
	}
	writer := bufio.NewWriter(w)
	_, err := fmt.Fprintln(writer, "// darkspin resource dse v1")
	if err == nil {
		_, err = fmt.Fprintf(writer, "GMSH %q\n", identity)
	}
	if err == nil {
		_, err = fmt.Fprintln(writer, "\tVERSION 1")
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tFORMATVERSION %d\n\tNUMMESHES %d\n", document.Version, len(document.Meshes))
	}
	for meshIndex, mesh := range document.Meshes {
		if err != nil {
			break
		}
		_, err = fmt.Fprintf(writer, "\t\tMESH %q\n\t\t\tVERSION %d\n\t\t\tNUMSTREAMS %d\n", fmt.Sprintf("mesh%d", meshIndex), mesh.Version, len(mesh.Streams))
		for streamIndex, stream := range mesh.Streams {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(writer, "\t\t\t\tSTREAM %q\n", fmt.Sprintf("stream%d", streamIndex))
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\tUSAGE %q\n\t\t\t\t\tUSAGECODE %d\n\t\t\t\t\tUSAGEINDEX %d\n\t\t\t\t\tFORMATCODE %d\n\t\t\t\t\tFIELD03 %d\n\t\t\t\t\tCOUNT %d\n\t\t\t\t\tSTRIDE %d\n", streamUsageName(stream), stream.Usage, stream.UsageIndex, stream.Format, stream.Field03, stream.Count, stream.Stride)
			}
			if err == nil {
				err = writeStreamRows(writer, stream)
			}
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tNUMBUFFERS %d\n", len(mesh.Buffers))
		}
		for bufferIndex, buffer := range mesh.Buffers {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(writer, "\t\t\t\tBUFFER %q\n\t\t\t\t\tCOUNT %d\n\t\t\t\t\tSTRIDE %d\n", fmt.Sprintf("buffer%d", bufferIndex), buffer.Count, buffer.Stride)
			if err == nil {
				err = writeIndexRows(writer, "\t\t\t\t\t", buffer.Count, buffer.Stride, buffer.Data)
			}
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\tFIELD 0x%08X\n\t\t\t\t\tNUMREMAPS %d\n", buffer.Field, len(buffer.Remaps))
			}
			for _, remap := range buffer.Remaps {
				if err == nil {
					_, err = fmt.Fprintf(writer, "\t\t\t\t\t\tREMAP %d %d\n", remap[0], remap[1])
				}
			}
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\tNUMAUXILIARY %d\n", len(buffer.Auxiliary))
			}
			for auxiliaryIndex, auxiliary := range buffer.Auxiliary {
				if err != nil {
					break
				}
				_, err = fmt.Fprintf(writer, "\t\t\t\t\t\tAUXILIARY %q\n\t\t\t\t\t\t\tCOUNT %d\n\t\t\t\t\t\t\tSTRIDE %d\n", fmt.Sprintf("auxiliary%d", auxiliaryIndex), auxiliary.Count, auxiliary.Stride)
				if err == nil {
					err = writeByteRows(writer, "\t\t\t\t\t\t\t", auxiliary.Data, int(auxiliary.Stride))
				}
			}
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tNUMDRAWS %d\n", len(mesh.Draws))
		}
		for drawIndex, draw := range mesh.Draws {
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\tDRAW %q\n", fmt.Sprintf("draw%d", drawIndex))
			}
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\tPRIMITIVETYPE %q\n\t\t\t\t\tPRIMITIVECODE %d\n\t\t\t\t\tBUFFER %d\n\t\t\t\t\tFIRSTINDEX %d\n\t\t\t\t\tENDINDEX %d\n\t\t\t\t\tMATERIAL %d\n", primitiveName(draw.PrimitiveType), draw.PrimitiveType, draw.BufferIndex, draw.FirstIndex, draw.EndIndex, draw.MaterialIndex)
			}
		}
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMMATERIALSETS %d\n", len(document.MaterialSets))
	}
	for setIndex, materials := range document.MaterialSets {
		if err != nil {
			break
		}
		_, err = fmt.Fprintf(writer, "\t\tMATERIALSET %q\n\t\t\tNUMMATERIALS %d\n", fmt.Sprintf("materialSet%d", setIndex), len(materials))
		for materialIndex, material := range materials {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(writer, "\t\t\t\tMATERIAL %q\n\t\t\t\t\tINDEX %d\n\t\t\t\t\tSHADER %d\n", fmt.Sprintf("material%d", materialIndex), material.Index, material.ShaderIndex)
			if err == nil {
				err = writeOptionalReference(writer, "\t\t\t\t\t", "REFERENCEA?", material.ReferenceA)
			}
			if err == nil {
				err = writeOptionalReference(writer, "\t\t\t\t\t", "REFERENCEB?", material.ReferenceB)
			}
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	if err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

func streamUsageName(stream Stream) string {
	switch {
	case stream.Usage == 1 && stream.UsageIndex == 0 && stream.Format == 3 && stream.Stride == 12:
		return "POSITION"
	case stream.Usage == 2 && stream.UsageIndex == 0 && stream.Format == 7 && stream.Stride == 4:
		return "NORMAL"
	case stream.Usage == 3 && stream.UsageIndex == 0 && stream.Format == 7 && stream.Stride == 4:
		return "TANGENT"
	case stream.Usage == 8 && stream.UsageIndex == 0 && stream.Format == 2 && stream.Stride == 8:
		return "TEXCOORD"
	case stream.Usage == 9 && stream.UsageIndex == 0 && stream.Format == 7 && stream.Stride == 4:
		return "BONEINDICES"
	case stream.Usage == 10 && stream.UsageIndex == 0 && stream.Format == 10 && stream.Stride == 4:
		return "BONEWEIGHTS"
	case stream.Usage == 18 && stream.UsageIndex == 0 && stream.Format == 14 && stream.Stride == 48:
		return "MATRIX3X4"
	case stream.Usage == 21 && stream.UsageIndex == 0 && stream.Format == 6 && stream.Stride == 4:
		return "MATERIALLOOKUP"
	case stream.Format == 3 && stream.Stride == 12:
		return "VECTOR3"
	}
	return "UNCLASSIFIED"
}

func writeStreamRows(writer *bufio.Writer, stream Stream) error {
	usageName := streamUsageName(stream)
	switch usageName {
	case "POSITION", "VECTOR3":
		return writeFloatRows(writer, stream.Data, 3, usageName)
	case "NORMAL", "TANGENT":
		return writePackedVectorRows(writer, stream.Data, usageName)
	case "TEXCOORD":
		return writeFloatRows(writer, stream.Data, 2, usageName)
	case "BONEINDICES", "BONEWEIGHTS":
		return writeUint8Rows(writer, stream.Data, usageName)
	case "MATRIX3X4":
		return writeFloatRows(writer, stream.Data, 12, usageName)
	case "MATERIALLOOKUP":
		return writeUint32Rows(writer, stream.Data, usageName)
	case "UNCLASSIFIED":
		return writeByteRows(writer, "\t\t\t\t\t", stream.Data, int(stream.Stride))
	}
	return fmt.Errorf("usageUnsupported: %q", usageName)
}

func writeFloatRows(writer *bufio.Writer, data []byte, componentCount int, rowName string) error {
	rowSize := componentCount * 4
	_, err := fmt.Fprintf(writer, "\t\t\t\t\tNUMROWS %d\n", len(data)/rowSize)
	for offset := 0; offset < len(data) && err == nil; offset += rowSize {
		_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t%s", rowName)
		for componentIndex := 0; componentIndex < componentCount && err == nil; componentIndex++ {
			bits := binary.LittleEndian.Uint32(data[offset+componentIndex*4:])
			_, err = fmt.Fprintf(writer, " %s", formatFloat(math.Float32frombits(bits)))
		}
		if err == nil {
			_, err = fmt.Fprintln(writer)
		}
	}
	if err != nil {
		return fmt.Errorf("floatOutput: %w", err)
	}
	return nil
}

func writePackedVectorRows(writer *bufio.Writer, data []byte, rowName string) error {
	_, err := fmt.Fprintf(writer, "\t\t\t\t\tNUMROWS %d\n", len(data)/4)
	for offset := 0; offset < len(data) && err == nil; offset += 4 {
		_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t%s", rowName)
		for componentIndex := 0; componentIndex < 4 && err == nil; componentIndex++ {
			component := float32(data[offset+componentIndex])/127 - 1
			_, err = fmt.Fprintf(writer, " %s", formatFloat(component))
		}
		if err == nil {
			_, err = fmt.Fprintln(writer)
		}
	}
	if err != nil {
		return fmt.Errorf("packedVectorOutput: %w", err)
	}
	return nil
}

func writeUint8Rows(writer *bufio.Writer, data []byte, rowName string) error {
	_, err := fmt.Fprintf(writer, "\t\t\t\t\tNUMROWS %d\n", len(data)/4)
	for offset := 0; offset < len(data) && err == nil; offset += 4 {
		_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t%s %d %d %d %d\n", rowName, data[offset], data[offset+1], data[offset+2], data[offset+3])
	}
	if err != nil {
		return fmt.Errorf("uint8Output: %w", err)
	}
	return nil
}

func writeUint32Rows(writer *bufio.Writer, data []byte, rowName string) error {
	_, err := fmt.Fprintf(writer, "\t\t\t\t\tNUMROWS %d\n", len(data)/4)
	for offset := 0; offset < len(data) && err == nil; offset += 4 {
		_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t%s %d\n", rowName, binary.LittleEndian.Uint32(data[offset:]))
	}
	if err != nil {
		return fmt.Errorf("uint32Output: %w", err)
	}
	return nil
}

func primitiveName(primitiveType uint32) string {
	if primitiveType == 4 {
		return "TRIANGLELIST"
	}
	return "UNKNOWN"
}

func writeOptionalReference(writer *bufio.Writer, indent, name string, reference uint32) error {
	if reference == math.MaxUint32 {
		_, err := fmt.Fprintf(writer, "%s%s NULL\n", indent, name)
		if err != nil {
			return fmt.Errorf("nullOutput: %w", err)
		}
		return nil
	}
	_, err := fmt.Fprintf(writer, "%s%s 0x%08X\n", indent, name, reference)
	if err != nil {
		return fmt.Errorf("referenceOutput: %w", err)
	}
	return nil
}

func writeIndexRows(writer *bufio.Writer, indent string, count uint32, stride uint16, data []byte) error {
	if stride != 2 && stride != 4 {
		return writeByteRows(writer, indent, data, int(stride))
	}
	_, err := fmt.Fprintf(writer, "%sNUMROWS %d\n", indent, count)
	for index := uint32(0); index < count && err == nil; index++ {
		offset := int(index) * int(stride)
		item := uint32(binary.LittleEndian.Uint16(data[offset:]))
		if stride == 4 {
			item = binary.LittleEndian.Uint32(data[offset:])
		}
		_, err = fmt.Fprintf(writer, "%s\tINDEX %d\n", indent, item)
	}
	if err != nil {
		return fmt.Errorf("indexOutput: %w", err)
	}
	return nil
}

func writeByteRows(writer *bufio.Writer, indent string, data []byte, rowSize int) error {
	if rowSize <= 0 {
		rowSize = dseRowSize
	}
	rowCount := (len(data) + rowSize - 1) / rowSize
	_, err := fmt.Fprintf(writer, "%sNUMROWS %d\n", indent, rowCount)
	for offset := 0; offset < len(data) && err == nil; offset += rowSize {
		end := offset + rowSize
		if end > len(data) {
			end = len(data)
		}
		_, err = fmt.Fprintf(writer, "%s\tBYTES", indent)
		for _, item := range data[offset:end] {
			if err == nil {
				_, err = fmt.Fprintf(writer, " %d", item)
			}
		}
		if err == nil {
			_, err = fmt.Fprintln(writer)
		}
	}
	if err != nil {
		return fmt.Errorf("byteOutput: %w", err)
	}
	return nil
}

func formatFloat(number float32) string {
	return strconv.FormatFloat(float64(number), 'g', -1, 32)
}

// ReadDSE parses a strict editable GMSH definition and rebuilds its payload.
func ReadDSE(r io.Reader, identity string) ([]byte, error) {
	parser := newDSEParser(r)
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definition: %w", err)
	}
	if len(definition) != 2 || definition[0] != "GMSH" && definition[0] != "DARKSPINGMSH" || definition[1] != identity {
		return nil, fmt.Errorf("definition: got %v, want GMSH %q", definition, identity)
	}
	version, err := parser.uintProperty("VERSION", 32)
	if err != nil || version != 1 {
		return nil, fmt.Errorf("version: got %d: %w", version, err)
	}
	formatVersion, err := parser.uintProperty("FORMATVERSION", 32)
	if err != nil {
		return nil, fmt.Errorf("formatVersion: %w", err)
	}
	meshCount, err := parser.countProperty("NUMMESHES")
	if err != nil {
		return nil, fmt.Errorf("meshCount: %w", err)
	}
	document := &Document{Version: uint32(formatVersion), Meshes: make([]Mesh, meshCount)}
	for meshIndex := range document.Meshes {
		_, err = parser.named("MESH", fmt.Sprintf("mesh%d", meshIndex))
		if err != nil {
			return nil, fmt.Errorf("mesh[%d]: %w", meshIndex, err)
		}
		meshVersion, readErr := parser.uintProperty("VERSION", 16)
		if readErr != nil {
			return nil, fmt.Errorf("meshVersion[%d]: %w", meshIndex, readErr)
		}
		streamCount, readErr := parser.countProperty("NUMSTREAMS")
		if readErr != nil {
			return nil, fmt.Errorf("streamCount[%d]: %w", meshIndex, readErr)
		}
		mesh := Mesh{Version: uint16(meshVersion), Streams: make([]Stream, streamCount)}
		for streamIndex := range mesh.Streams {
			_, readErr = parser.named("STREAM", fmt.Sprintf("stream%d", streamIndex))
			if readErr != nil {
				return nil, fmt.Errorf("stream[%d:%d]: %w", meshIndex, streamIndex, readErr)
			}
			usageName, usageErr := parser.property("USAGE", 1)
			if usageErr != nil {
				return nil, fmt.Errorf("streamUsage[%d:%d]: %w", meshIndex, streamIndex, usageErr)
			}
			stream, streamErr := parser.stream(usageName[0])
			if streamErr != nil {
				return nil, fmt.Errorf("streamData[%d:%d]: %w", meshIndex, streamIndex, streamErr)
			}
			mesh.Streams[streamIndex] = stream
		}
		bufferCount, readErr := parser.countProperty("NUMBUFFERS")
		if readErr != nil {
			return nil, fmt.Errorf("bufferCount[%d]: %w", meshIndex, readErr)
		}
		mesh.Buffers = make([]Buffer, bufferCount)
		for bufferIndex := range mesh.Buffers {
			_, readErr = parser.named("BUFFER", fmt.Sprintf("buffer%d", bufferIndex))
			if readErr != nil {
				return nil, fmt.Errorf("buffer[%d:%d]: %w", meshIndex, bufferIndex, readErr)
			}
			buffer, bufferErr := parser.buffer()
			if bufferErr != nil {
				return nil, fmt.Errorf("bufferData[%d:%d]: %w", meshIndex, bufferIndex, bufferErr)
			}
			mesh.Buffers[bufferIndex] = buffer
		}
		drawCount, readErr := parser.countProperty("NUMDRAWS")
		if readErr != nil {
			return nil, fmt.Errorf("drawCount[%d]: %w", meshIndex, readErr)
		}
		mesh.Draws = make([]Draw, drawCount)
		for drawIndex := range mesh.Draws {
			_, drawErr := parser.named("DRAW", fmt.Sprintf("draw%d", drawIndex))
			if drawErr != nil {
				return nil, fmt.Errorf("draw[%d:%d]: %w", meshIndex, drawIndex, drawErr)
			}
			primitiveNameFields, drawErr := parser.property("PRIMITIVETYPE", 1)
			if drawErr != nil {
				return nil, fmt.Errorf("drawPrimitiveName[%d:%d]: %w", meshIndex, drawIndex, drawErr)
			}
			primitiveCode, drawErr := parser.uintProperty("PRIMITIVECODE", 32)
			if drawErr != nil || primitiveNameFields[0] != primitiveName(uint32(primitiveCode)) {
				return nil, fmt.Errorf("drawPrimitive[%d:%d]: name %q, code %d: %w", meshIndex, drawIndex, primitiveNameFields[0], primitiveCode, drawErr)
			}
			bufferIndex, drawErr := parser.uintProperty("BUFFER", 32)
			if drawErr != nil {
				return nil, fmt.Errorf("drawBuffer[%d:%d]: %w", meshIndex, drawIndex, drawErr)
			}
			firstIndex, drawErr := parser.uintProperty("FIRSTINDEX", 32)
			if drawErr != nil {
				return nil, fmt.Errorf("drawFirstIndex[%d:%d]: %w", meshIndex, drawIndex, drawErr)
			}
			endIndex, drawErr := parser.uintProperty("ENDINDEX", 32)
			if drawErr != nil {
				return nil, fmt.Errorf("drawEndIndex[%d:%d]: %w", meshIndex, drawIndex, drawErr)
			}
			materialIndex, drawErr := parser.uintProperty("MATERIAL", 32)
			if drawErr != nil {
				return nil, fmt.Errorf("drawMaterial[%d:%d]: %w", meshIndex, drawIndex, drawErr)
			}
			mesh.Draws[drawIndex] = Draw{PrimitiveType: uint32(primitiveCode), BufferIndex: uint32(bufferIndex), FirstIndex: uint32(firstIndex), EndIndex: uint32(endIndex), MaterialIndex: uint32(materialIndex)}
		}
		document.Meshes[meshIndex] = mesh
	}
	materialSetCount, err := parser.countProperty("NUMMATERIALSETS")
	if err != nil {
		return nil, fmt.Errorf("materialSetCount: %w", err)
	}
	if materialSetCount != len(document.Meshes) {
		return nil, fmt.Errorf("materialSetCount: got %d, want %d", materialSetCount, len(document.Meshes))
	}
	document.MaterialSets = make([][]Material, materialSetCount)
	for setIndex := range document.MaterialSets {
		_, err = parser.named("MATERIALSET", fmt.Sprintf("materialSet%d", setIndex))
		if err != nil {
			return nil, fmt.Errorf("materialSet[%d]: %w", setIndex, err)
		}
		materialCount, readErr := parser.countProperty("NUMMATERIALS")
		if readErr != nil {
			return nil, fmt.Errorf("materialCount[%d]: %w", setIndex, readErr)
		}
		materials := make([]Material, materialCount)
		for materialIndex := range materials {
			_, readErr = parser.named("MATERIAL", fmt.Sprintf("material%d", materialIndex))
			if readErr != nil {
				return nil, fmt.Errorf("material[%d:%d]: %w", setIndex, materialIndex, readErr)
			}
			index, materialErr := parser.uintProperty("INDEX", 32)
			if materialErr != nil {
				return nil, fmt.Errorf("materialIndex[%d:%d]: %w", setIndex, materialIndex, materialErr)
			}
			shaderIndex, materialErr := parser.uintProperty("SHADER", 16)
			if materialErr != nil {
				return nil, fmt.Errorf("materialShader[%d:%d]: %w", setIndex, materialIndex, materialErr)
			}
			referenceA, materialErr := parser.optionalReference("REFERENCEA?")
			if materialErr != nil {
				return nil, fmt.Errorf("materialReferenceA[%d:%d]: %w", setIndex, materialIndex, materialErr)
			}
			referenceB, materialErr := parser.optionalReference("REFERENCEB?")
			if materialErr != nil {
				return nil, fmt.Errorf("materialReferenceB[%d:%d]: %w", setIndex, materialIndex, materialErr)
			}
			materials[materialIndex] = Material{Index: uint32(index), ShaderIndex: uint16(shaderIndex), ReferenceA: referenceA, ReferenceB: referenceB}
		}
		document.MaterialSets[setIndex] = materials
	}
	remaining, err := parser.next()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing: %w", err)
	}
	if len(remaining) != 0 {
		return nil, fmt.Errorf("trailing: %v", remaining)
	}
	payload, err := Encode(document)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return payload, nil
}

type dseParser struct {
	scanner *bufio.Scanner
	line    int
}

func newDSEParser(r io.Reader) *dseParser {
	return &dseParser{scanner: bufio.NewScanner(r)}
}

func (e *dseParser) next() ([]string, error) {
	for e.scanner.Scan() {
		e.line++
		line := strings.TrimSpace(e.scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		for fieldIndex := range fields {
			if strings.HasPrefix(fields[fieldIndex], "\"") {
				unquoted, err := strconv.Unquote(fields[fieldIndex])
				if err != nil {
					return nil, fmt.Errorf("line[%d]: %w", e.line, err)
				}
				fields[fieldIndex] = unquoted
			}
		}
		return fields, nil
	}
	if err := e.scanner.Err(); err != nil {
		return nil, fmt.Errorf("lineRead: %w", err)
	}
	return nil, io.EOF
}

func (e *dseParser) property(name string, argumentCount int) ([]string, error) {
	fields, err := e.next()
	if err != nil {
		return nil, fmt.Errorf("%sRead: %w", name, err)
	}
	if len(fields) != argumentCount+1 || fields[0] != name {
		return nil, fmt.Errorf("line[%d]: expected %s with %d arguments, got %v", e.line, name, argumentCount, fields)
	}
	return fields[1:], nil
}

func (e *dseParser) named(kind, name string) ([]string, error) {
	fields, err := e.property(kind, 1)
	if err != nil {
		return nil, fmt.Errorf("property: %w", err)
	}
	if fields[0] != name {
		return nil, fmt.Errorf("name: got %q, want %q", fields[0], name)
	}
	return fields, nil
}

func (e *dseParser) uintProperty(name string, bits int) (uint64, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseUint(fields[0], 0, bits)
	if err != nil {
		return 0, fmt.Errorf("parse: %w", err)
	}
	return number, nil
}

func (e *dseParser) countProperty(name string) (int, error) {
	number, err := e.uintProperty(name, 31)
	if err != nil {
		return 0, err
	}
	return int(number), nil
}

func (e *dseParser) optionalReference(name string) (uint32, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, fmt.Errorf("property: %w", err)
	}
	if fields[0] == "NULL" {
		return math.MaxUint32, nil
	}
	reference, err := strconv.ParseUint(fields[0], 0, 32)
	if err != nil {
		return 0, fmt.Errorf("referenceParse: %w", err)
	}
	return uint32(reference), nil
}

func (e *dseParser) stream(usageName string) (Stream, error) {
	usage, err := e.uintProperty("USAGECODE", 8)
	if err != nil {
		return Stream{}, fmt.Errorf("usageCode: %w", err)
	}
	usageIndex, err := e.uintProperty("USAGEINDEX", 8)
	if err != nil {
		return Stream{}, fmt.Errorf("usageIndex: %w", err)
	}
	format, err := e.uintProperty("FORMATCODE", 8)
	if err != nil {
		return Stream{}, fmt.Errorf("formatCode: %w", err)
	}
	field03, err := e.uintProperty("FIELD03", 8)
	if err != nil {
		return Stream{}, fmt.Errorf("field03: %w", err)
	}
	count, err := e.uintProperty("COUNT", 32)
	if err != nil {
		return Stream{}, fmt.Errorf("count: %w", err)
	}
	stride, err := e.uintProperty("STRIDE", 16)
	if err != nil {
		return Stream{}, fmt.Errorf("stride: %w", err)
	}
	stream := Stream{Usage: uint8(usage), UsageIndex: uint8(usageIndex), Format: uint8(format), Field03: uint8(field03), Count: uint32(count), Stride: uint16(stride)}
	if streamUsageName(stream) != usageName {
		return Stream{}, fmt.Errorf("usageMismatch: declaration is %q, got %q", streamUsageName(stream), usageName)
	}
	switch usageName {
	case "POSITION", "VECTOR3":
		stream.Data, err = e.floatRows(stream.Count, 3, usageName)
	case "NORMAL", "TANGENT":
		stream.Data, err = e.packedVectorRows(stream.Count, usageName)
	case "TEXCOORD":
		stream.Data, err = e.floatRows(stream.Count, 2, usageName)
	case "BONEINDICES", "BONEWEIGHTS":
		stream.Data, err = e.uint8Rows(stream.Count, usageName)
	case "MATRIX3X4":
		stream.Data, err = e.floatRows(stream.Count, 12, usageName)
	case "MATERIALLOOKUP":
		stream.Data, err = e.uint32Rows(stream.Count, usageName)
	case "UNCLASSIFIED":
		stream.Data, err = e.byteRows(int(stream.Stride), true)
	default:
		return Stream{}, fmt.Errorf("usageUnsupported: %q", usageName)
	}
	if err != nil {
		return Stream{}, fmt.Errorf("rows: %w", err)
	}
	if len(stream.Data) != int(stream.Count)*int(stream.Stride) {
		return Stream{}, fmt.Errorf("size: got %d, want %d*%d", len(stream.Data), stream.Count, stream.Stride)
	}
	return stream, nil
}

func (e *dseParser) floatRows(count uint32, componentCount int, rowName string) ([]byte, error) {
	rowCount, err := e.countProperty("NUMROWS")
	if err != nil {
		return nil, fmt.Errorf("rowCount: %w", err)
	}
	if uint32(rowCount) != count {
		return nil, fmt.Errorf("rowCount: got %d, want %d", rowCount, count)
	}
	data := make([]byte, 0, rowCount*componentCount*4)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		fields, readErr := e.property(rowName, componentCount)
		if readErr != nil {
			return nil, fmt.Errorf("row[%d]: %w", rowIndex, readErr)
		}
		for coordinateIndex, field := range fields {
			coordinate, parseErr := strconv.ParseFloat(field, 32)
			if parseErr != nil {
				return nil, fmt.Errorf("coordinate[%d:%d]: %w", rowIndex, coordinateIndex, parseErr)
			}
			var encoded [4]byte
			binary.LittleEndian.PutUint32(encoded[:], math.Float32bits(float32(coordinate)))
			data = append(data, encoded[:]...)
		}
	}
	return data, nil
}

func (e *dseParser) packedVectorRows(count uint32, rowName string) ([]byte, error) {
	rowCount, err := e.countProperty("NUMROWS")
	if err != nil {
		return nil, fmt.Errorf("rowCount: %w", err)
	}
	if uint32(rowCount) != count {
		return nil, fmt.Errorf("rowCount: got %d, want %d", rowCount, count)
	}
	data := make([]byte, 0, rowCount*4)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		fields, readErr := e.property(rowName, 4)
		if readErr != nil {
			return nil, fmt.Errorf("row[%d]: %w", rowIndex, readErr)
		}
		for componentIndex, field := range fields {
			component, parseErr := strconv.ParseFloat(field, 32)
			if parseErr != nil {
				return nil, fmt.Errorf("componentParse[%d:%d]: %w", rowIndex, componentIndex, parseErr)
			}
			if component < -1 || component > float64(128)/127 {
				return nil, fmt.Errorf("componentRange[%d:%d]: %q", rowIndex, componentIndex, field)
			}
			encoded := math.Round((component + 1) * 127)
			if encoded < 0 || encoded > math.MaxUint8 {
				return nil, fmt.Errorf("componentRange[%d:%d]: %s", rowIndex, componentIndex, field)
			}
			data = append(data, byte(encoded))
		}
	}
	return data, nil
}

func (e *dseParser) uint8Rows(count uint32, rowName string) ([]byte, error) {
	rowCount, err := e.countProperty("NUMROWS")
	if err != nil {
		return nil, fmt.Errorf("rowCount: %w", err)
	}
	if uint32(rowCount) != count {
		return nil, fmt.Errorf("rowCount: got %d, want %d", rowCount, count)
	}
	data := make([]byte, 0, rowCount*4)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		fields, readErr := e.property(rowName, 4)
		if readErr != nil {
			return nil, fmt.Errorf("row[%d]: %w", rowIndex, readErr)
		}
		row, parseErr := parseBytes(fields)
		if parseErr != nil {
			return nil, fmt.Errorf("rowBytes[%d]: %w", rowIndex, parseErr)
		}
		data = append(data, row...)
	}
	return data, nil
}

func (e *dseParser) uint32Rows(count uint32, rowName string) ([]byte, error) {
	rowCount, err := e.countProperty("NUMROWS")
	if err != nil {
		return nil, fmt.Errorf("rowCount: %w", err)
	}
	if uint32(rowCount) != count {
		return nil, fmt.Errorf("rowCount: got %d, want %d", rowCount, count)
	}
	data := make([]byte, 0, rowCount*4)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		fields, readErr := e.property(rowName, 1)
		if readErr != nil {
			return nil, fmt.Errorf("row[%d]: %w", rowIndex, readErr)
		}
		item, parseErr := strconv.ParseUint(fields[0], 0, 32)
		if parseErr != nil {
			return nil, fmt.Errorf("item[%d]: %w", rowIndex, parseErr)
		}
		var encoded [4]byte
		binary.LittleEndian.PutUint32(encoded[:], uint32(item))
		data = append(data, encoded[:]...)
	}
	return data, nil
}

func (e *dseParser) buffer() (Buffer, error) {
	count, err := e.uintProperty("COUNT", 32)
	if err != nil {
		return Buffer{}, fmt.Errorf("count: %w", err)
	}
	stride, err := e.uintProperty("STRIDE", 16)
	if err != nil {
		return Buffer{}, fmt.Errorf("stride: %w", err)
	}
	buffer := Buffer{Count: uint32(count), Stride: uint16(stride)}
	buffer.Data, err = e.indexRows(buffer.Count, buffer.Stride)
	if err != nil {
		return Buffer{}, fmt.Errorf("indexes: %w", err)
	}
	field, err := e.uintProperty("FIELD", 32)
	if err != nil {
		return Buffer{}, fmt.Errorf("field: %w", err)
	}
	buffer.Field = uint32(field)
	remapCount, err := e.countProperty("NUMREMAPS")
	if err != nil {
		return Buffer{}, fmt.Errorf("remapCount: %w", err)
	}
	if remapCount > math.MaxUint8 {
		return Buffer{}, fmt.Errorf("remapCount: %d", remapCount)
	}
	buffer.Remaps = make([][2]uint16, remapCount)
	for remapIndex := range buffer.Remaps {
		fields, readErr := e.property("REMAP", 2)
		if readErr != nil {
			return Buffer{}, fmt.Errorf("remap[%d]: %w", remapIndex, readErr)
		}
		for itemIndex, field := range fields {
			item, parseErr := strconv.ParseUint(field, 0, 16)
			if parseErr != nil {
				return Buffer{}, fmt.Errorf("remap[%d:%d]: %w", remapIndex, itemIndex, parseErr)
			}
			buffer.Remaps[remapIndex][itemIndex] = uint16(item)
		}
	}
	auxiliaryCount, err := e.countProperty("NUMAUXILIARY")
	if err != nil {
		return Buffer{}, fmt.Errorf("auxiliaryCount: %w", err)
	}
	if auxiliaryCount > math.MaxUint8 {
		return Buffer{}, fmt.Errorf("auxiliaryCount: %d", auxiliaryCount)
	}
	buffer.Auxiliary = make([]Blob, auxiliaryCount)
	for auxiliaryIndex := range buffer.Auxiliary {
		_, err = e.named("AUXILIARY", fmt.Sprintf("auxiliary%d", auxiliaryIndex))
		if err != nil {
			return Buffer{}, fmt.Errorf("auxiliaryName[%d]: %w", auxiliaryIndex, err)
		}
		auxiliaryItemCount, readErr := e.uintProperty("COUNT", 32)
		if readErr != nil {
			return Buffer{}, fmt.Errorf("auxiliaryCount[%d]: %w", auxiliaryIndex, readErr)
		}
		auxiliaryStride, readErr := e.uintProperty("STRIDE", 16)
		if readErr != nil {
			return Buffer{}, fmt.Errorf("auxiliaryStride[%d]: %w", auxiliaryIndex, readErr)
		}
		data, readErr := e.byteRows(int(auxiliaryStride), true)
		if readErr != nil {
			return Buffer{}, fmt.Errorf("auxiliaryRows[%d]: %w", auxiliaryIndex, readErr)
		}
		if len(data) != int(auxiliaryItemCount)*int(auxiliaryStride) {
			return Buffer{}, fmt.Errorf("auxiliarySize[%d]: got %d, want %d*%d", auxiliaryIndex, len(data), auxiliaryItemCount, auxiliaryStride)
		}
		buffer.Auxiliary[auxiliaryIndex] = Blob{Count: uint32(auxiliaryItemCount), Stride: uint16(auxiliaryStride), Data: data}
	}
	return buffer, nil
}

func (e *dseParser) indexRows(count uint32, stride uint16) ([]byte, error) {
	if stride != 2 && stride != 4 {
		data, err := e.byteRows(int(stride), true)
		if err != nil {
			return nil, fmt.Errorf("byteRows: %w", err)
		}
		if len(data) != int(count)*int(stride) {
			return nil, fmt.Errorf("indexSize: got %d, want %d*%d", len(data), count, stride)
		}
		return data, nil
	}
	rowCount, err := e.countProperty("NUMROWS")
	if err != nil {
		return nil, fmt.Errorf("rowCount: %w", err)
	}
	if uint32(rowCount) != count {
		return nil, fmt.Errorf("indexCount: got %d, want %d", rowCount, count)
	}
	data := make([]byte, 0, rowCount*int(stride))
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		fields, readErr := e.property("INDEX", 1)
		if readErr != nil {
			return nil, fmt.Errorf("index[%d]: %w", rowIndex, readErr)
		}
		item, parseErr := strconv.ParseUint(fields[0], 0, int(stride)*8)
		if parseErr != nil {
			return nil, fmt.Errorf("index[%d]: %w", rowIndex, parseErr)
		}
		var encoded [4]byte
		if stride == 2 {
			binary.LittleEndian.PutUint16(encoded[:], uint16(item))
		} else {
			binary.LittleEndian.PutUint32(encoded[:], uint32(item))
		}
		data = append(data, encoded[:stride]...)
	}
	return data, nil
}

func (e *dseParser) byteRows(rowSize int, isExact bool) ([]byte, error) {
	rowCount, err := e.countProperty("NUMROWS")
	if err != nil {
		return nil, fmt.Errorf("rowCount: %w", err)
	}
	data := make([]byte, 0, rowCount*rowSize)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		fields, readErr := e.next()
		if readErr != nil {
			return nil, fmt.Errorf("row[%d]: %w", rowIndex, readErr)
		}
		if len(fields) < 2 || fields[0] != "BYTES" {
			return nil, fmt.Errorf("row[%d]: got %v", rowIndex, fields)
		}
		if isExact && len(fields)-1 != rowSize {
			return nil, fmt.Errorf("rowSize[%d]: got %d, want %d", rowIndex, len(fields)-1, rowSize)
		}
		row, parseErr := parseBytes(fields[1:])
		if parseErr != nil {
			return nil, fmt.Errorf("rowBytes[%d]: %w", rowIndex, parseErr)
		}
		data = append(data, row...)
	}
	return data, nil
}

func parseBytes(fields []string) ([]byte, error) {
	data := make([]byte, len(fields))
	for fieldIndex, field := range fields {
		item, err := strconv.ParseUint(field, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("byte[%d]: %w", fieldIndex, err)
		}
		data[fieldIndex] = byte(item)
	}
	return data, nil
}

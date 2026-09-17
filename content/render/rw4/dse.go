package rw4

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const dseLineSize = 32

type dseSpan struct {
	offset int
	data   []byte
}

// WriteDSE writes a decoded RW4 resource with semantic known sections and
// bounded hexadecimal storage for layout, base-resource, and unknown bytes.
func WriteDSE(w io.Writer, identity string, document *Document) error {
	if document == nil || len(document.Payload) == 0 {
		return errors.New("nil document")
	}
	spans, err := document.layoutSpans()
	if err != nil {
		return fmt.Errorf("layout: %w", err)
	}
	writer := bufio.NewWriter(w)
	_, err = fmt.Fprintln(writer, "// darkspin resource dse v1")
	if err == nil {
		_, err = fmt.Fprintf(writer, "RW4 %q\n", identity)
	}
	lines := []string{
		"\tVERSION 1",
		fmt.Sprintf("\tTYPE 0x%08X", document.Type),
		fmt.Sprintf("\tBUFFEROFFSET %d", document.BufferOffset),
		fmt.Sprintf("\tSECTIONOFFSET %d", document.SectionOffset),
		fmt.Sprintf("\tNUMLAYOUTS %d", len(spans)),
	}
	for _, line := range lines {
		if err == nil {
			_, err = fmt.Fprintln(writer, line)
		}
	}
	for spanIndex, span := range spans {
		if err != nil {
			break
		}
		_, err = fmt.Fprintf(writer, "\t\tLAYOUT %q\n", fmt.Sprintf("layout%d", spanIndex))
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tOFFSET %d\n", span.offset)
		}
		if err == nil {
			err = writeDSEHex(writer, "\t\t\t", span.data)
		}
	}
	if err == nil {
		err = document.writeDSERenderAssets(writer)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMSECTIONS %d\n", len(document.Sections))
	}
	for _, section := range document.Sections {
		if err != nil {
			break
		}
		err = document.writeDSESection(writer, section)
	}
	if err == nil {
		err = writer.Flush()
	}
	if err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

func (e *Document) layoutSpans() ([]dseSpan, error) {
	sections := append([]Section(nil), e.Sections...)
	sort.Slice(sections, func(left, right int) bool { return sections[left].Offset < sections[right].Offset })
	spans := make([]dseSpan, 0, len(sections)+1)
	offset := 0
	for _, section := range sections {
		start := int(section.Offset)
		end := start + int(section.Size)
		if start < offset || end > len(e.Payload) {
			return nil, fmt.Errorf("section[%d]: invalid %d:%d", section.Ordinal, start, end)
		}
		if start > offset {
			spans = append(spans, dseSpan{offset: offset, data: e.Payload[offset:start]})
		}
		offset = end
	}
	if offset < len(e.Payload) {
		spans = append(spans, dseSpan{offset: offset, data: e.Payload[offset:]})
	}
	return spans, nil
}

func (e *Document) writeDSESection(writer *bufio.Writer, section Section) error {
	kind := e.dseSectionKind(section)
	_, err := fmt.Fprintf(writer, "\t\t%s %q // %d\n", sectionDeclaration(kind), sectionTag(section.Ordinal), section.Ordinal)
	metadata := []string{
		fmt.Sprintf("\t\t\tOFFSET %d", section.Offset),
		fmt.Sprintf("\t\t\tFIELD04 0x%08X", section.Field04),
		fmt.Sprintf("\t\t\tSIZE %d", section.Size),
		fmt.Sprintf("\t\t\tALIGNMENT %d", section.Alignment),
		fmt.Sprintf("\t\t\tTYPECODEINDEX %d", section.TypeCodeIndex),
		fmt.Sprintf("\t\t\tTYPECODE 0x%08X", section.TypeCode),
	}
	for _, line := range metadata {
		if err == nil {
			_, err = fmt.Fprintln(writer, line)
		}
	}
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	err = e.writeDSEFields(writer, section)
	if err != nil {
		return fmt.Errorf("fields: %w", err)
	}
	if e.isSemanticDSESection(section) {
		return nil
	}
	err = writeDSEHex(writer, "\t\t\t", section.Data)
	if err != nil {
		return fmt.Errorf("bytes: %w", err)
	}
	return nil
}

func isSemanticSection(typeCode uint32) bool {
	switch typeCode {
	case TypeRaster, TypeVertexDescription, TypeVertexBuffer, TypeIndexBuffer, TypeMesh, TypeMeshStateLink,
		TypeKeyframeAnimation, TypeSkeleton, TypeAnimationSkin, TypeSkinMatrixBuffer, TypeSkinsInK, TypeBoundingBox, TypeMorphHandle, TypeAnimations:
		return true
	default:
		return false
	}
}

func (e *Document) dseSectionKind(section Section) string {
	if section.TypeCode == TypeBaseResource {
		if _, isFound := e.vertexDataInfo(section.Ordinal); isFound {
			return "VERTEXDATA"
		}
		if _, isFound := e.indexDataInfo(section.Ordinal); isFound {
			return "INDEXDATA"
		}
	}
	return sectionKind(section.TypeCode)
}

func (e *Document) isSemanticDSESection(section Section) bool {
	if isSemanticSection(section.TypeCode) {
		return true
	}
	return e.dseSectionKind(section) == "VERTEXDATA" || e.dseSectionKind(section) == "INDEXDATA"
}

func (e *Document) writeDSEFields(writer *bufio.Writer, section Section) error {
	switch section.TypeCode {
	case TypeBaseResource:
		return e.writeDSEBuffer(writer, section)
	case TypeVertexDescription:
		description := e.VertexFormats[section.Ordinal]
		_, err := fmt.Fprintf(writer, "\t\t\tFIELD00 0x%08X\n\t\t\tFIELD04 0x%08X\n\t\t\tDECLARATION 0x%08X\n\t\t\tFIELD0E %d\n\t\t\tVERTEXSIZE %d\n\t\t\tELEMENTFLAGS 0x%08X\n\t\t\tFIELD14 0x%08X\n\t\t\tNUMELEMENTS %d\n", description.Field00, description.Field04, description.Declaration, description.Field0E, description.VertexSize, description.ElementFlags, description.Field14, len(description.Elements))
		for elementIndex, element := range description.Elements {
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\tELEMENT %q\n\t\t\t\t\tSTREAM %d\n\t\t\t\t\tOFFSET %d\n\t\t\t\t\tDATATYPE %q\n\t\t\t\t\tMETHOD %d\n\t\t\t\t\tUSAGE %q\n\t\t\t\t\tUSAGEINDEX %d\n\t\t\t\t\tTYPECODE 0x%08X\n", fmt.Sprintf("element%d", elementIndex), element.Stream, element.Offset, vertexDataTypeName(element.DataType), element.Method, vertexUsageName(element.Usage), element.UsageIndex, element.TypeCode)
			}
		}
		return dseWriteError("vertexDescription", err)
	case TypeVertexBuffer:
		buffer := e.VertexBuffers[section.Ordinal]
		_, err := fmt.Fprintf(writer, "\t\t\tDESCRIPTION %s\n\t\t\tFIELD04 0x%08X\n\t\t\tBASEVERTEX %d\n\t\t\tNUMVERTICES %d\n\t\t\tFIELD10 0x%08X\n\t\t\tVERTEXSIZE %d\n\t\t\tDATA? %s\n", referenceTag(buffer.DescriptionRef), buffer.Field04, buffer.BaseVertex, buffer.VertexCount, buffer.Field10, buffer.VertexSize, referenceTag(buffer.DataRef))
		return dseWriteError("vertexBuffer", err)
	case TypeIndexBuffer:
		buffer := e.IndexBuffers[section.Ordinal]
		_, err := fmt.Fprintf(writer, "\t\t\tDECLARATION 0x%08X\n\t\t\tSTARTINDEX %d\n\t\t\tNUMPRIMITIVES %d\n\t\t\tUSAGE 0x%08X\n\t\t\tFORMAT %q\n\t\t\tPRIMITIVETYPE %q\n\t\t\tDATA? %s\n", buffer.Declaration, buffer.StartIndex, buffer.PrimitiveCount, buffer.Usage, indexFormatName(buffer.Format), primitiveTypeName(buffer.PrimitiveType), referenceTag(buffer.DataRef))
		return dseWriteError("indexBuffer", err)
	case TypeMesh:
		mesh := e.Meshes[section.Ordinal]
		_, err := fmt.Fprintf(writer, "\t\t\tFIELD00 0x%08X\n\t\t\tPRIMITIVETYPE %q\n\t\t\tINDEXBUFFER %s\n\t\t\tNUMTRIANGLES %d\n\t\t\tNUMVERTEXBUFFERS %d\n", mesh.Field00, primitiveTypeName(mesh.PrimitiveType), referenceTag(mesh.IndexBufferRef), mesh.TriangleCount, len(mesh.VertexBufferRefs))
		for _, reference := range mesh.VertexBufferRefs {
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\tVERTEXBUFFER %s\n", referenceTag(reference))
			}
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tFIRSTINDEX %d\n\t\t\tNUMPRIMITIVES %d\n\t\t\tFIRSTVERTEX %d\n\t\t\tNUMVERTICES %d\n", mesh.FirstIndex, mesh.PrimitiveCount, mesh.FirstVertex, mesh.VertexCount)
		}
		return dseWriteError("mesh", err)
	case TypeRaster:
		raster := e.Rasters[section.Ordinal]
		_, err := fmt.Fprintf(writer, "\t\t\tFORMAT %q\n\t\t\tFLAGS 0x%04X\n\t\t\tVOLUMEDEPTH %d\n\t\t\tDXTEXTURE 0x%08X\n\t\t\tWIDTH %d\n\t\t\tHEIGHT %d\n\t\t\tFIELD10 %d\n\t\t\tNUMMIPMAPS %d\n\t\t\tFIELD12 0x%04X\n\t\t\tFIELD14 0x%08X\n\t\t\tFIELD18 0x%08X\n\t\t\tDATA? %s\n", textureFormatName(raster.TextureFormat), raster.TextureFlags, raster.VolumeDepth, raster.DXTexture, raster.Width, raster.Height, raster.Field10, raster.MipmapLevels, raster.Field12, raster.Field14, raster.Field18, referenceTag(raster.DataRef))
		return dseWriteError("raster", err)
	case TypeMeshStateLink:
		link := e.MeshStateLinks[section.Ordinal]
		_, err := fmt.Fprintf(writer, "\t\t\tMESH %s\n\t\t\tNUMSTATES %d\n", referenceTag(link.MeshRef), len(link.CompiledStateRefs))
		for _, reference := range link.CompiledStateRefs {
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\tSTATE %s\n", referenceTag(reference))
			}
		}
		return dseWriteError("meshStateLink", err)
	case TypeKeyframeAnimation:
		return e.writeDSEKeyframeAnimation(writer, section)
	case TypeSkeleton:
		return e.writeDSESkeleton(writer, section)
	case TypeAnimationSkin:
		return e.writeDSEAnimationSkin(writer, section)
	case TypeSkinMatrixBuffer:
		return e.writeDSESkinMatrixBuffer(writer, section)
	case TypeSkinsInK:
		return writeDSESkinsInK(writer, section.Data)
	case TypeBoundingBox:
		return writeDSEBoundingBox(writer, section.Data)
	case TypeMorphHandle:
		return writeDSEMorphHandle(writer, section.Data)
	case TypeAnimations:
		return writeDSEAnimations(writer, section.Data)
	default:
		return nil
	}
}

func dseWriteError(operation string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func writeDSEHex(writer *bufio.Writer, indent string, data []byte) error {
	lineCount := (len(data) + dseLineSize - 1) / dseLineSize
	_, err := fmt.Fprintf(writer, "%sNUMLINES %d\n", indent, lineCount)
	for offset := 0; offset < len(data) && err == nil; offset += dseLineSize {
		end := offset + dseLineSize
		if end > len(data) {
			end = len(data)
		}
		_, err = fmt.Fprintf(writer, "%s\tHEX %s\n", indent, strings.ToUpper(hex.EncodeToString(data[offset:end])))
	}
	if err != nil {
		return fmt.Errorf("hexOutput: %w", err)
	}
	return nil
}

func sectionKind(typeCode uint32) string {
	switch typeCode {
	case TypeBaseResource:
		return "BASERESOURCE"
	case TypeRaster:
		return "RASTER"
	case TypeVertexDescription:
		return "VERTEXDESCRIPTION"
	case TypeVertexBuffer:
		return "VERTEXBUFFER"
	case TypeIndexBuffer:
		return "INDEXBUFFER"
	case TypeMesh:
		return "MESH"
	case TypeCompiledState:
		return "COMPILEDSTATE"
	case TypeMeshStateLink:
		return "MESHSTATELINK"
	case TypeKeyframeAnimation:
		return "KEYFRAMEANIMATION"
	case TypeSkeleton:
		return "SKELETON"
	case TypeAnimationSkin:
		return "ANIMATIONSKIN"
	case TypeSkeletonBinding:
		return "SKELETONBINDING"
	case TypeSkinsInK:
		return "SKINBINDING"
	case TypeSkinMatrixBuffer:
		return "SKINMATRIXBUFFER"
	case TypeBoundingBox:
		return "BOUNDINGBOX"
	case TypeMorphHandle:
		return "MORPHHANDLE"
	case TypeAnimations:
		return "ANIMATIONS"
	default:
		return "OPAQUE"
	}
}

func isDSESectionKind(typeCode uint32, kind string) bool {
	if kind == "OPAQUE" && isSemanticSection(typeCode) {
		return true
	}
	if typeCode == TypeBaseResource {
		return kind == "BASERESOURCE" || kind == "VERTEXDATA" || kind == "INDEXDATA"
	}
	return kind == sectionKind(typeCode)
}

func sectionDeclaration(kind string) string {
	return "RW4" + kind
}

func sectionDeclarationKind(declaration string) (string, bool) {
	if !strings.HasPrefix(declaration, "RW4") || len(declaration) == 3 {
		return "", false
	}
	return strings.TrimPrefix(declaration, "RW4"), true
}

func sectionTag(ordinal int) string { return fmt.Sprintf("section%d", ordinal) }

func referenceTag(reference uint32) string {
	if reference>>22 == 0 {
		return strconv.Quote(sectionTag(int(reference & 0x003FFFFF)))
	}
	if reference>>22 == 1 || reference == ^uint32(0) {
		return "NULL"
	}
	return fmt.Sprintf("0x%08X", reference)
}

func vertexDataTypeName(dataType uint8) string {
	switch dataType {
	case dataFloat2:
		return "FLOAT2"
	case dataFloat3:
		return "FLOAT3"
	case dataUByte4:
		return "UBYTE4"
	case dataUByte4N:
		return "UBYTE4N"
	default:
		return fmt.Sprintf("0x%02X", dataType)
	}
}

func vertexUsageName(usage uint8) string {
	switch usage {
	case usagePosition:
		return "POSITION"
	case usageNormal:
		return "NORMAL"
	case usageTexCoord0:
		return "TEXCOORD"
	default:
		return fmt.Sprintf("0x%02X", usage)
	}
}

func indexFormatName(format uint32) string {
	switch format {
	case formatIndex16:
		return "UINT16"
	case formatIndex32:
		return "UINT32"
	default:
		return fmt.Sprintf("0x%08X", format)
	}
}

func primitiveTypeName(primitiveType uint32) string {
	if primitiveType == PrimitiveTriangleList {
		return "TRIANGLELIST"
	}
	return fmt.Sprintf("0x%08X", primitiveType)
}

func textureFormatName(format uint32) string {
	switch format {
	case formatR8G8B8:
		return "R8G8B8"
	case formatA8R8G8B8:
		return "A8R8G8B8"
	case formatA8:
		return "A8"
	case formatDXT1:
		return "DXT1"
	case formatDXT3:
		return "DXT3"
	case formatDXT5:
		return "DXT5"
	default:
		return fmt.Sprintf("0x%08X", format)
	}
}

// ReadDSE strictly parses semantic RW4 DSE and rebuilds the complete decoded
// RW4 byte stream. Known fields replace their bounded section bytes.
func ReadDSE(r io.Reader, identity string) ([]byte, error) {
	parser := newRW4Parser(r)
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definition: %w", err)
	}
	if len(definition) != 2 || definition[0] != "RW4" && definition[0] != "DARKSPINRW4" || definition[1] != identity {
		return nil, fmt.Errorf("definition: got %v", definition)
	}
	version, err := parser.uint32("VERSION")
	if err != nil || version != 1 {
		return nil, fmt.Errorf("version: %d: %v", version, err)
	}
	documentType, err := parser.uint32("TYPE")
	if err != nil {
		return nil, fmt.Errorf("type: %w", err)
	}
	bufferOffset, err := parser.uint32("BUFFEROFFSET")
	if err != nil {
		return nil, fmt.Errorf("bufferOffset: %w", err)
	}
	sectionOffset, err := parser.uint32("SECTIONOFFSET")
	if err != nil {
		return nil, fmt.Errorf("sectionOffset: %w", err)
	}
	spans := make([]dseSpan, 0)
	layoutCount, err := parser.count("NUMLAYOUTS")
	if err != nil {
		return nil, fmt.Errorf("layoutCount: %w", err)
	}
	for layoutIndex := 0; layoutIndex < layoutCount; layoutIndex++ {
		layout, readErr := parser.property("LAYOUT", 1)
		if readErr != nil {
			return nil, fmt.Errorf("layout[%d]: %w", layoutIndex, readErr)
		}
		if layout[0] != fmt.Sprintf("layout%d", layoutIndex) {
			return nil, fmt.Errorf("layoutName[%d]: %q", layoutIndex, layout[0])
		}
		offset, readErr := parser.uint32("OFFSET")
		if readErr != nil {
			return nil, fmt.Errorf("layoutOffset[%d]: %w", layoutIndex, readErr)
		}
		data, readErr := parser.hexBlock()
		if readErr != nil {
			return nil, fmt.Errorf("layoutData[%d]: %w", layoutIndex, readErr)
		}
		spans = append(spans, dseSpan{offset: int(offset), data: data})
	}
	renderAssets, err := readDSERenderAssets(parser)
	if err != nil {
		return nil, fmt.Errorf("renderAssets: %w", err)
	}
	sectionCount, err := parser.count("NUMSECTIONS")
	if err != nil {
		return nil, fmt.Errorf("sectionCount: %w", err)
	}
	sections := make([]Section, sectionCount)
	for ordinal := 0; ordinal < sectionCount; ordinal++ {
		sectionFields, readErr := parser.next()
		if readErr != nil {
			return nil, fmt.Errorf("section[%d]: %w", ordinal, readErr)
		}
		if len(sectionFields) != 2 || sectionFields[1] != sectionTag(ordinal) {
			return nil, fmt.Errorf("sectionName[%d]: %v", ordinal, sectionFields)
		}
		kind, isKindFound := sectionDeclarationKind(sectionFields[0])
		if !isKindFound {
			return nil, fmt.Errorf("sectionDeclaration[%d]: %q", ordinal, sectionFields[0])
		}
		section := Section{Ordinal: ordinal}
		section.Offset, readErr = parser.uint32("OFFSET")
		if readErr == nil {
			section.Field04, readErr = parser.uint32("FIELD04")
		}
		if readErr == nil {
			section.Size, readErr = parser.uint32("SIZE")
		}
		if readErr == nil {
			section.Alignment, readErr = parser.uint32("ALIGNMENT")
		}
		if readErr == nil {
			section.TypeCodeIndex, readErr = parser.uint32("TYPECODEINDEX")
		}
		if readErr == nil {
			section.TypeCode, readErr = parser.uint32("TYPECODE")
		}
		if readErr != nil {
			return nil, fmt.Errorf("sectionMetadata[%d]: %w", ordinal, readErr)
		}
		if !isDSESectionKind(section.TypeCode, kind) {
			return nil, fmt.Errorf("sectionKind[%d]: got %q for 0x%08X", ordinal, kind, section.TypeCode)
		}
		semanticData, readErr := readDSEFields(parser, section, kind)
		if readErr != nil {
			return nil, fmt.Errorf("sectionFields[%d]: %w", ordinal, readErr)
		}
		if semanticData != nil {
			section.Data = semanticData
		} else {
			section.Data, readErr = parser.hexBlock()
			if readErr != nil {
				return nil, fmt.Errorf("sectionData[%d]: %w", ordinal, readErr)
			}
		}
		if uint32(len(section.Data)) != section.Size {
			return nil, fmt.Errorf("sectionSize[%d]: got %d, want %d", ordinal, len(section.Data), section.Size)
		}
		sections[ordinal] = section
	}
	remaining, err := parser.next()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing: %w", err)
	}
	if len(remaining) != 0 {
		return nil, fmt.Errorf("trailing: %v", remaining)
	}
	payloadSize := uint64(len(magic))
	sectionTableEnd := uint64(sectionOffset) + uint64(sectionCount)*24
	if sectionTableEnd > payloadSize {
		payloadSize = sectionTableEnd
	}
	for _, span := range spans {
		end := uint64(span.offset) + uint64(len(span.data))
		if end > payloadSize {
			payloadSize = end
		}
	}
	for _, section := range sections {
		end := uint64(section.Offset) + uint64(len(section.Data))
		if end > payloadSize {
			payloadSize = end
		}
	}
	if payloadSize > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("payloadSize: %d exceeds int", payloadSize)
	}
	payload := make([]byte, int(payloadSize))
	occupied := make([]bool, int(payloadSize))
	for spanIndex, span := range spans {
		placeErr := placeDSEBytes(payload, occupied, uint32(span.offset), span.data)
		if placeErr != nil {
			return nil, fmt.Errorf("layoutPlace[%d]: %w", spanIndex, placeErr)
		}
	}
	for ordinal, section := range sections {
		placeErr := placeDSEBytes(payload, occupied, section.Offset, section.Data)
		if placeErr != nil {
			return nil, fmt.Errorf("sectionPlace[%d]: %w", ordinal, placeErr)
		}
	}
	for offset, isOccupied := range occupied {
		if !isOccupied {
			return nil, fmt.Errorf("unaccountedByte: %d", offset)
		}
	}
	copy(payload[:len(magic)], magic)
	binary.LittleEndian.PutUint32(payload[0x1c:0x20], documentType)
	binary.LittleEndian.PutUint32(payload[0x24:0x28], uint32(sectionCount))
	binary.LittleEndian.PutUint32(payload[0x30:0x34], sectionOffset)
	binary.LittleEndian.PutUint32(payload[0x44:0x48], bufferOffset)
	if uint64(sectionOffset)+uint64(sectionCount)*24 > uint64(len(payload)) {
		return nil, errors.New("sectionTable: exceeds payload")
	}
	for ordinal, section := range sections {
		offset := int(sectionOffset) + ordinal*24
		storedOffset := section.Offset
		if section.TypeCode == TypeBaseResource {
			if section.Offset < bufferOffset {
				return nil, fmt.Errorf("baseOffset[%d]: %d precedes buffer %d", ordinal, section.Offset, bufferOffset)
			}
			storedOffset -= bufferOffset
		}
		fields := []uint32{storedOffset, section.Field04, section.Size, section.Alignment, section.TypeCodeIndex, section.TypeCode}
		for fieldIndex, field := range fields {
			binary.LittleEndian.PutUint32(payload[offset+fieldIndex*4:offset+fieldIndex*4+4], field)
		}
	}
	rebuiltDocument, err := Decode(payload)
	if err != nil {
		return nil, fmt.Errorf("rebuiltDecode: %w", err)
	}
	err = rebuiltDocument.validateDSERenderAssets(renderAssets)
	if err != nil {
		return nil, fmt.Errorf("renderAssetValidate: %w", err)
	}
	return payload, nil
}

func readDSEFields(parser *rw4Parser, section Section, kind string) ([]byte, error) {
	if kind == "OPAQUE" {
		return nil, nil
	}
	switch section.TypeCode {
	case TypeBaseResource:
		return readDSEBuffer(parser, kind)
	case TypeVertexDescription:
		return readDSEVertexDescription(parser)
	case TypeVertexBuffer:
		return readDSEVertexBuffer(parser)
	case TypeIndexBuffer:
		return readDSEIndexBuffer(parser)
	case TypeMesh:
		return readDSEMesh(parser)
	case TypeRaster:
		return readDSERaster(parser)
	case TypeMeshStateLink:
		return readDSEMeshStateLink(parser)
	case TypeKeyframeAnimation:
		return readDSEKeyframeAnimation(parser, section)
	case TypeSkeleton:
		return readDSESkeleton(parser, section)
	case TypeAnimationSkin:
		return readDSEAnimationSkin(parser, section)
	case TypeSkinMatrixBuffer:
		return readDSESkinMatrixBuffer(parser, section)
	case TypeSkinsInK:
		return readDSESkinsInK(parser)
	case TypeBoundingBox:
		return readDSEBoundingBox(parser)
	case TypeMorphHandle:
		return readDSEMorphHandle(parser)
	case TypeAnimations:
		return readDSEAnimations(parser)
	default:
		return nil, nil
	}
}

func readDSEVertexDescription(parser *rw4Parser) ([]byte, error) {
	field00, err := parser.uint32("FIELD00")
	field04, err := parser.nextUint32("FIELD04", err)
	declaration, err := parser.nextUint32("DECLARATION", err)
	field0E, err := parser.nextUint32("FIELD0E", err)
	vertexSize, err := parser.nextUint32("VERTEXSIZE", err)
	elementFlags, err := parser.nextUint32("ELEMENTFLAGS", err)
	field14, err := parser.nextUint32("FIELD14", err)
	elementCount, err := parser.nextCount("NUMELEMENTS", err)
	if err != nil || field0E > 0xFF || vertexSize > 0xFF {
		return nil, fmt.Errorf("header: %v", err)
	}
	payload := make([]byte, 24+elementCount*12)
	binary.LittleEndian.PutUint32(payload[0:4], field00)
	binary.LittleEndian.PutUint32(payload[4:8], field04)
	binary.LittleEndian.PutUint32(payload[8:12], declaration)
	binary.LittleEndian.PutUint16(payload[12:14], uint16(elementCount))
	payload[14] = uint8(field0E)
	payload[15] = uint8(vertexSize)
	binary.LittleEndian.PutUint32(payload[16:20], elementFlags)
	binary.LittleEndian.PutUint32(payload[20:24], field14)
	for elementIndex := 0; elementIndex < elementCount; elementIndex++ {
		element, readErr := parser.property("ELEMENT", 1)
		if readErr != nil || element[0] != fmt.Sprintf("element%d", elementIndex) {
			return nil, fmt.Errorf("element[%d]: %v: %v", elementIndex, element, readErr)
		}
		stream, readErr := parser.uint32("STREAM")
		offset, readErr := parser.nextUint32("OFFSET", readErr)
		dataType, readErr := parser.nextSymbol("DATATYPE", readErr, map[string]uint32{"FLOAT2": dataFloat2, "FLOAT3": dataFloat3, "UBYTE4": dataUByte4, "UBYTE4N": dataUByte4N})
		method, readErr := parser.nextUint32("METHOD", readErr)
		usage, readErr := parser.nextSymbol("USAGE", readErr, map[string]uint32{"POSITION": usagePosition, "NORMAL": usageNormal, "TEXCOORD": usageTexCoord0})
		usageIndex, readErr := parser.nextUint32("USAGEINDEX", readErr)
		typeCode, readErr := parser.nextUint32("TYPECODE", readErr)
		if readErr != nil || stream > 0xFFFF || offset > 0xFFFF || dataType > 0xFF || method > 0xFF || usage > 0xFF || usageIndex > 0xFF {
			return nil, fmt.Errorf("element[%d]Fields: %v", elementIndex, readErr)
		}
		base := 24 + elementIndex*12
		binary.LittleEndian.PutUint16(payload[base:base+2], uint16(stream))
		binary.LittleEndian.PutUint16(payload[base+2:base+4], uint16(offset))
		payload[base+4], payload[base+5], payload[base+6], payload[base+7] = uint8(dataType), uint8(method), uint8(usage), uint8(usageIndex)
		binary.LittleEndian.PutUint32(payload[base+8:base+12], typeCode)
	}
	return payload, nil
}

func readDSEVertexBuffer(parser *rw4Parser) ([]byte, error) {
	description, err := parser.reference("DESCRIPTION")
	field04, err := parser.nextUint32("FIELD04", err)
	baseVertex, err := parser.nextUint32("BASEVERTEX", err)
	vertexCount, err := parser.nextUint32("NUMVERTICES", err)
	field10, err := parser.nextUint32("FIELD10", err)
	vertexSize, err := parser.nextUint32("VERTEXSIZE", err)
	dataRef, err := parser.nextReference("DATA?", err)
	if err != nil {
		return nil, fmt.Errorf("fields: %w", err)
	}
	return uint32Payload(description, field04, baseVertex, vertexCount, field10, vertexSize, dataRef), nil
}

func readDSEIndexBuffer(parser *rw4Parser) ([]byte, error) {
	declaration, err := parser.uint32("DECLARATION")
	startIndex, err := parser.nextUint32("STARTINDEX", err)
	primitiveCount, err := parser.nextUint32("NUMPRIMITIVES", err)
	usage, err := parser.nextUint32("USAGE", err)
	format, err := parser.nextSymbol("FORMAT", err, map[string]uint32{"UINT16": formatIndex16, "UINT32": formatIndex32})
	primitiveType, err := parser.nextSymbol("PRIMITIVETYPE", err, map[string]uint32{"TRIANGLELIST": PrimitiveTriangleList})
	dataRef, err := parser.nextReference("DATA?", err)
	if err != nil {
		return nil, fmt.Errorf("fields: %w", err)
	}
	return uint32Payload(declaration, startIndex, primitiveCount, usage, format, primitiveType, dataRef), nil
}

func readDSEMesh(parser *rw4Parser) ([]byte, error) {
	field00, err := parser.uint32("FIELD00")
	primitiveType, err := parser.nextSymbol("PRIMITIVETYPE", err, map[string]uint32{"TRIANGLELIST": PrimitiveTriangleList})
	indexRef, err := parser.nextReference("INDEXBUFFER", err)
	triangleCount, err := parser.nextUint32("NUMTRIANGLES", err)
	bufferCount, err := parser.nextCount("NUMVERTEXBUFFERS", err)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	bufferRefs := make([]uint32, bufferCount)
	for bufferIndex := range bufferRefs {
		bufferRefs[bufferIndex], err = parser.reference("VERTEXBUFFER")
		if err != nil {
			return nil, fmt.Errorf("vertexBuffer[%d]: %w", bufferIndex, err)
		}
	}
	firstIndex, err := parser.uint32("FIRSTINDEX")
	primitiveCount, err := parser.nextUint32("NUMPRIMITIVES", err)
	firstVertex, err := parser.nextUint32("FIRSTVERTEX", err)
	vertexCount, err := parser.nextUint32("NUMVERTICES", err)
	if err != nil {
		return nil, fmt.Errorf("range: %w", err)
	}
	payload := uint32Payload(field00, primitiveType, indexRef, triangleCount, uint32(bufferCount), firstIndex, primitiveCount, firstVertex, vertexCount)
	for _, reference := range bufferRefs {
		item := make([]byte, 4)
		binary.LittleEndian.PutUint32(item, reference)
		payload = append(payload, item...)
	}
	return payload, nil
}

func readDSERaster(parser *rw4Parser) ([]byte, error) {
	format, err := parser.symbol("FORMAT", map[string]uint32{"R8G8B8": formatR8G8B8, "A8R8G8B8": formatA8R8G8B8, "A8": formatA8, "DXT1": formatDXT1, "DXT3": formatDXT3, "DXT5": formatDXT5})
	flags, err := parser.nextUint32("FLAGS", err)
	volumeDepth, err := parser.nextUint32("VOLUMEDEPTH", err)
	dxTexture, err := parser.nextUint32("DXTEXTURE", err)
	width, err := parser.nextUint32("WIDTH", err)
	height, err := parser.nextUint32("HEIGHT", err)
	field10, err := parser.nextUint32("FIELD10", err)
	mipmapCount, err := parser.nextUint32("NUMMIPMAPS", err)
	field12, err := parser.nextUint32("FIELD12", err)
	field14, err := parser.nextUint32("FIELD14", err)
	field18, err := parser.nextUint32("FIELD18", err)
	dataRef, err := parser.nextReference("DATA?", err)
	if err != nil || flags > 0xFFFF || volumeDepth > 0xFFFF || width > 0xFFFF || height > 0xFFFF || field10 > 0xFF || mipmapCount > 0xFF || field12 > 0xFFFF {
		return nil, fmt.Errorf("fields: %v", err)
	}
	payload := make([]byte, 32)
	binary.LittleEndian.PutUint32(payload[0:4], format)
	binary.LittleEndian.PutUint16(payload[4:6], uint16(flags))
	binary.LittleEndian.PutUint16(payload[6:8], uint16(volumeDepth))
	binary.LittleEndian.PutUint32(payload[8:12], dxTexture)
	binary.LittleEndian.PutUint16(payload[12:14], uint16(width))
	binary.LittleEndian.PutUint16(payload[14:16], uint16(height))
	payload[16], payload[17] = uint8(field10), uint8(mipmapCount)
	binary.LittleEndian.PutUint16(payload[18:20], uint16(field12))
	binary.LittleEndian.PutUint32(payload[20:24], field14)
	binary.LittleEndian.PutUint32(payload[24:28], field18)
	binary.LittleEndian.PutUint32(payload[28:32], dataRef)
	return payload, nil
}

func readDSEMeshStateLink(parser *rw4Parser) ([]byte, error) {
	meshRef, err := parser.reference("MESH")
	stateCount, err := parser.nextCount("NUMSTATES", err)
	if err != nil {
		return nil, fmt.Errorf("fields: %w", err)
	}
	fields := make([]uint32, 0, stateCount+2)
	fields = append(fields, meshRef, uint32(stateCount))
	for stateIndex := 0; stateIndex < stateCount; stateIndex++ {
		stateRef, readErr := parser.reference("STATE")
		if readErr != nil {
			return nil, fmt.Errorf("state[%d]: %w", stateIndex, readErr)
		}
		fields = append(fields, stateRef)
	}
	return uint32Payload(fields...), nil
}

func uint32Payload(fields ...uint32) []byte {
	payload := make([]byte, len(fields)*4)
	for fieldIndex, field := range fields {
		binary.LittleEndian.PutUint32(payload[fieldIndex*4:fieldIndex*4+4], field)
	}
	return payload
}

func placeDSEBytes(payload []byte, occupied []bool, offset uint32, data []byte) error {
	end := uint64(offset) + uint64(len(data))
	if end > uint64(len(payload)) {
		return fmt.Errorf("range: %d:%d exceeds %d", offset, end, len(payload))
	}
	for index := int(offset); index < int(end); index++ {
		if occupied[index] {
			return fmt.Errorf("overlap: byte %d", index)
		}
		occupied[index] = true
	}
	copy(payload[int(offset):int(end)], data)
	return nil
}

type rw4Parser struct {
	scanner *bufio.Scanner
	line    int
}

func newRW4Parser(r io.Reader) *rw4Parser {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	return &rw4Parser{scanner: scanner}
}

func (e *rw4Parser) next() ([]string, error) {
	for e.scanner.Scan() {
		e.line++
		fields, err := tokenizeDSE(e.scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line[%d]: %w", e.line, err)
		}
		if len(fields) != 0 {
			return fields, nil
		}
	}
	err := e.scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("lineRead: %w", err)
	}
	return nil, io.EOF
}

func (e *rw4Parser) property(name string, count int) ([]string, error) {
	fields, err := e.next()
	if err != nil {
		return nil, fmt.Errorf("%sRead: %w", name, err)
	}
	if len(fields) != count+1 || fields[0] != name {
		return nil, fmt.Errorf("line[%d]: expected %s with %d arguments, got %v", e.line, name, count, fields)
	}
	return fields[1:], nil
}

func (e *rw4Parser) nextProperty(name string, previousErr error) ([]string, error) {
	if previousErr != nil {
		return nil, previousErr
	}
	return e.property(name, 1)
}

func (e *rw4Parser) uint32(name string) (uint32, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, fmt.Errorf("property: %w", err)
	}
	number, err := strconv.ParseUint(fields[0], 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return uint32(number), nil
}

func (e *rw4Parser) nextUint32(name string, previousErr error) (uint32, error) {
	if previousErr != nil {
		return 0, previousErr
	}
	return e.uint32(name)
}

func (e *rw4Parser) symbol(name string, symbols map[string]uint32) (uint32, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, fmt.Errorf("property: %w", err)
	}
	number, isFound := symbols[fields[0]]
	if isFound {
		return number, nil
	}
	parsed, err := strconv.ParseUint(fields[0], 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return uint32(parsed), nil
}

func (e *rw4Parser) nextSymbol(name string, previousErr error, symbols map[string]uint32) (uint32, error) {
	if previousErr != nil {
		return 0, previousErr
	}
	return e.symbol(name, symbols)
}

func (e *rw4Parser) count(name string) (int, error) {
	number, err := e.uint32(name)
	if err != nil {
		return 0, fmt.Errorf("number: %w", err)
	}
	if uint64(number) > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%s: %d exceeds int", name, number)
	}
	return int(number), nil
}

func (e *rw4Parser) nextCount(name string, previousErr error) (int, error) {
	if previousErr != nil {
		return 0, previousErr
	}
	return e.count(name)
}

func (e *rw4Parser) reference(name string) (uint32, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, fmt.Errorf("property: %w", err)
	}
	if fields[0] == "NULL" {
		return 0x00400000, nil
	}
	if strings.HasPrefix(fields[0], "section") {
		ordinal, parseErr := strconv.ParseUint(strings.TrimPrefix(fields[0], "section"), 10, 22)
		if parseErr != nil {
			return 0, fmt.Errorf("%sSection: %w", name, parseErr)
		}
		return uint32(ordinal), nil
	}
	number, err := strconv.ParseUint(fields[0], 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return uint32(number), nil
}

func (e *rw4Parser) nextReference(name string, previousErr error) (uint32, error) {
	if previousErr != nil {
		return 0, previousErr
	}
	return e.reference(name)
}

func (e *rw4Parser) hexBlock() ([]byte, error) {
	lineCount, err := e.count("NUMLINES")
	if err != nil {
		return nil, fmt.Errorf("lineCount: %w", err)
	}
	payload := make([]byte, 0, lineCount*dseLineSize)
	for lineIndex := 0; lineIndex < lineCount; lineIndex++ {
		fields, readErr := e.property("HEX", 1)
		if readErr != nil {
			return nil, fmt.Errorf("hex[%d]: %w", lineIndex, readErr)
		}
		line, decodeErr := hex.DecodeString(fields[0])
		if decodeErr != nil {
			return nil, fmt.Errorf("hexDecode[%d]: %w", lineIndex, decodeErr)
		}
		if len(line) == 0 || len(line) > dseLineSize {
			return nil, fmt.Errorf("hexSize[%d]: got %d, want 1-%d", lineIndex, len(line), dseLineSize)
		}
		if lineIndex != lineCount-1 && len(line) != dseLineSize {
			return nil, fmt.Errorf("hexSize[%d]: got %d, want %d", lineIndex, len(line), dseLineSize)
		}
		payload = append(payload, line...)
	}
	return payload, nil
}

func tokenizeDSE(line string) ([]string, error) {
	fields := make([]string, 0, 4)
	for offset := 0; offset < len(line); {
		for offset < len(line) && unicode.IsSpace(rune(line[offset])) {
			offset++
		}
		if offset >= len(line) || strings.HasPrefix(line[offset:], "//") {
			break
		}
		if line[offset] == '"' {
			offset++
			start := offset
			for offset < len(line) && line[offset] != '"' {
				offset++
			}
			if offset >= len(line) {
				return nil, errors.New("unterminated quote")
			}
			fields = append(fields, line[start:offset])
			offset++
			continue
		}
		start := offset
		for offset < len(line) && !unicode.IsSpace(rune(line[offset])) && !strings.HasPrefix(line[offset:], "//") {
			offset++
		}
		if start != offset {
			fields = append(fields, line[start:offset])
		}
	}
	return fields, nil
}

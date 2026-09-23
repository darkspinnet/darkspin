package ds

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/darkspinnet/darkspin/content/audio"
)

type patchRecord struct {
	name     string
	contents []byte
}

const pureDataPatchType = "pd"

type patchEnvelope struct {
	version uint32
	field08 uint32
	field16 uint32
	field20 uint32
	patches []patchRecord
}

func writePatchResource(destinationPath, identity string, ordinal int, resourceType uint32, payload []byte) error {
	declaration := "PD"
	envelope := patchEnvelope{
		patches: []patchRecord{{name: identity, contents: append([]byte(nil), payload...)}},
	}
	if resourceType == audio.PDRResourceType {
		declaration = "PDR"
		var err error
		envelope, err = decodePatchEnvelope(payload)
		if err != nil {
			return writeRawPatchResource(destinationPath, identity, ordinal, declaration, payload)
		}
	}
	for _, patch := range envelope.patches {
		err := validatePatchRecord(patch)
		if err != nil {
			return writeRawPatchResource(destinationPath, identity, ordinal, declaration, payload)
		}
	}

	var definition strings.Builder
	_, err := fmt.Fprintf(&definition, "%s %q\n\tVERSION %d\n", declaration, identity, version)
	if err == nil && resourceType == audio.PDRResourceType {
		_, err = fmt.Fprintf(&definition, "\tEAPDVERSION %d\n\tFIELD08 %d\n\tFIELD16 %d\n\tFIELD20 %d\n", envelope.version, envelope.field08, envelope.field16, envelope.field20)
	}
	if err == nil {
		_, err = fmt.Fprintf(&definition, "\tNUMPATCHES %d\n", len(envelope.patches))
	}
	for patchIndex, patch := range envelope.patches {
		if err != nil {
			break
		}
		_, err = fmt.Fprintf(&definition, "\t\tPATCH %q // %d\n", patch.name, patchIndex)
		if err != nil {
			break
		}
		lines, lineEnding, isTrailingNewline, lineErr := splitPatchLines(patch.contents)
		if lineErr != nil {
			err = fmt.Errorf("patchLines[%d]: %w", patchIndex, lineErr)
			break
		}
		_, err = fmt.Fprintf(&definition, "\t\t\tLINEENDING %q\n\t\t\tISTRAILINGNEWLINE %d\n\t\t\tNUMLINES %d\n", lineEnding, boolNumber(isTrailingNewline), len(lines))
		for _, line := range lines {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(&definition, "\t\t\t\tLINE %s\n", strconv.Quote(line))
		}
	}
	if err != nil {
		return fmt.Errorf("definitionRender: %w", err)
	}
	header := fmt.Sprintf("// darkspin resource dse v%d\n", version)
	existingPayload, err := os.ReadFile(destinationPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("definitionRead: %w", err)
	}
	body := ""
	if err == nil {
		existing := string(existingPayload)
		if !strings.HasPrefix(existing, header) {
			return errors.New("definitionHeader: invalid")
		}
		body = strings.TrimPrefix(existing, header)
		if strings.HasPrefix(body, declaration+" ") || strings.Contains(body, "\n"+declaration+" ") {
			return fmt.Errorf("definitionDuplicate: %s", declaration)
		}
	}
	contents := header + definition.String()
	if body != "" {
		contents = header + body + "\n" + definition.String()
		if resourceType == audio.PDResourceType {
			contents = header + definition.String() + "\n" + body
		}
	}
	err = os.WriteFile(destinationPath, []byte(contents), 0o644)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func writeRawPatchResource(destinationPath, identity string, ordinal int, declaration string, payload []byte) error {
	extension := "." + strings.ToLower(declaration)
	rawName := identity + extension
	rawPath := filepath.Join(filepath.Dir(destinationPath), rawName)
	err := os.WriteFile(rawPath, payload, 0o644)
	if err != nil {
		return fmt.Errorf("rawWrite: %w", err)
	}
	definition := fmt.Sprintf("%s%s %q\n\tVERSION %d\n\tORDINAL %d\n\tRAW %q\n", resourceDSEHeader, declaration, identity, version, ordinal, rawName)
	err = writeRenderDefinition(destinationPath, declaration, []byte(definition))
	if err != nil {
		return fmt.Errorf("definitionMerge: %w", err)
	}
	return nil
}

func readPatchResource(sourcePath, identity string, ordinal int, resourceType uint32) ([]byte, error) {
	_ = ordinal
	expectedDeclaration := "PD"
	if resourceType == audio.PDRResourceType {
		expectedDeclaration = "PDR"
	} else if resourceType != audio.PDResourceType {
		return nil, fmt.Errorf("resourceType: 0x%08X", resourceType)
	}
	patchDefinitions, err := readResourceDefinitions(sourcePath, map[string]bool{"PD": true, "PDR": true, "DARKSPINPD": true, "DARKSPINPDR": true})
	if err != nil {
		return nil, fmt.Errorf("definitionFilter: %w", err)
	}
	parser := newParser(bytes.NewReader(patchDefinitions))
	lastOrder := 0
	isFound := false
	var payload []byte
	for {
		definition, readErr := parser.next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("definitionRead: %w", readErr)
		}
		definitionOrder := 0
		switch definition[0] {
		case "PD", "DARKSPINPD":
			definitionOrder = 1
		case "PDR", "DARKSPINPDR":
			definitionOrder = 2
		default:
			return nil, fmt.Errorf("definitionUnsupported: %q", definition[0])
		}
		if definitionOrder <= lastOrder {
			return nil, fmt.Errorf("definitionOrder: %q", definition[0])
		}
		lastOrder = definitionOrder
		definitionPayload, parseErr := readPatchDefinition(parser, definition, sourcePath, identity)
		if parseErr != nil {
			return nil, parseErr
		}
		isPDDefinition := definition[0] == "PD" || definition[0] == "DARKSPINPD"
		isExpectedDefinition := resourceType == audio.PDResourceType && isPDDefinition ||
			resourceType == audio.PDRResourceType && !isPDDefinition
		if isExpectedDefinition {
			if isFound {
				return nil, fmt.Errorf("definitionDuplicate: %q", expectedDeclaration)
			}
			isFound = true
			payload = definitionPayload
		}
	}
	if !isFound {
		return nil, fmt.Errorf("definitionMissing: %q", expectedDeclaration)
	}
	return payload, nil
}

func readPatchDefinition(parser *parser, definition []string, sourcePath, identity string) ([]byte, error) {
	if len(definition) != 2 || definition[1] != identity {
		return nil, fmt.Errorf("definition: got %v, want Pure Data declaration and %q", definition, identity)
	}
	format := ""
	switch definition[0] {
	case "PD", "DARKSPINPD":
		format = "PD"
	case "PDR", "DARKSPINPDR":
		format = "PDR"
	default:
		return nil, fmt.Errorf("definitionUnsupported: %q", definition[0])
	}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return nil, fmt.Errorf("versionRead: %w", err)
	}
	parsedVersion, err := parseUint(versionFields[0], 32)
	if err != nil {
		return nil, fmt.Errorf("versionParse: %w", err)
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("versionUnsupported: %q", versionFields[0])
	}
	_, _, err = parser.optionalProperty("ORDINAL", 1)
	if err != nil {
		return nil, fmt.Errorf("ordinalRead: %w", err)
	}
	rawFields, isRaw, err := parser.optionalProperty("RAW", 1)
	if err != nil {
		return nil, fmt.Errorf("rawRead: %w", err)
	}
	if isRaw {
		dsRoot, rootErr := audioDSRoot(sourcePath)
		if rootErr != nil {
			return nil, fmt.Errorf("dsRoot: %w", rootErr)
		}
		rawPath, pathErr := safeAudioPath(dsRoot, filepath.Dir(sourcePath), rawFields[0])
		if pathErr != nil {
			return nil, fmt.Errorf("rawPath: %w", pathErr)
		}
		payload, readErr := os.ReadFile(rawPath)
		if readErr != nil {
			return nil, fmt.Errorf("rawOpen: %w", readErr)
		}
		return payload, nil
	}
	envelope := patchEnvelope{}
	if format == "PDR" {
		envelope.version, err = parser.uint32Property("EAPDVERSION")
		if err != nil {
			return nil, fmt.Errorf("eapdVersion: %w", err)
		}
		envelope.field08, err = parser.uint32Property("FIELD08")
		if err != nil {
			return nil, fmt.Errorf("field08: %w", err)
		}
		envelope.field16, err = parser.uint32Property("FIELD16")
		if err != nil {
			return nil, fmt.Errorf("field16: %w", err)
		}
		envelope.field20, err = parser.uint32Property("FIELD20")
		if err != nil {
			return nil, fmt.Errorf("field20: %w", err)
		}
	}
	patchCount, err := parser.uint32Property("NUMPATCHES")
	if err != nil {
		return nil, fmt.Errorf("patchCount: %w", err)
	}
	if patchCount == 0 {
		return nil, errors.New("patchCount: zero")
	}
	envelope.patches = make([]patchRecord, 0, patchCount)
	for patchIndex := uint32(0); patchIndex < patchCount; patchIndex++ {
		patchFields, readErr := parser.property("PATCH", 1)
		if readErr != nil {
			return nil, fmt.Errorf("patchRead[%d]: %w", patchIndex, readErr)
		}
		lineEnding := "CRLF"
		lineEndingFields, isLineEndingPresent, readErr := parser.optionalProperty("LINEENDING", 1)
		if readErr != nil {
			return nil, fmt.Errorf("lineEnding[%d]: %w", patchIndex, readErr)
		}
		if isLineEndingPresent {
			lineEnding = lineEndingFields[0]
		}
		separator := ""
		switch lineEnding {
		case "NONE":
		case "LF":
			separator = "\n"
		case "CRLF":
			separator = "\r\n"
		case "CR":
			separator = "\r"
		default:
			return nil, fmt.Errorf("lineEnding[%d]: unsupported %q", patchIndex, lineEnding)
		}
		isTrailingNewline := true
		trailingFields, isTrailingPresent, readErr := parser.optionalProperty("ISTRAILINGNEWLINE", 1)
		if readErr != nil {
			return nil, fmt.Errorf("trailingNewline[%d]: %w", patchIndex, readErr)
		}
		if isTrailingPresent {
			switch trailingFields[0] {
			case "0":
				isTrailingNewline = false
			case "1":
			default:
				return nil, fmt.Errorf("trailingNewline[%d]: expected 0 or 1, got %q", patchIndex, trailingFields[0])
			}
		}
		lineCount, readErr := parser.uint32Property("NUMLINES")
		if readErr != nil {
			return nil, fmt.Errorf("lineCount[%d]: %w", patchIndex, readErr)
		}
		var contents bytes.Buffer
		for lineIndex := uint32(0); lineIndex < lineCount; lineIndex++ {
			lineFields, lineErr := parser.property("LINE", 1)
			if lineErr != nil {
				return nil, fmt.Errorf("lineRead[%d][%d]: %w", patchIndex, lineIndex, lineErr)
			}
			contents.WriteString(lineFields[0])
			if lineIndex+1 < lineCount || isTrailingNewline {
				contents.WriteString(separator)
			}
		}
		if lineEnding == "NONE" && (lineCount > 1 || isTrailingNewline) {
			return nil, fmt.Errorf("lineEnding[%d]: NONE requires at most one unterminated line", patchIndex)
		}
		patch := patchRecord{name: patchFields[0], contents: contents.Bytes()}
		readErr = validatePatchRecord(patch)
		if readErr != nil {
			return nil, fmt.Errorf("patchValidate[%d]: %w", patchIndex, readErr)
		}
		envelope.patches = append(envelope.patches, patch)
	}
	var payload []byte
	if format == "PD" {
		if len(envelope.patches) != 1 {
			return nil, fmt.Errorf("patchCount: PD requires 1, got %d", len(envelope.patches))
		}
		payload = append([]byte(nil), envelope.patches[0].contents...)
	} else {
		payload, err = encodePatchEnvelope(envelope)
		if err != nil {
			return nil, fmt.Errorf("envelopeEncode: %w", err)
		}
	}
	return payload, nil
}

func decodePatchEnvelope(payload []byte) (patchEnvelope, error) {
	if len(payload) < 24 || !bytes.Equal(payload[0:4], []byte("EAPD")) {
		return patchEnvelope{}, errors.New("headerInvalid")
	}
	if binary.LittleEndian.Uint32(payload[12:16]) != uint32(len(payload)) {
		return patchEnvelope{}, fmt.Errorf("totalSize: got %d, want %d", binary.LittleEndian.Uint32(payload[12:16]), len(payload))
	}
	envelope := patchEnvelope{
		version: binary.LittleEndian.Uint32(payload[4:8]),
		field08: binary.LittleEndian.Uint32(payload[8:12]),
		field16: binary.LittleEndian.Uint32(payload[16:20]),
		field20: binary.LittleEndian.Uint32(payload[20:24]),
	}
	for offset := 24; offset < len(payload); {
		if offset+4 > len(payload) {
			return patchEnvelope{}, fmt.Errorf("nameSizeOffset: %d", offset)
		}
		nameSize := int(binary.LittleEndian.Uint32(payload[offset : offset+4]))
		offset += 4
		if nameSize < 4 || nameSize%4 != 0 || offset+nameSize+8 > len(payload) {
			return patchEnvelope{}, fmt.Errorf("nameSize: %d at %d", nameSize, offset-4)
		}
		nameBytes := bytes.TrimRight(payload[offset:offset+nameSize], "\x00")
		offset += nameSize
		kindBytes := bytes.TrimRight(payload[offset:offset+4], "\x00")
		offset += 4
		if string(kindBytes) != pureDataPatchType {
			return patchEnvelope{}, fmt.Errorf("patchType[%d]: unsupported %q", len(envelope.patches), string(kindBytes))
		}
		patchSize := int(binary.LittleEndian.Uint32(payload[offset : offset+4]))
		offset += 4
		if patchSize < 0 || offset+patchSize > len(payload) {
			return patchEnvelope{}, fmt.Errorf("patchSize: %d at %d", patchSize, offset-4)
		}
		patch := patchRecord{
			name:     string(nameBytes),
			contents: append([]byte(nil), payload[offset:offset+patchSize]...),
		}
		err := validatePatchRecord(patch)
		if err != nil {
			return patchEnvelope{}, fmt.Errorf("patchValidate[%d]: %w", len(envelope.patches), err)
		}
		envelope.patches = append(envelope.patches, patch)
		offset += patchSize
	}
	if len(envelope.patches) == 0 {
		return patchEnvelope{}, errors.New("patchCount: zero")
	}
	return envelope, nil
}

func encodePatchEnvelope(envelope patchEnvelope) ([]byte, error) {
	totalSize := 24
	for patchIndex, patch := range envelope.patches {
		err := validatePatchRecord(patch)
		if err != nil {
			return nil, fmt.Errorf("patchValidate[%d]: %w", patchIndex, err)
		}
		nameSize := (len(patch.name) + 1 + 3) &^ 3
		totalSize += 4 + nameSize + 4 + 4 + len(patch.contents)
	}
	payload := make([]byte, totalSize)
	copy(payload[0:4], "EAPD")
	binary.LittleEndian.PutUint32(payload[4:8], envelope.version)
	binary.LittleEndian.PutUint32(payload[8:12], envelope.field08)
	binary.LittleEndian.PutUint32(payload[12:16], uint32(totalSize))
	binary.LittleEndian.PutUint32(payload[16:20], envelope.field16)
	binary.LittleEndian.PutUint32(payload[20:24], envelope.field20)
	offset := 24
	for _, patch := range envelope.patches {
		nameSize := (len(patch.name) + 1 + 3) &^ 3
		binary.LittleEndian.PutUint32(payload[offset:offset+4], uint32(nameSize))
		offset += 4
		copy(payload[offset:offset+nameSize], patch.name)
		offset += nameSize
		copy(payload[offset:offset+4], pureDataPatchType)
		offset += 4
		binary.LittleEndian.PutUint32(payload[offset:offset+4], uint32(len(patch.contents)))
		offset += 4
		copy(payload[offset:offset+len(patch.contents)], patch.contents)
		offset += len(patch.contents)
	}
	return payload, nil
}

func validatePatchRecord(patch patchRecord) error {
	if patch.name == "" || strings.IndexByte(patch.name, 0) >= 0 || !utf8.ValidString(patch.name) {
		return fmt.Errorf("nameInvalid: %q", patch.name)
	}
	if !utf8.Valid(patch.contents) || bytes.IndexByte(patch.contents, 0) >= 0 {
		return errors.New("textInvalid")
	}
	return nil
}

func splitPatchLines(contents []byte) ([]string, string, bool, error) {
	if len(contents) == 0 {
		return nil, "NONE", false, nil
	}
	lines := make([]string, 0, bytes.Count(contents, []byte{'\n'})+1)
	lineStart := 0
	lineEnding := ""
	for offset := 0; offset < len(contents); offset++ {
		ending := ""
		endingSize := 1
		switch contents[offset] {
		case '\n':
			ending = "LF"
		case '\r':
			ending = "CR"
			if offset+1 < len(contents) && contents[offset+1] == '\n' {
				ending = "CRLF"
				endingSize = 2
			}
		default:
			continue
		}
		if lineEnding != "" && lineEnding != ending {
			return nil, "", false, errors.New("lineEnding: mixed")
		}
		lineEnding = ending
		lines = append(lines, string(contents[lineStart:offset]))
		offset += endingSize - 1
		lineStart = offset + 1
	}
	if lineEnding == "" {
		return []string{string(contents)}, "NONE", false, nil
	}
	isTrailingNewline := lineStart == len(contents)
	if !isTrailingNewline {
		lines = append(lines, string(contents[lineStart:]))
	}
	return lines, lineEnding, isTrailingNewline, nil
}

package ds

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/animation"
	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/movie"
	"github.com/darkspinnet/darkspin/content/render/gmsh"
	"github.com/darkspinnet/darkspin/content/render/prop"
	"github.com/darkspinnet/darkspin/content/render/rw4"
	"github.com/darkspinnet/darkspin/content/scaleform"
)

const opaqueLineSize = 32

const resourceDSEHeader = "// darkspin resource dse v1\n"

type renderDefinitionBlock struct {
	declaration string
	start       int
	end         int
}

func resourceIdentity(ordinal int, entry dbpf.Entry) string {
	return strings.TrimSuffix(dbpf.ResourceName(ordinal, entry), ".bin")
}

func readResource(sourcePath, identity string, ordinal int, resourceType uint32) ([]byte, error) {
	return readResourceWithPropertyDeclaration(sourcePath, identity, ordinal, resourceType, "PROPERTYLIST")
}

func readResourceWithPropertyDeclaration(sourcePath, identity string, ordinal int, resourceType uint32, propertyDeclaration string) ([]byte, error) {
	if scaleform.IsResourceType(resourceType) {
		payload, err := readScaleformResource(sourcePath, identity, resourceType)
		if err != nil {
			return nil, fmt.Errorf("scaleformRead: %w", err)
		}
		return payload, nil
	}
	if resourceType == animation.ResourceType {
		definition, err := readRenderDefinition(sourcePath, "ANIMATION")
		if err != nil {
			return nil, fmt.Errorf("animationDefinition: %w", err)
		}
		payload, err := animation.ReadDSE(bytes.NewReader(definition), identity)
		if err != nil {
			return nil, fmt.Errorf("animationRead: %w", err)
		}
		return payload, nil
	}
	if resourceType == movie.ResourceType {
		payload, err := readMovieResource(sourcePath, identity)
		if err != nil {
			return nil, fmt.Errorf("movieRead: %w", err)
		}
		return payload, nil
	}
	if audio.IsStreamType(resourceType) {
		payload, err := readAudioResource(sourcePath, identity, ordinal, resourceType)
		if err != nil {
			return nil, fmt.Errorf("audioRead: %w", err)
		}
		return payload, nil
	}
	if audio.IsPatchType(resourceType) {
		payload, err := readPatchResource(sourcePath, identity, ordinal, resourceType)
		if err != nil {
			return nil, fmt.Errorf("patchRead: %w", err)
		}
		return payload, nil
	}
	if resourceType == rw4.ResourceType {
		definition, err := readCompatibleRenderDefinition(sourcePath, "RW4", "DARKSPINRW4")
		if err != nil {
			return nil, fmt.Errorf("rw4Definition: %w", err)
		}
		payload, err := rw4.ReadDSE(bytes.NewReader(definition), identity)
		if err != nil {
			return nil, fmt.Errorf("rw4Read: %w", err)
		}
		return payload, nil
	}
	if resourceType == gmsh.ResourceType {
		definition, err := readCompatibleRenderDefinition(sourcePath, "GMSH", "DARKSPINGMSH")
		if err != nil {
			return nil, fmt.Errorf("gmshDefinition: %w", err)
		}
		payload, err := gmsh.ReadDSE(bytes.NewReader(definition), identity)
		if err != nil {
			return nil, fmt.Errorf("gmshRead: %w", err)
		}
		return payload, nil
	}
	if prop.IsResourceType(resourceType) {
		hasProperty, err := hasRenderDefinition(sourcePath, propertyDeclaration)
		if err != nil {
			return nil, fmt.Errorf("propertyDefinition: %w", err)
		}
		selectedDeclaration := propertyDeclaration
		if !hasProperty {
			hasLegacy, legacyErr := hasRenderDefinition(sourcePath, "DARKSPINPROPERTYLIST")
			if legacyErr != nil {
				return nil, fmt.Errorf("propertyLegacy: %w", legacyErr)
			}
			if hasLegacy {
				selectedDeclaration = "DARKSPINPROPERTYLIST"
			} else {
				declaration, declarationErr := resourceDeclaration(sourcePath, identity)
				if declarationErr != nil {
					return nil, fmt.Errorf("propertyDeclaration: %w", declarationErr)
				}
				if declaration != "AUDIOBASE" && declaration != "DARKSPINAUDIOBASE" {
					return nil, fmt.Errorf("propertyDefinitionMissing: %s", propertyDeclaration)
				}
				document, readErr := readAudioBaseResource(sourcePath, identity)
				if readErr != nil {
					return nil, fmt.Errorf("audioBaseRead: %w", readErr)
				}
				payload, encodeErr := prop.Encode(document)
				if encodeErr != nil {
					return nil, fmt.Errorf("audioBaseEncode: %w", encodeErr)
				}
				return payload, nil
			}
		}
		document, readErr := readPropertyResource(sourcePath, identity, ordinal, selectedDeclaration)
		if readErr != nil {
			return nil, fmt.Errorf("propertyRead: %w", readErr)
		}
		payload, encodeErr := prop.Encode(document)
		if encodeErr != nil {
			return nil, fmt.Errorf("propertyEncode: %w", encodeErr)
		}
		return payload, nil
	}
	declaration, err := resourceDeclaration(sourcePath, identity)
	if err != nil {
		return nil, fmt.Errorf("declarationRead: %w", err)
	}
	switch declaration {
	case "OPAQUE", "DARKSPINOPAQUE":
		payload, readErr := readOpaqueResource(sourcePath, identity)
		if readErr != nil {
			return nil, fmt.Errorf("opaqueRead: %w", readErr)
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("declarationUnsupported: %q", declaration)
	}
}

func readCompatibleRenderDefinition(sourcePath, declaration, legacyDeclaration string) ([]byte, error) {
	hasDefinition, err := hasRenderDefinition(sourcePath, declaration)
	if err != nil {
		return nil, fmt.Errorf("definitionCheck: %w", err)
	}
	if hasDefinition {
		return readRenderDefinition(sourcePath, declaration)
	}
	hasLegacy, err := hasRenderDefinition(sourcePath, legacyDeclaration)
	if err != nil {
		return nil, fmt.Errorf("legacyCheck: %w", err)
	}
	if !hasLegacy {
		return nil, fmt.Errorf("definitionMissing: %s", declaration)
	}
	return readRenderDefinition(sourcePath, legacyDeclaration)
}

func hasRenderDefinition(sourcePath, declaration string) (bool, error) {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return false, fmt.Errorf("definitionRead: %w", err)
	}
	_, blocks, err := parseRenderDefinitions(payload)
	if err != nil {
		return false, fmt.Errorf("definitionParse: %w", err)
	}
	for _, block := range blocks {
		if block.declaration == declaration {
			return true, nil
		}
	}
	return false, nil
}

func resourceDeclaration(sourcePath, identity string) (string, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("definitionOpen: %w", err)
	}
	defer r.Close()
	parser := newParser(r)
	definition, err := parser.next()
	if err != nil {
		return "", fmt.Errorf("definitionRead: %w", err)
	}
	if len(definition) != 2 || definition[1] != identity {
		return "", fmt.Errorf("definition: got %v, want declaration and %q", definition, identity)
	}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return "", err
	}
	parsedVersion, err := parseUint(versionFields[0], 32)
	if err != nil {
		return "", fmt.Errorf("versionParse: %w", err)
	}
	if parsedVersion != version {
		return "", fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	return definition[0], nil
}

func writeRenderDefinition(destinationPath, declaration string, rendered []byte) error {
	body, blocks, err := parseRenderDefinitions(rendered)
	if err != nil {
		return fmt.Errorf("definitionParse: %w", err)
	}
	if len(blocks) != 1 || blocks[0].declaration != declaration {
		return fmt.Errorf("definitionRendered: got %v, want %s", blocks, declaration)
	}
	definition := strings.TrimRight(body, "\r\n")
	existingPayload, err := os.ReadFile(destinationPath)
	if errors.Is(err, os.ErrNotExist) {
		err = os.WriteFile(destinationPath, []byte(resourceDSEHeader+definition+"\n"), 0o644)
		if err != nil {
			return fmt.Errorf("definitionWrite: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("definitionRead: %w", err)
	}
	existingBody, existingBlocks, err := parseRenderDefinitions(existingPayload)
	if err != nil {
		return fmt.Errorf("existingParse: %w", err)
	}
	for _, block := range existingBlocks {
		if block.declaration == declaration {
			return fmt.Errorf("definitionDuplicate: %s", declaration)
		}
	}
	definitions := make([]renderDefinitionBlock, 0, len(existingBlocks)+1)
	definitions = append(definitions, existingBlocks...)
	definitions = append(definitions, renderDefinitionBlock{declaration: declaration, start: 0, end: len(definition)})
	definitionTexts := make(map[string]string, len(definitions))
	for _, block := range existingBlocks {
		definitionTexts[block.declaration] = strings.TrimRight(existingBody[block.start:block.end], "\r\n")
	}
	definitionTexts[declaration] = definition
	sort.SliceStable(definitions, func(firstIndex, secondIndex int) bool {
		return resourceDefinitionOrder(definitions[firstIndex].declaration) < resourceDefinitionOrder(definitions[secondIndex].declaration)
	})
	orderedDefinitions := make([]string, 0, len(definitions))
	for _, block := range definitions {
		orderedDefinitions = append(orderedDefinitions, definitionTexts[block.declaration])
	}
	contents := resourceDSEHeader + strings.Join(orderedDefinitions, "\n\n") + "\n"
	err = os.WriteFile(destinationPath, []byte(contents), 0o644)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func readRenderDefinition(sourcePath, declaration string) ([]byte, error) {
	return readResourceDefinitions(sourcePath, map[string]bool{declaration: true})
}

func readOrdinalRenderDefinition(sourcePath string, declarations map[string]bool, identity string, ordinal int) ([]byte, error) {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	body, blocks, err := parseRenderDefinitions(payload)
	if err != nil {
		return nil, fmt.Errorf("definitionParse: %w", err)
	}
	selected := ""
	for _, block := range blocks {
		if !declarations[block.declaration] {
			continue
		}
		definitionText := strings.TrimRight(body[block.start:block.end], "\r\n")
		parser := newParser(strings.NewReader(definitionText))
		definition, readErr := parser.next()
		if readErr != nil {
			return nil, fmt.Errorf("definitionHeader: %w", readErr)
		}
		if len(definition) != 2 {
			return nil, fmt.Errorf("definitionHeader: got %v", definition)
		}
		_, readErr = parser.property("VERSION", 1)
		if readErr != nil {
			return nil, fmt.Errorf("definitionVersion: %w", readErr)
		}
		ordinalFields, isOrdinalPresent, readErr := parser.optionalProperty("ORDINAL", 1)
		if readErr != nil {
			return nil, fmt.Errorf("definitionOrdinal: %w", readErr)
		}
		isMatch := !isOrdinalPresent && definition[1] == identity
		if isOrdinalPresent {
			parsedOrdinal, parseErr := strconv.Atoi(ordinalFields[0])
			if parseErr != nil || parsedOrdinal < 0 {
				return nil, fmt.Errorf("definitionOrdinal: %q", ordinalFields[0])
			}
			isMatch = parsedOrdinal == ordinal
		}
		if !isMatch {
			continue
		}
		if selected != "" {
			return nil, fmt.Errorf("definitionDuplicate: ordinal %d", ordinal)
		}
		selected = definitionText
	}
	if selected == "" {
		return nil, fmt.Errorf("definitionMissing: ordinal %d", ordinal)
	}
	return []byte(resourceDSEHeader + selected + "\n"), nil
}

func readResourceDefinitions(sourcePath string, declarations map[string]bool) ([]byte, error) {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	body, blocks, err := parseRenderDefinitions(payload)
	if err != nil {
		return nil, fmt.Errorf("definitionParse: %w", err)
	}
	definitions := make([]string, 0, len(declarations))
	for _, block := range blocks {
		if !declarations[block.declaration] {
			continue
		}
		definitions = append(definitions, strings.TrimRight(body[block.start:block.end], "\r\n"))
	}
	if len(definitions) == 0 {
		return nil, fmt.Errorf("definitionMissing")
	}
	return []byte(resourceDSEHeader + strings.Join(definitions, "\n\n") + "\n"), nil
}

func parseRenderDefinitions(payload []byte) (string, []renderDefinitionBlock, error) {
	contents := string(payload)
	if !strings.HasPrefix(contents, resourceDSEHeader) {
		return "", nil, errors.New("headerInvalid")
	}
	body := strings.TrimPrefix(contents, resourceDSEHeader)
	blocks := make([]renderDefinitionBlock, 0, 2)
	for lineStart := 0; lineStart < len(body); {
		lineEnd := strings.IndexByte(body[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(body)
		} else {
			lineEnd += lineStart
		}
		line := strings.TrimSuffix(body[lineStart:lineEnd], "\r")
		declaration := ""
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "//") {
			if lineEnd == len(body) {
				break
			}
			lineStart = lineEnd + 1
			continue
		}
		if line != "" && line[0] != '\t' && line[0] != ' ' {
			separator := strings.IndexByte(line, ' ')
			if separator < 0 {
				return "", nil, fmt.Errorf("declarationUnsupported: %q", line)
			}
			declaration = line[:separator]
			if resourceDefinitionOrder(declaration) == 0 {
				return "", nil, fmt.Errorf("declarationUnsupported: %q", declaration)
			}
		}
		if declaration != "" {
			if len(blocks) > 0 {
				blocks[len(blocks)-1].end = lineStart
			}
			blocks = append(blocks, renderDefinitionBlock{declaration: declaration, start: lineStart, end: len(body)})
		}
		if lineEnd == len(body) {
			break
		}
		lineStart = lineEnd + 1
	}
	if len(blocks) == 0 {
		return "", nil, errors.New("definitionMissing")
	}
	lastOrder := 0
	for _, block := range blocks {
		order := resourceDefinitionOrder(block.declaration)
		if order < lastOrder {
			return "", nil, fmt.Errorf("definitionOrder: %s", block.declaration)
		}
		lastOrder = order
	}
	return body, blocks, nil
}

func resourceDefinitionOrder(declaration string) int {
	switch declaration {
	case "PROPERTYLIST", "AUDIOLIST", "PROPSLIST", "AUDIOBASE", "GMSH", "PD", "OPAQUE", "ANIMATION",
		"GFX", "GFXIMAGE", "DARKSPINPROPERTYLIST", "DARKSPINAUDIOBASE", "DARKSPINGMSH", "DARKSPINPD", "DARKSPINOPAQUE":
		return 10
	case "SNR", "PDR", "RW4", "DARKSPINSNR", "DARKSPINPDR", "DARKSPINRW4":
		return 20
	case "SNS", "DARKSPINSNS":
		return 30
	default:
		return 0
	}
}

func writeResource(destinationPath, identity string, ordinal int, entry dbpf.Entry, payload io.Reader, names map[uint32]string) error {
	return writeResourceWithPropertyDeclaration(destinationPath, identity, ordinal, entry, payload, names, "PROPERTYLIST")
}

func writeResourceWithPropertyDeclaration(destinationPath, identity string, ordinal int, entry dbpf.Entry, payload io.Reader, names map[uint32]string, propertyDeclaration string) error {
	if scaleform.IsResourceType(entry.Type) {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("scaleformRead: %w", err)
		}
		err = writeScaleformResource(destinationPath, identity, entry.Type, contents)
		if err != nil {
			return fmt.Errorf("scaleformWrite: %w", err)
		}
		return nil
	}
	if entry.Type == animation.ResourceType {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("animationRead: %w", err)
		}
		document, err := animation.Decode(contents)
		if err != nil {
			return fmt.Errorf("animationDecode: %w", err)
		}
		var definition bytes.Buffer
		err = animation.WriteDSE(&definition, identity, document)
		if err != nil {
			return fmt.Errorf("animationWrite: %w", err)
		}
		err = writeRenderDefinition(destinationPath, "ANIMATION", definition.Bytes())
		if err != nil {
			return fmt.Errorf("animationDefinition: %w", err)
		}
		return nil
	}
	if entry.Type == movie.ResourceType {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("movieRead: %w", err)
		}
		err = writeMovieResource(destinationPath, identity, contents)
		if err != nil {
			return fmt.Errorf("movieWrite: %w", err)
		}
		return nil
	}
	if audio.IsPatchType(entry.Type) {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("patchRead: %w", err)
		}
		err = writePatchResource(destinationPath, identity, ordinal, entry.Type, contents)
		if err != nil {
			return fmt.Errorf("patchWrite: %w", err)
		}
		return nil
	}
	if audio.IsStreamType(entry.Type) {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("audioRead: %w", err)
		}
		err = writeAudioResource(destinationPath, identity, ordinal, entry.Type, contents)
		if err != nil {
			return fmt.Errorf("audioWrite: %w", err)
		}
		return nil
	}
	if entry.Type == rw4.ResourceType {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("rw4Read: %w", err)
		}
		document, err := rw4.Decode(contents)
		if err != nil {
			return fmt.Errorf("rw4Decode: %w", err)
		}
		var definition bytes.Buffer
		writeErr := rw4.WriteDSE(&definition, identity, document)
		if writeErr != nil {
			return fmt.Errorf("rw4Write: %w", writeErr)
		}
		err = writeRenderDefinition(destinationPath, "RW4", definition.Bytes())
		if err != nil {
			return fmt.Errorf("rw4Definition: %w", err)
		}
		return nil
	}
	if entry.Type == gmsh.ResourceType {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("gmshRead: %w", err)
		}
		document, err := gmsh.Decode(contents)
		if err != nil {
			return fmt.Errorf("gmshDecode: %w", err)
		}
		var definition bytes.Buffer
		writeErr := gmsh.WriteDSE(&definition, identity, document)
		if writeErr != nil {
			return fmt.Errorf("gmshWrite: %w", writeErr)
		}
		err = writeRenderDefinition(destinationPath, "GMSH", definition.Bytes())
		if err != nil {
			return fmt.Errorf("gmshDefinition: %w", err)
		}
		return nil
	}
	if prop.IsResourceType(entry.Type) {
		contents, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("propertyRead: %w", err)
		}
		if len(contents) != int(entry.Size) {
			return fmt.Errorf("propertySize: got %d, want %d", len(contents), entry.Size)
		}
		document, err := prop.Decode(contents)
		if err != nil {
			return fmt.Errorf("propertyDecode: %w", err)
		}
		if isAudioBaseResource(entry) {
			err = writeAudioBaseResource(destinationPath, identity, document, names)
			if err != nil {
				return fmt.Errorf("audioBaseWrite: %w", err)
			}
			return nil
		}
		err = writePropertyResource(destinationPath, identity, ordinal, document, names, propertyDeclaration)
		if err != nil {
			return fmt.Errorf("propertyWrite: %w", err)
		}
		return nil
	}
	err := writeOpaqueResource(destinationPath, identity, payload, entry.StoredSize)
	if err != nil {
		return fmt.Errorf("opaqueWrite: %w", err)
	}
	return nil
}

func writeAudioResource(destinationPath, identity string, ordinal int, resourceType uint32, payload []byte) error {
	declaration := "SNR"
	if resourceType == audio.SNSResourceType {
		declaration = "SNS"
	}
	isRaw := false
	var definition strings.Builder
	_, err := fmt.Fprintf(&definition, "%s %q\n\tVERSION %d\n\tORDINAL %d\n", declaration, identity, version, ordinal)
	if err == nil && resourceType == audio.SNRResourceType {
		header, headerErr := audio.DecodeResolvedHeader(payload)
		if headerErr != nil {
			err = fmt.Errorf("headerDecode: %w", headerErr)
		} else {
			_, err = fmt.Fprintf(&definition, "\tAUDIOVERSION %d\n\tCODEC %q\n\tCHANNELS %d\n\tSAMPLERATE %d\n\tNUMSAMPLES %d\n\tISLOOPED %d\n\tSTORAGE %q\n", header.Version, header.Codec, header.Channels, header.SampleRate, header.SampleCount, boolNumber(header.IsLooped), header.Storage)
			isRaw = header.Codec == "NONE" || header.Codec == "RESERVED"
		}
	}
	if err == nil && isRaw {
		rawName := identity + ".snr"
		_, err = fmt.Fprintf(&definition, "\tRAW %q\n", rawName)
		if err == nil {
			rawPath := filepath.Join(filepath.Dir(destinationPath), rawName)
			err = os.WriteFile(rawPath, payload, 0o644)
		}
	} else if err == nil {
		_, err = fmt.Fprintf(&definition, "\tWAV %q\n", identity+".wav")
	}
	if err != nil {
		return fmt.Errorf("definitionRender: %w", err)
	}
	rendered := []byte(resourceDSEHeader + definition.String())
	err = writeRenderDefinition(destinationPath, declaration, rendered)
	if err != nil {
		return fmt.Errorf("definitionMerge: %w", err)
	}
	return nil
}

func readAudioResource(sourcePath, identity string, ordinal int, resourceType uint32) ([]byte, error) {
	expectedDeclaration := "SNR"
	if resourceType == audio.SNSResourceType {
		expectedDeclaration = "SNS"
	} else if resourceType != audio.SNRResourceType {
		return nil, fmt.Errorf("resourceType: 0x%08X", resourceType)
	}
	declarations := map[string]bool{expectedDeclaration: true, "DARKSPIN" + expectedDeclaration: true}
	audioDefinitions, err := readOrdinalRenderDefinition(sourcePath, declarations, identity, ordinal)
	if err != nil {
		return nil, fmt.Errorf("definitionSelect: %w", err)
	}
	parser := newParser(bytes.NewReader(audioDefinitions))
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	if len(definition) != 2 || !declarations[definition[0]] {
		return nil, fmt.Errorf("definition: got %v, want %q", definition, expectedDeclaration)
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
	header := audio.Header{}
	if resourceType == audio.SNRResourceType {
		header, err = readAudioHeader(parser)
		if err != nil {
			return nil, fmt.Errorf("headerRead: %w", err)
		}
	}
	rawFields, isRaw, err := parser.optionalProperty("RAW", 1)
	if err != nil {
		return nil, fmt.Errorf("rawRead: %w", err)
	}
	dsRoot, err := audioDSRoot(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("dsRoot: %w", err)
	}
	if isRaw {
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
	wavFields, err := parser.property("WAV", 1)
	if err != nil {
		return nil, fmt.Errorf("waveRead: %w", err)
	}
	wavPathField := wavFields[0]
	wavPath, err := safeAudioPath(dsRoot, filepath.Dir(sourcePath), wavPathField)
	if err != nil {
		return nil, fmt.Errorf("wavePath: %w", err)
	}
	wavPayload, err := os.ReadFile(wavPath)
	if err != nil {
		return nil, fmt.Errorf("waveOpen: %w", err)
	}
	wav, err := audio.DecodeWAV(wavPayload)
	if err != nil {
		return nil, fmt.Errorf("waveDecode: %w", err)
	}
	if resourceType == audio.SNSResourceType {
		payload, encodeErr := audio.EncodeSNSPCM(wav)
		if encodeErr != nil {
			return nil, fmt.Errorf("snsEncode: %w", encodeErr)
		}
		return payload, nil
	}
	storage := header.Storage
	if storage != "RAM" {
		storage = "STREAM"
	}
	payload, err := audio.EncodeSNRPCM(wav, storage, header.IsLooped)
	if err != nil {
		return nil, fmt.Errorf("snrEncode: %w", err)
	}
	return payload, nil
}

func readAudioHeader(parser *parser) (audio.Header, error) {
	audioVersion, err := parser.uint32Property("AUDIOVERSION")
	if err != nil {
		return audio.Header{}, fmt.Errorf("audioVersion: %w", err)
	}
	if audioVersion > 0x0F {
		return audio.Header{}, fmt.Errorf("audioVersionRange: %d", audioVersion)
	}
	codecFields, err := parser.property("CODEC", 1)
	if err != nil {
		return audio.Header{}, fmt.Errorf("codec: %w", err)
	}
	channels, err := parser.uint32Property("CHANNELS")
	if err != nil {
		return audio.Header{}, fmt.Errorf("channels: %w", err)
	}
	if channels == 0 || channels > 64 {
		return audio.Header{}, fmt.Errorf("channelRange: %d", channels)
	}
	sampleRate, err := parser.uint32Property("SAMPLERATE")
	if err != nil {
		return audio.Header{}, fmt.Errorf("sampleRate: %w", err)
	}
	sampleCount, err := parser.uint32Property("NUMSAMPLES")
	if err != nil {
		return audio.Header{}, fmt.Errorf("sampleCount: %w", err)
	}
	loopFields, err := parser.property("ISLOOPED", 1)
	if err != nil {
		return audio.Header{}, fmt.Errorf("looped: %w", err)
	}
	isLooped := false
	switch loopFields[0] {
	case "0":
	case "1":
		isLooped = true
	default:
		return audio.Header{}, fmt.Errorf("looped: %q", loopFields[0])
	}
	storageFields, err := parser.property("STORAGE", 1)
	if err != nil {
		return audio.Header{}, fmt.Errorf("storage: %w", err)
	}
	header := audio.Header{Version: uint8(audioVersion), Codec: codecFields[0], Channels: uint8(channels), SampleRate: sampleRate, SampleCount: sampleCount, IsLooped: isLooped, Storage: storageFields[0]}
	return header, nil
}

func boolNumber(isSet bool) int {
	if isSet {
		return 1
	}
	return 0
}

func writeOpaqueResource(destinationPath, identity string, payload io.Reader, payloadSize uint32) error {
	w, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("definitionCreate: %w", err)
	}
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.Remove(destinationPath)
		}
	}()
	writer := bufio.NewWriter(w)
	lineCount := (uint64(payloadSize) + opaqueLineSize - 1) / opaqueLineSize
	_, err = fmt.Fprintf(writer, "// darkspin resource dse v%d\n", version)
	if err == nil {
		_, err = fmt.Fprintf(writer, "OPAQUE %q\n", identity)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tVERSION %d\n", version)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMLINES %d\n", lineCount)
	}
	buffer := make([]byte, opaqueLineSize)
	remaining := uint64(payloadSize)
	for lineIndex := uint64(0); lineIndex < lineCount && err == nil; lineIndex++ {
		lineSize := uint64(opaqueLineSize)
		if remaining < lineSize {
			lineSize = remaining
		}
		_, err = io.ReadFull(payload, buffer[:lineSize])
		if err != nil {
			err = fmt.Errorf("hexRead[%d]: %w", lineIndex, err)
			break
		}
		_, err = fmt.Fprintf(writer, "\t\tHEX %s\n", strings.ToUpper(hex.EncodeToString(buffer[:lineSize])))
		remaining -= lineSize
	}
	if err == nil {
		extra := make([]byte, 1)
		count, readErr := payload.Read(extra)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			err = fmt.Errorf("trailingRead: %w", readErr)
		} else if count != 0 {
			err = errors.New("trailingData")
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("definitionOutput: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("definitionClose: %w", closeErr)
	}
	isComplete = true
	return nil
}

func readOpaqueResource(sourcePath, identity string) ([]byte, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("definitionOpen: %w", err)
	}
	defer r.Close()
	parser := newParser(r)
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	if len(definition) != 2 || definition[0] != "OPAQUE" && definition[0] != "DARKSPINOPAQUE" || definition[1] != identity {
		return nil, fmt.Errorf("definition: got %v, want OPAQUE %q", definition, identity)
	}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return nil, err
	}
	parsedVersion, err := parseUint(versionFields[0], 32)
	if err != nil {
		return nil, fmt.Errorf("versionParse: %w", err)
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	lineFields, err := parser.property("NUMLINES", 1)
	if err != nil {
		return nil, err
	}
	lineCount, err := strconv.Atoi(lineFields[0])
	if err != nil || lineCount < 0 {
		return nil, fmt.Errorf("lineCount: %q", lineFields[0])
	}
	payload := make([]byte, 0, lineCount*opaqueLineSize)
	for lineIndex := 0; lineIndex < lineCount; lineIndex++ {
		hexFields, readErr := parser.property("HEX", 1)
		if readErr != nil {
			return nil, fmt.Errorf("hex[%d]: %w", lineIndex, readErr)
		}
		line, decodeErr := hex.DecodeString(hexFields[0])
		if decodeErr != nil {
			return nil, fmt.Errorf("hexDecode[%d]: %w", lineIndex, decodeErr)
		}
		if len(line) == 0 || len(line) > opaqueLineSize {
			return nil, fmt.Errorf("hexSize[%d]: got %d, want 1-%d", lineIndex, len(line), opaqueLineSize)
		}
		if lineIndex != lineCount-1 && len(line) != opaqueLineSize {
			return nil, fmt.Errorf("hexSize[%d]: got %d, want %d", lineIndex, len(line), opaqueLineSize)
		}
		payload = append(payload, line...)
	}
	remaining, err := parser.next()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("definitionTrailing: %w", err)
	}
	if len(remaining) != 0 {
		return nil, fmt.Errorf("definitionTrailing: %v", remaining)
	}
	return payload, nil
}

func writePropertyResource(destinationPath, identity string, ordinal int, document *prop.Document, names map[uint32]string, declaration string) error {
	var definition bytes.Buffer
	writer := bufio.NewWriter(&definition)
	var err error
	_, err = fmt.Fprintf(writer, "// darkspin resource dse v%d\n", version)
	if err == nil {
		_, err = fmt.Fprintf(writer, "%s %q\n", declaration, identity)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tVERSION %d\n", version)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tORDINAL %d\n", ordinal)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMPROPERTIES %d\n", len(document.Properties))
	}
	primitiveIDs, primitiveErr := primitivePropertyIDs(document)
	if primitiveErr != nil && err == nil {
		err = primitiveErr
	}
	for ordinal, property := range document.Properties {
		if err != nil {
			break
		}
		name, isFound := prop.Name(property.ID)
		isPrimitive := !isFound && primitiveIDs[property.ID]
		isHashed := !isFound && !isPrimitive
		if isFound && property.Name != "" && property.Name != name {
			err = fmt.Errorf("propertyName[%d]: got %q, want %q", ordinal, property.Name, name)
			break
		}
		declaration := "PROPERTY"
		if property.IsArray {
			declaration = "ARRAYPROPERTY"
		}
		propertyKey := strconv.Quote(name)
		if isPrimitive {
			declaration = "PRIMITIVE"
			if property.IsArray {
				declaration = "ARRAYPRIMITIVE"
			}
			propertyKey = fmt.Sprintf("0x%08X", property.ID)
		}
		if isHashed {
			declaration = "HASHEDPROPERTY"
			if property.IsArray {
				declaration = "ARRAYHASHEDPROPERTY"
			}
			propertyKey = fmt.Sprintf("0x%08X", property.ID)
		}
		_, err = fmt.Fprintf(writer, "\t\t%s %s // %d\n", declaration, propertyKey, ordinal)
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tTYPE %q\n", propertyTypeName(property.Type))
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tFLAGS 0x%04X\n", property.Flags)
		}
		if err == nil && property.IsArray {
			_, err = fmt.Fprintf(writer, "\t\t\tITEMSIZE %d\n", property.ItemSize)
		}
		if err == nil && property.IsArray {
			_, err = fmt.Fprintf(writer, "\t\t\tNUMVALUES %d\n", len(property.Items))
		}
		for itemIndex, item := range property.Items {
			if err != nil {
				break
			}
			formattedItem, formatErr := formatPropertyItem(property.Type, property.IsArray, item, names)
			if formatErr != nil {
				err = fmt.Errorf("propertyValue[%d:%d]: %w", ordinal, itemIndex, formatErr)
				break
			}
			valueIndent := "\t\t\t"
			if property.IsArray {
				valueIndent += "\t"
			}
			_, err = fmt.Fprintf(writer, "%sVALUE %s%s\n", valueIndent, formattedItem, propertyItemComment(property.Type, item))
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	if err != nil {
		return fmt.Errorf("definitionOutput: %w", err)
	}
	err = writeRenderDefinition(destinationPath, declaration, definition.Bytes())
	if err != nil {
		return fmt.Errorf("definitionMerge: %w", err)
	}
	return nil
}

func readPropertyResource(sourcePath, identity string, ordinal int, declaration string) (*prop.Document, error) {
	definitionPayload, err := readOrdinalRenderDefinition(sourcePath, map[string]bool{declaration: true}, identity, ordinal)
	if err != nil {
		return nil, fmt.Errorf("definitionSelect: %w", err)
	}
	parser := newParser(bytes.NewReader(definitionPayload))
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	if len(definition) != 2 || definition[0] != declaration {
		return nil, fmt.Errorf("definition: got %v, want %s", definition, declaration)
	}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return nil, err
	}
	parsedVersion, err := parseUint(versionFields[0], 32)
	if err != nil {
		return nil, fmt.Errorf("versionParse: %w", err)
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	_, _, err = parser.optionalProperty("ORDINAL", 1)
	if err != nil {
		return nil, fmt.Errorf("ordinalRead: %w", err)
	}
	countFields, err := parser.property("NUMPROPERTIES", 1)
	if err != nil {
		return nil, err
	}
	propertyCount, err := strconv.Atoi(countFields[0])
	if err != nil || propertyCount < 0 {
		return nil, fmt.Errorf("propertyCount: %q", countFields[0])
	}
	document := &prop.Document{Properties: make([]prop.Property, 0, propertyCount)}
	primitiveDeclarations := make(map[uint32]bool)
	for ordinal := 0; ordinal < propertyCount; ordinal++ {
		propertyFields, readErr := parser.next()
		if readErr != nil {
			return nil, fmt.Errorf("property[%d]: %w", ordinal, readErr)
		}
		if len(propertyFields) != 2 || propertyFields[0] != "PROPERTY" && propertyFields[0] != "ARRAYPROPERTY" && propertyFields[0] != "PRIMITIVE" && propertyFields[0] != "ARRAYPRIMITIVE" && propertyFields[0] != "HASHEDPROPERTY" && propertyFields[0] != "ARRAYHASHEDPROPERTY" {
			return nil, fmt.Errorf("property[%d]: got %v", ordinal, propertyFields)
		}
		isPrimitive := propertyFields[0] == "PRIMITIVE" || propertyFields[0] == "ARRAYPRIMITIVE"
		isHashed := propertyFields[0] == "HASHEDPROPERTY" || propertyFields[0] == "ARRAYHASHEDPROPERTY"
		isDeclaredArray := propertyFields[0] == "ARRAYPROPERTY" || propertyFields[0] == "ARRAYPRIMITIVE" || propertyFields[0] == "ARRAYHASHEDPROPERTY"
		propertyID, isFound := prop.ID(propertyFields[1])
		if isPrimitive || isHashed {
			if !strings.HasPrefix(propertyFields[1], "0x") {
				return nil, fmt.Errorf("propertyHash[%d]: got %q", ordinal, propertyFields[1])
			}
			parsedID, parseErr := audioBaseEntryID(propertyFields[1])
			if parseErr != nil {
				return nil, fmt.Errorf("primitiveName[%d]: %w", ordinal, parseErr)
			}
			propertyID = parsedID
			isFound = true
			if isPrimitive {
				primitiveDeclarations[propertyID] = true
			}
		}
		if !isFound {
			return nil, fmt.Errorf("propertyName[%d]: unknown %q", ordinal, propertyFields[1])
		}
		typeFields, readErr := parser.property("TYPE", 1)
		if readErr != nil {
			return nil, fmt.Errorf("propertyType[%d]: %w", ordinal, readErr)
		}
		propertyType, isFound := propertyTypeCode(typeFields[0])
		if !isFound {
			return nil, fmt.Errorf("propertyType[%d]: got %q", ordinal, typeFields[0])
		}
		flags, readErr := parser.uint32Property("FLAGS")
		if readErr != nil || flags > uint32(^uint16(0)) {
			return nil, fmt.Errorf("propertyFlags[%d]: %v", ordinal, readErr)
		}
		isArray := flags&0x30 != 0
		if isArray && flags&0x40 != 0 {
			return nil, fmt.Errorf("propertyFlags[%d]: unsupported 0x%04X", ordinal, flags)
		}
		if isArray != isDeclaredArray {
			return nil, fmt.Errorf("propertyFraming[%d]: %s conflicts with flags 0x%04X", ordinal, propertyFields[0], flags)
		}
		itemSize := uint32(0)
		valueCount := 1
		if isArray {
			itemSize, readErr = parser.uint32Property("ITEMSIZE")
			if readErr != nil {
				return nil, fmt.Errorf("propertyItemSize[%d]: %w", ordinal, readErr)
			}
			valueFields, valueErr := parser.property("NUMVALUES", 1)
			if valueErr != nil {
				return nil, fmt.Errorf("propertyValueCount[%d]: %w", ordinal, valueErr)
			}
			valueCount, readErr = strconv.Atoi(valueFields[0])
			if readErr != nil || valueCount < 0 {
				return nil, fmt.Errorf("propertyValueCount[%d]: %q", ordinal, valueFields[0])
			}
		}
		propertyName := propertyFields[1]
		if isPrimitive || isHashed {
			propertyName = ""
		}
		property := prop.Property{Name: propertyName, ID: propertyID, Type: propertyType, Flags: uint16(flags), IsArray: isArray, ItemSize: itemSize, Items: make([][]byte, valueCount)}
		for valueIndex := range property.Items {
			itemFields, itemErr := parser.property("VALUE", 1)
			if itemErr != nil {
				return nil, fmt.Errorf("propertyValue[%d:%d]: %w", ordinal, valueIndex, itemErr)
			}
			propertyItem, valueErr := parsePropertyItem(property.Type, property.IsArray, itemFields[0])
			if valueErr != nil {
				return nil, fmt.Errorf("propertyValue[%d:%d]: %w", ordinal, valueIndex, valueErr)
			}
			property.Items[valueIndex] = propertyItem
		}
		document.Properties = append(document.Properties, property)
	}
	primitiveIDs, primitiveErr := primitivePropertyIDs(document)
	if primitiveErr != nil {
		return nil, primitiveErr
	}
	for primitiveID := range primitiveDeclarations {
		if !primitiveIDs[primitiveID] {
			return nil, fmt.Errorf("primitiveDeclaration: 0x%08X is absent from primitives", primitiveID)
		}
	}
	remaining, err := parser.next()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("definitionTrailing: %w", err)
	}
	if len(remaining) != 0 {
		return nil, fmt.Errorf("definitionTrailing: %v", remaining)
	}
	return document, nil
}

func primitivePropertyIDs(document *prop.Document) (map[uint32]bool, error) {
	primitiveIDs := make(map[uint32]bool)
	for propertyIndex, property := range document.Properties {
		if property.ID != 0x3DA0A727 {
			continue
		}
		if property.Type != prop.TypeUInt32 || !property.IsArray {
			return nil, fmt.Errorf("primitivesShape[%d]: type 0x%04X array %t", propertyIndex, property.Type, property.IsArray)
		}
		for itemIndex, item := range property.Items {
			if len(item) != 4 {
				return nil, fmt.Errorf("primitiveSize[%d:%d]: %d", propertyIndex, itemIndex, len(item))
			}
			primitiveIDs[binary.BigEndian.Uint32(item)] = true
		}
	}
	return primitiveIDs, nil
}

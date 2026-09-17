package ds

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/scaleform"
)

func writeScaleformResource(destinationPath, identity string, resourceType uint32, payload []byte) error {
	if resourceType == scaleform.MovieResourceType {
		return writeScaleformMovie(destinationPath, identity, payload)
	}
	image, err := scaleform.DecodeImage(payload)
	if err != nil {
		return fmt.Errorf("imageDecode: %w", err)
	}
	ddsName := strings.TrimSuffix(filepath.Base(destinationPath), filepath.Ext(destinationPath)) + ".dds"
	ddsPath := filepath.Join(filepath.Dir(destinationPath), ddsName)
	err = os.WriteFile(ddsPath, image.DDS, 0o644)
	if err != nil {
		return fmt.Errorf("ddsWrite: %w", err)
	}
	var definition bytes.Buffer
	_, err = fmt.Fprintf(&definition, "%sGFXIMAGE %q\n\tVERSION %d\n\tDDS %q\n", resourceDSEHeader, identity, version, ddsName)
	if err != nil {
		return fmt.Errorf("definitionRender: %w", err)
	}
	err = writeRenderDefinition(destinationPath, "GFXIMAGE", definition.Bytes())
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func readScaleformResource(sourcePath, identity string, resourceType uint32) ([]byte, error) {
	if resourceType == scaleform.MovieResourceType {
		return readScaleformMovie(sourcePath, identity)
	}
	definition, err := readRenderDefinition(sourcePath, "GFXIMAGE")
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	parser := newParser(bytes.NewReader(definition))
	header, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("headerRead: %w", err)
	}
	if len(header) != 2 || header[0] != "GFXIMAGE" || header[1] != identity {
		return nil, fmt.Errorf("header: got %v, want GFXIMAGE %q", header, identity)
	}
	err = readScaleformVersion(parser)
	if err != nil {
		return nil, err
	}
	ddsFields, err := parser.property("DDS", 1)
	if err != nil {
		return nil, fmt.Errorf("ddsRead: %w", err)
	}
	ddsPath, err := scaleformSidecarPath(sourcePath, ddsFields[0], ".dds")
	if err != nil {
		return nil, fmt.Errorf("ddsPath: %w", err)
	}
	ddsPayload, err := os.ReadFile(ddsPath)
	if err != nil {
		return nil, fmt.Errorf("ddsOpen: %w", err)
	}
	payload, err := scaleform.EncodeImage(&scaleform.Image{DDS: ddsPayload})
	if err != nil {
		return nil, fmt.Errorf("imageEncode: %w", err)
	}
	return payload, nil
}

func writeScaleformMovie(destinationPath, identity string, payload []byte) error {
	err := writeScaleformTypeScriptWorkspace(destinationPath, nil)
	if err != nil {
		return fmt.Errorf("typescriptWorkspace: %w", err)
	}
	movie, err := scaleform.DecodeMovie(payload)
	if err != nil {
		return fmt.Errorf("movieDecode: %w", err)
	}
	actionBlocks, err := scaleform.ExtractActionBlocks(movie.Tags)
	if err != nil {
		return fmt.Errorf("actionExtract: %w", err)
	}
	baseName := strings.TrimSuffix(filepath.Base(destinationPath), filepath.Ext(destinationPath))
	actionSourceName := baseName + ".ts"
	actionSource, err := scaleform.WriteTypeScriptBlocks(actionBlocks)
	if err != nil {
		return fmt.Errorf("actionRender: %w", err)
	}
	actionSourcePath := filepath.Join(filepath.Dir(destinationPath), actionSourceName)
	err = os.WriteFile(actionSourcePath, actionSource, 0o644)
	if err != nil {
		return fmt.Errorf("actionWrite: %w", err)
	}
	err = writeScaleformTypeScriptWorkspace(destinationPath, [][]byte{actionSource})
	if err != nil {
		return fmt.Errorf("typescriptWorkspaceRefresh: %w", err)
	}
	var definition bytes.Buffer
	writer := bufio.NewWriter(&definition)
	_, err = fmt.Fprintf(writer, "%sGFX %q\n\tVERSION %d\n\tSWFVERSION %d\n\tCOMPRESSION %q\n\tXMIN %d\n\tXMAX %d\n\tYMIN %d\n\tYMAX %d\n\tFRAMERATERAW %d\n\tFRAMECOUNT %d\n\tNUMTAGS %d\n", resourceDSEHeader, identity, version, movie.Version, movie.Compression, movie.FrameSize.XMin, movie.FrameSize.XMax, movie.FrameSize.YMin, movie.FrameSize.YMax, movie.FrameRateRaw, movie.FrameCount, len(movie.Tags))
	for tagIndex, tag := range movie.Tags {
		if err != nil {
			break
		}
		err = writeScaleformTag(writer, tagIndex, tag)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tACTIONS %q\n", actionSourceName)
	}
	if err == nil {
		err = writer.Flush()
	}
	if err != nil {
		return fmt.Errorf("definitionRender: %w", err)
	}
	err = writeRenderDefinition(destinationPath, "GFX", definition.Bytes())
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func writeScaleformTag(writer *bufio.Writer, tagIndex int, tag scaleform.Tag) error {
	path := fmt.Sprintf("root/tag_%04d", tagIndex)
	switch tag.Code {
	case 12:
		_, err := fmt.Fprintf(writer, "\t\tDOACTION %q\n", path)
		if err != nil {
			return fmt.Errorf("doAction: %w", err)
		}
		return nil
	case 59:
		if len(tag.Payload) < 2 {
			return fmt.Errorf("doInitAction[%s]: short", path)
		}
		spriteID := binary.LittleEndian.Uint16(tag.Payload[:2])
		_, err := fmt.Fprintf(writer, "\t\tDOINITACTION %q\n\t\t\tSPRITEID %d\n", path, spriteID)
		if err != nil {
			return fmt.Errorf("doInitAction: %w", err)
		}
		return nil
	default:
		_, err := fmt.Fprintf(writer, "\t\tTAG %q\n\t\t\tCODE %d\n\t\t\tISLONG %d\n", fmt.Sprintf("%04d_%s", tagIndex, scaleformTagName(tag.Code)), tag.Code, boolNumber(tag.IsLong))
		if err != nil {
			return fmt.Errorf("tagHeader: %w", err)
		}
		err = writeScaleformHex(writer, tag.Payload, "\t\t\t")
		if err != nil {
			return fmt.Errorf("tagPayload: %w", err)
		}
		return nil
	}
}

func readScaleformMovie(sourcePath, identity string) ([]byte, error) {
	definition, err := readRenderDefinition(sourcePath, "GFX")
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	parser := newParser(bytes.NewReader(definition))
	header, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("headerRead: %w", err)
	}
	if len(header) != 2 || header[0] != "GFX" || header[1] != identity {
		return nil, fmt.Errorf("header: got %v, want GFX %q", header, identity)
	}
	err = readScaleformVersion(parser)
	if err != nil {
		return nil, err
	}
	movie := &scaleform.Movie{}
	swfVersion, err := readScaleformUint(parser, "SWFVERSION", 8)
	if err != nil {
		return nil, err
	}
	movie.Version = uint8(swfVersion)
	compressionFields, err := parser.property("COMPRESSION", 1)
	if err != nil {
		return nil, fmt.Errorf("compressionRead: %w", err)
	}
	movie.Compression = compressionFields[0]
	coordinates := []*int32{&movie.FrameSize.XMin, &movie.FrameSize.XMax, &movie.FrameSize.YMin, &movie.FrameSize.YMax}
	coordinateNames := []string{"XMIN", "XMAX", "YMIN", "YMAX"}
	for coordinateIndex, coordinate := range coordinates {
		coordinateFields, readErr := parser.property(coordinateNames[coordinateIndex], 1)
		if readErr != nil {
			return nil, fmt.Errorf("%sRead: %w", strings.ToLower(coordinateNames[coordinateIndex]), readErr)
		}
		coordinateNumber, parseErr := strconv.ParseInt(coordinateFields[0], 10, 32)
		if parseErr != nil {
			return nil, fmt.Errorf("%sParse: %w", strings.ToLower(coordinateNames[coordinateIndex]), parseErr)
		}
		*coordinate = int32(coordinateNumber)
	}
	frameRate, err := readScaleformUint(parser, "FRAMERATERAW", 16)
	if err != nil {
		return nil, err
	}
	movie.FrameRateRaw = uint16(frameRate)
	frameCount, err := readScaleformUint(parser, "FRAMECOUNT", 16)
	if err != nil {
		return nil, err
	}
	movie.FrameCount = uint16(frameCount)
	tagCount, err := readScaleformCount(parser, "NUMTAGS")
	if err != nil {
		return nil, err
	}
	movie.Tags = make([]scaleform.Tag, tagCount)
	for tagIndex := range movie.Tags {
		tagFields, readErr := parser.next()
		if readErr != nil {
			return nil, fmt.Errorf("tag[%d]: %w", tagIndex, readErr)
		}
		tag, parseErr := readScaleformTag(parser, tagIndex, tagFields)
		if parseErr != nil {
			return nil, fmt.Errorf("tag[%d]: %w", tagIndex, parseErr)
		}
		movie.Tags[tagIndex] = tag
	}
	actionFields, err := parser.property("ACTIONS", 1)
	if err != nil {
		return nil, fmt.Errorf("actionsRead: %w", err)
	}
	actionSourcePath, err := scaleformSidecarPath(sourcePath, actionFields[0], ".ts")
	if err != nil {
		return nil, fmt.Errorf("actionsPath: %w", err)
	}
	actionSource, err := os.ReadFile(actionSourcePath)
	if err != nil {
		return nil, fmt.Errorf("actionsOpen: %w", err)
	}
	actionsByPath, err := scaleform.ReadTypeScriptBlocks(actionSource)
	if err != nil {
		return nil, fmt.Errorf("actionsCompile: %w", err)
	}
	err = scaleform.ApplyActionBlocks(movie.Tags, actionsByPath)
	if err != nil {
		return nil, fmt.Errorf("actionApply: %w", err)
	}
	payload, err := scaleform.EncodeMovie(movie)
	if err != nil {
		return nil, fmt.Errorf("movieEncode: %w", err)
	}
	return payload, nil
}

func writeScaleformHex(writer *bufio.Writer, payload []byte, indent string) error {
	lineCount := (len(payload) + opaqueLineSize - 1) / opaqueLineSize
	_, err := fmt.Fprintf(writer, "%sNUMLINES %d\n", indent, lineCount)
	for offset := 0; err == nil && offset < len(payload); offset += opaqueLineSize {
		end := offset + opaqueLineSize
		if end > len(payload) {
			end = len(payload)
		}
		_, err = fmt.Fprintf(writer, "%s\tHEX %s\n", indent, strings.ToUpper(hex.EncodeToString(payload[offset:end])))
	}
	return err
}

func readScaleformHex(parser *parser) ([]byte, error) {
	lineCount, err := readScaleformCount(parser, "NUMLINES")
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("hexSize[%d]: %d", lineIndex, len(line))
		}
		payload = append(payload, line...)
	}
	return payload, nil
}

func readScaleformTag(parser *parser, tagIndex int, fields []string) (scaleform.Tag, error) {
	if len(fields) != 2 {
		return scaleform.Tag{}, fmt.Errorf("header: %v", fields)
	}
	path := fmt.Sprintf("root/tag_%04d", tagIndex)
	switch fields[0] {
	case "DOACTION":
		if fields[1] != path {
			return scaleform.Tag{}, fmt.Errorf("doActionPath: got %q, want %q", fields[1], path)
		}
		return scaleform.Tag{Code: 12}, nil
	case "DOINITACTION":
		if fields[1] != path {
			return scaleform.Tag{}, fmt.Errorf("doInitActionPath: got %q, want %q", fields[1], path)
		}
		spriteID, err := readScaleformUint(parser, "SPRITEID", 16)
		if err != nil {
			return scaleform.Tag{}, fmt.Errorf("doInitActionSprite: %w", err)
		}
		payload := make([]byte, 2)
		binary.LittleEndian.PutUint16(payload, uint16(spriteID))
		return scaleform.Tag{Code: 59, Payload: payload}, nil
	case "TAG":
		expectedPrefix := fmt.Sprintf("%04d_", tagIndex)
		if !strings.HasPrefix(fields[1], expectedPrefix) {
			return scaleform.Tag{}, fmt.Errorf("tagName: %q", fields[1])
		}
		code, err := readScaleformUint(parser, "CODE", 10)
		if err != nil {
			return scaleform.Tag{}, fmt.Errorf("tagCode: %w", err)
		}
		isLongNumber, err := readScaleformUint(parser, "ISLONG", 1)
		if err != nil {
			return scaleform.Tag{}, fmt.Errorf("tagLong: %w", err)
		}
		payload, err := readScaleformHex(parser)
		if err != nil {
			return scaleform.Tag{}, fmt.Errorf("tagPayload: %w", err)
		}
		return scaleform.Tag{Code: uint16(code), IsLong: isLongNumber == 1, Payload: payload}, nil
	default:
		return scaleform.Tag{}, fmt.Errorf("headerName: %q", fields[0])
	}
}

func readScaleformVersion(parser *parser) error {
	parsedVersion, err := readScaleformUint(parser, "VERSION", 32)
	if err != nil {
		return err
	}
	if parsedVersion != version {
		return fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	return nil
}

func readScaleformUint(parser *parser, name string, bits int) (uint64, error) {
	fields, err := parser.property(name, 1)
	if err != nil {
		return 0, fmt.Errorf("%sRead: %w", strings.ToLower(name), err)
	}
	number, err := parseUint(fields[0], bits)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", strings.ToLower(name), err)
	}
	return number, nil
}

func readScaleformCount(parser *parser, name string) (int, error) {
	fields, err := parser.property(name, 1)
	if err != nil {
		return 0, fmt.Errorf("%sRead: %w", strings.ToLower(name), err)
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count < 0 {
		return 0, fmt.Errorf("%s: %q", strings.ToLower(name), fields[0])
	}
	return count, nil
}

func scaleformSidecarPath(definitionPath, name, extension string) (string, error) {
	if filepath.Base(name) != name || filepath.Ext(name) != extension || name == extension {
		return "", fmt.Errorf("name: %q", name)
	}
	return filepath.Join(filepath.Dir(definitionPath), name), nil
}

func scaleformSidecarNames(definitionPath string, _ uint32) ([]string, error) {
	payload, err := os.ReadFile(definitionPath)
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	names := make([]string, 0, 1)
	for lineIndex, line := range strings.Split(string(payload), "\n") {
		fields, tokenizeErr := tokenize(strings.TrimSuffix(line, "\r"))
		if tokenizeErr != nil {
			return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, tokenizeErr)
		}
		if len(fields) != 2 || fields[0] != "ACTIONS" && fields[0] != "DDS" {
			continue
		}
		extension := ".ts"
		if fields[0] == "DDS" {
			extension = ".dds"
		}
		_, pathErr := scaleformSidecarPath(definitionPath, fields[1], extension)
		if pathErr != nil {
			return nil, fmt.Errorf("sidecar[%d]: %w", lineIndex+1, pathErr)
		}
		names = append(names, fields[1])
	}
	if len(names) != 1 {
		return nil, fmt.Errorf("sidecarCount: got %d, want 1", len(names))
	}
	return names, nil
}

func writeScaleformTypeScriptWorkspace(destinationPath string, sources [][]byte) error {
	root := filepath.Dir(destinationPath)
	for filepath.Ext(root) != ".ds" {
		parent := filepath.Dir(root)
		if parent == root {
			return fmt.Errorf("dsRoot: %q", destinationPath)
		}
		root = parent
	}
	files := map[string][]byte{
		"scaleform.d.ts": nil,
		"tsconfig.json":  scaleform.TypeScriptConfig(),
	}
	globalNames, err := scaleformTypeScriptGlobals(filepath.Join(root, "scaleform.d.ts"), sources)
	if err != nil {
		return fmt.Errorf("typescriptGlobals: %w", err)
	}
	files["scaleform.d.ts"] = scaleform.TypeScriptDeclarations(globalNames, nil)
	for name, payload := range files {
		path := filepath.Join(root, name)
		existing, err := os.ReadFile(path)
		if err == nil && bytes.Equal(existing, payload) {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%sRead: %w", name, err)
		}
		err = os.WriteFile(path, payload, 0o644)
		if err != nil {
			return fmt.Errorf("%sWrite: %w", name, err)
		}
	}
	return nil
}

var typeScriptGlobalPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)

func scaleformTypeScriptGlobals(declarationPath string, sources [][]byte) ([]string, error) {
	globalNames := make(map[string]bool, 32)
	existing, err := os.ReadFile(declarationPath)
	if err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "declare const ") || !strings.HasSuffix(line, ": AVM1Value;") {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(line, "declare const "), ": AVM1Value;")
			if isScaleformTypeScriptIdentifier(name) && !strings.HasPrefix(name, "register") {
				globalNames[name] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("declarationRead: %w", err)
	}
	for _, payload := range sources {
		unquotedSource := typeScriptUnquoted(payload)
		for _, match := range typeScriptGlobalPattern.FindAllString(unquotedSource, -1) {
			name := strings.TrimSuffix(match, ".")
			if name == "avm1" || strings.HasPrefix(name, "register") || !isScaleformTypeScriptIdentifier(name) {
				continue
			}
			globalNames[name] = true
		}
		for _, line := range strings.Split(unquotedSource, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "avm1.") || strings.HasPrefix(line, "function ") || strings.HasPrefix(line, "if (") {
				continue
			}
			openIndex := strings.IndexByte(line, '(')
			closeIndex := strings.LastIndexByte(line, ')')
			if openIndex < 0 || closeIndex <= openIndex {
				continue
			}
			for _, argument := range strings.Split(line[openIndex+1:closeIndex], ",") {
				name := strings.TrimSpace(argument)
				if name == "true" || name == "false" || name == "null" || name == "undefined" || strings.HasPrefix(name, "register") || !isScaleformTypeScriptIdentifier(name) {
					continue
				}
				globalNames[name] = true
			}
		}
	}
	names := make([]string, 0, len(globalNames))
	for name := range globalNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func typeScriptUnquoted(payload []byte) string {
	content := make([]byte, len(payload))
	isQuoted := false
	isEscaped := false
	for index, character := range payload {
		if isEscaped {
			isEscaped = false
			content[index] = ' '
			continue
		}
		if isQuoted && character == '\\' {
			isEscaped = true
			content[index] = ' '
			continue
		}
		if character == '"' {
			isQuoted = !isQuoted
			content[index] = ' '
			continue
		}
		if isQuoted {
			content[index] = ' '
			continue
		}
		content[index] = character
	}
	return string(content)
}

func isScaleformTypeScriptIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for characterIndex, character := range name {
		isLetter := character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		if !isLetter && (characterIndex == 0 || character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func scaleformTagName(code uint16) string {
	names := map[uint16]string{0: "End", 1: "ShowFrame", 9: "SetBackgroundColor", 12: "DoAction", 26: "PlaceObject2", 28: "RemoveObject2", 39: "DefineSprite", 43: "FrameLabel", 56: "ExportAssets", 59: "DoInitAction", 69: "FileAttributes", 76: "SymbolClass", 77: "Metadata", 82: "DoABC", 86: "DefineSceneAndFrameLabelData", 1000: "ExporterInfo", 1001: "DefineExternalImage", 1002: "FontTextureInfo", 1004: "DefineExternalImage2"}
	if name := names[code]; name != "" {
		return name
	}
	return fmt.Sprintf("Code%d", code)
}

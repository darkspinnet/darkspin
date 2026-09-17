package ds

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/render/prop"
)

const (
	audioBaseGroup    = 0x40494600
	audioBaseInstance = 0x4CF9B596
)

func isAudioBaseResource(entry dbpf.Entry) bool {
	return entry.Type == prop.AudioResourceType && entry.Group == audioBaseGroup && uint32(entry.Instance) == audioBaseInstance
}

func writeAudioBaseResource(destinationPath, identity string, document *prop.Document, names map[uint32]string) error {
	for entryIndex, entry := range document.Properties {
		if entry.Type != prop.TypeString8 || entry.Flags != 0x000D || entry.IsArray || len(entry.Items) != 1 {
			return fmt.Errorf("entryShape[%d]: type 0x%04X flags 0x%04X array %t items %d", entryIndex, entry.Type, entry.Flags, entry.IsArray, len(entry.Items))
		}
		if len(entry.Items[0]) < 4 {
			return fmt.Errorf("entryString[%d]: got %d bytes", entryIndex, len(entry.Items[0]))
		}
		stringSize := binary.BigEndian.Uint32(entry.Items[0][:4])
		if uint64(stringSize) != uint64(len(entry.Items[0])-4) {
			return fmt.Errorf("entryString[%d]: declared %d, got %d", entryIndex, stringSize, len(entry.Items[0])-4)
		}
	}
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
	_, err = fmt.Fprintf(writer, "// darkspin resource dse v%d\n", version)
	if err == nil {
		_, err = fmt.Fprintf(writer, "AUDIOBASE %q\n", identity)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tVERSION %d\n", version)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMENTRIES %d\n", len(document.Properties))
	}
	for entryIndex, entry := range document.Properties {
		if err != nil {
			break
		}
		entryKey := fmt.Sprintf("0x%08X", entry.ID)
		entryName := names[entry.ID]
		if entryName != "" && hashResourceName(entryName) == entry.ID {
			entryKey = strconv.Quote(entryName)
		}
		_, err = fmt.Fprintf(writer, "\t\tENTRY %s // %d\n", entryKey, entryIndex)
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tVALUE %q\n", string(entry.Items[0][4:]))
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

func readAudioBaseResource(sourcePath, identity string) (*prop.Document, error) {
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
	if len(definition) != 2 || definition[0] != "AUDIOBASE" && definition[0] != "DARKSPINAUDIOBASE" || definition[1] != identity {
		return nil, fmt.Errorf("definition: got %v, want AUDIOBASE %q", definition, identity)
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
		return nil, fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	countFields, err := parser.property("NUMENTRIES", 1)
	if err != nil {
		return nil, fmt.Errorf("entryCountRead: %w", err)
	}
	entryCount, err := strconv.Atoi(countFields[0])
	if err != nil || entryCount < 0 {
		return nil, fmt.Errorf("entryCount: %q", countFields[0])
	}
	document := &prop.Document{Properties: make([]prop.Property, 0, entryCount)}
	for entryIndex := 0; entryIndex < entryCount; entryIndex++ {
		entryFields, readErr := parser.next()
		if readErr != nil {
			return nil, fmt.Errorf("entry[%d]: %w", entryIndex, readErr)
		}
		if len(entryFields) != 2 || entryFields[0] != "ENTRY" {
			return nil, fmt.Errorf("entry[%d]: got %v", entryIndex, entryFields)
		}
		entryID, parseErr := audioBaseEntryID(entryFields[1])
		if parseErr != nil {
			return nil, fmt.Errorf("entryKey[%d]: %w", entryIndex, parseErr)
		}
		valueFields, readErr := parser.property("VALUE", 1)
		if readErr != nil {
			return nil, fmt.Errorf("entryValue[%d]: %w", entryIndex, readErr)
		}
		item, parseErr := parsePropertyItem(prop.TypeString8, false, valueFields[0])
		if parseErr != nil {
			return nil, fmt.Errorf("entryValue[%d]: %w", entryIndex, parseErr)
		}
		document.Properties = append(document.Properties, prop.Property{ID: entryID, Type: prop.TypeString8, Flags: 0x000D, Items: [][]byte{item}})
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

func audioBaseEntryID(entryKey string) (uint32, error) {
	if strings.HasPrefix(entryKey, "0x") {
		entryID, err := parseUint(entryKey, 32)
		if err != nil {
			return 0, fmt.Errorf("numericKey: %w", err)
		}
		return uint32(entryID), nil
	}
	if entryKey == "" {
		return 0, errors.New("empty key")
	}
	return hashResourceName(entryKey), nil
}

func hashResourceName(name string) uint32 {
	hash := uint32(0x811C9DC5)
	for characterIndex := 0; characterIndex < len(name); characterIndex++ {
		hash *= 0x01000193
		character := name[characterIndex]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		hash ^= uint32(character)
	}
	return hash
}

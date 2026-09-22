// Package audio identifies Game audio resources and their property links.
package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/render/prop"
)

const (
	SNRResourceType = 0x01A527DB
	SNSResourceType = 0x01EEF63A
	PDResourceType  = 0x617715D9
	PDRResourceType = 0x022D2C83
	samplesProperty = 0x701ED91E
)

// ClientNames contains audio resource identities recovered from retail client
// string tables and string-to-key calls. Each key is the lowercase FNV
// identity of its name.
func ClientNames() map[uint32]string {
	return map[uint32]string{
		0x72B275A3: "mixmode_inReplay",
		0x80504C15: "mixmode_inEditor",
		0xAEAF88A9: "mixmode_inPrePvp",
		0xAD7F8693: "listener_editor",
		0xD261BB69: "mixmode_inCashout",
		0xEDD98567: "listener_ingame",
	}
}

// InferredNames contains exact hash preimages inferred from retail client
// states and the behavior of otherwise unnamed AudioProps resources. These
// names are useful editable identities, but are not claimed as recovered
// authored strings.
func InferredNames() map[uint32]string {
	return map[uint32]string{
		0x48E99B83: "mixmode_PvPResults",
		0x7E947D96: "mixmode_inpostgame",
		0xDA38E226: "mixmode_inSpaceship",
		0xEBEE413F: "mixmode_inpregame",
	}
}

// PatchParameterAliases recovers resource names referenced symbolically by
// Pure Data gfparam objects in the same package.
func PatchParameterAliases(sourcePath string, names map[uint32]string) (map[uint32]string, error) {
	pkg, r, err := openPackage(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	instances := make(map[uint32]bool, len(pkg.Entries))
	for _, entry := range pkg.Entries {
		if entry.Instance <= uint64(^uint32(0)) {
			instances[uint32(entry.Instance)] = true
		}
	}
	candidatesByInstance := make(map[uint32]map[string]bool)
	for ordinal, entry := range pkg.Entries {
		if !IsPatchType(entry.Type) {
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return nil, fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		for _, parameterName := range patchGlobalParameters(payload) {
			instanceID := hashName(parameterName)
			if !instances[instanceID] {
				continue
			}
			if candidatesByInstance[instanceID] == nil {
				candidatesByInstance[instanceID] = make(map[string]bool)
			}
			candidatesByInstance[instanceID][parameterName] = true
		}
	}
	aliases := make(map[uint32]string)
	for instanceID, candidates := range candidatesByInstance {
		if names[instanceID] != "" || len(candidates) != 1 {
			continue
		}
		for candidate := range candidates {
			aliases[instanceID] = candidate
		}
	}
	return aliases, nil
}

func patchGlobalParameters(payload []byte) []string {
	marker := []byte("gfparam ")
	parameters := make([]string, 0)
	seenParameters := make(map[string]bool)
	for searchOffset := 0; searchOffset < len(payload); {
		markerOffset := bytes.Index(payload[searchOffset:], marker)
		if markerOffset < 0 {
			break
		}
		nameStart := searchOffset + markerOffset + len(marker)
		nameEnd := nameStart
		for nameEnd < len(payload) && isPatchParameterCharacter(payload[nameEnd]) {
			nameEnd++
		}
		if nameEnd > nameStart {
			parameterName := string(payload[nameStart:nameEnd])
			if !seenParameters[parameterName] {
				parameters = append(parameters, parameterName)
				seenParameters[parameterName] = true
			}
		}
		searchOffset = nameStart
	}
	return parameters
}

func isPatchParameterCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_'
}

// Header describes the editable metadata stored in an EA AudioCore SNR.
type Header struct {
	Version     uint8
	Codec       string
	Channels    uint8
	SampleRate  uint32
	Storage     string
	IsLooped    bool
	SampleCount uint32
}

// DecodeHeader reads the common eight-byte EA AudioCore SNR header.
func DecodeHeader(payload []byte) (Header, error) {
	if len(payload) < 8 {
		return Header{}, fmt.Errorf("headerSize: got %d, want at least 8", len(payload))
	}
	first := binary.BigEndian.Uint32(payload[0:4])
	second := binary.BigEndian.Uint32(payload[4:8])
	codecCode := uint8((first >> 24) & 0x0F)
	codecNames := map[uint8]string{
		0x00: "NONE", 0x01: "RESERVED", 0x02: "PCM16BE", 0x03: "EAXMA",
		0x04: "XAS1", 0x05: "EALAYER3_V1", 0x06: "EALAYER3_V2_PCM",
		0x07: "EALAYER3_V2_SPIKE", 0x08: "GCADPCM", 0x09: "EASPEEX",
		0x0A: "EATRAX", 0x0B: "EAMP3", 0x0C: "EAOPUS", 0x0D: "EAATRAC9",
		0x0E: "EAOPUSM",
	}
	codec, isFound := codecNames[codecCode]
	if !isFound {
		codec = fmt.Sprintf("UNKNOWN_0x%02X", codecCode)
	}
	storageNames := []string{"RAM", "STREAM", "GIGASAMPLE", "UNKNOWN"}
	return Header{
		Version:     uint8(first >> 28),
		Codec:       codec,
		Channels:    uint8((first>>18)&0x3F) + 1,
		SampleRate:  first & 0x03FFFF,
		Storage:     storageNames[(second>>30)&0x03],
		IsLooped:    ((second >> 29) & 0x01) != 0,
		SampleCount: second & 0x1FFFFFFF,
	}, nil
}

// EncodeHeader applies editable common metadata without disturbing codec data
// or optional fields after the common SNR header.
func EncodeHeader(payload []byte, header Header) ([]byte, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("headerSize: got %d, want at least 8", len(payload))
	}
	if header.Version > 0x0F {
		return nil, fmt.Errorf("versionRange: %d", header.Version)
	}
	if header.Channels == 0 || header.Channels > 64 {
		return nil, fmt.Errorf("channelRange: %d", header.Channels)
	}
	if header.SampleRate > 0x03FFFF {
		return nil, fmt.Errorf("sampleRateRange: %d", header.SampleRate)
	}
	if header.SampleCount > 0x1FFFFFFF {
		return nil, fmt.Errorf("sampleCountRange: %d", header.SampleCount)
	}
	codecCodes := map[string]uint8{
		"NONE": 0x00, "XAS0": 0x00, "RESERVED": 0x01, "PCM16BE": 0x02, "EAXMA": 0x03,
		"XAS1": 0x04, "EALAYER3_V1": 0x05, "EALAYER3_V2_PCM": 0x06,
		"EALAYER3_V2_SPIKE": 0x07, "GCADPCM": 0x08, "EASPEEX": 0x09,
		"EATRAX": 0x0A, "EAMP3": 0x0B, "EAOPUS": 0x0C, "EAATRAC9": 0x0D,
		"EAOPUSM":      0x0E,
		"UNKNOWN_0x0F": 0x0F,
	}
	codecCode, isFound := codecCodes[header.Codec]
	if !isFound {
		return nil, fmt.Errorf("codecUnsupported: %q", header.Codec)
	}
	storageCodes := map[string]uint32{"RAM": 0, "STREAM": 1, "GIGASAMPLE": 2, "UNKNOWN": 3}
	storageCode, isFound := storageCodes[header.Storage]
	if !isFound {
		return nil, fmt.Errorf("storageUnsupported: %q", header.Storage)
	}
	first := uint32(header.Version)<<28 | uint32(codecCode)<<24 | uint32(header.Channels-1)<<18 | header.SampleRate
	second := storageCode<<30 | header.SampleCount
	if header.IsLooped {
		second |= 1 << 29
	}
	encodedPayload := append([]byte(nil), payload...)
	binary.BigEndian.PutUint32(encodedPayload[0:4], first)
	binary.BigEndian.PutUint32(encodedPayload[4:8], second)
	return encodedPayload, nil
}

// SampleAliases derives stable stream names from AudioProps sample references.
// Existing registry names always remain authoritative.
func SampleAliases(sourcePath string, names map[uint32]string) (map[uint32]string, error) {
	records, err := SampleAliasRecords(sourcePath, names)
	if err != nil {
		return nil, fmt.Errorf("aliasRecords: %w", err)
	}
	aliases := make(map[uint32]string, len(records))
	for instanceID, record := range records {
		if names[instanceID] == "" {
			aliases[instanceID] = record.Name
		}
	}
	return aliases, nil
}

// SampleInstances returns every zero-scope SNR instance referenced by the
// AudioProps samples field.
func SampleInstances(sourcePath string) (map[uint32]bool, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return nil, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("packageRead: %w", err)
	}
	instances := make(map[uint32]bool)
	for ordinal, entry := range pkg.Entries {
		if entry.Type != prop.AudioResourceType {
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return nil, fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		document, decodeErr := prop.Decode(payload)
		if decodeErr != nil {
			return nil, fmt.Errorf("resourceDecode[%d]: %w", ordinal, decodeErr)
		}
		for _, property := range document.Properties {
			if property.ID != samplesProperty || property.Type != prop.TypeKey {
				continue
			}
			for itemIndex, item := range property.Items {
				if len(item) < 12 {
					return nil, fmt.Errorf("sampleKey[%d:%d]: got %d bytes", ordinal, itemIndex, len(item))
				}
				if binary.LittleEndian.Uint32(item[4:8]) != 0 || binary.LittleEndian.Uint32(item[8:12]) != 0 {
					continue
				}
				instances[binary.LittleEndian.Uint32(item[:4])] = true
			}
		}
	}
	return instances, nil
}

type propertyReference struct {
	ownerID    uint32
	targetID   uint32
	targetName string
	roleName   string
	itemIndex  int
	itemCount  int
}

// InheritedPropertyAliases follows zero-scope keys between AudioProps entries
// and derives a name only when all currently named owners agree on one alias.
func InheritedPropertyAliases(sourcePath string, names map[uint32]string) (map[uint32]string, error) {
	pkg, r, err := openPackage(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	targets := make(map[uint32]bool, len(pkg.Entries))
	for _, entry := range pkg.Entries {
		if entry.Type == prop.AudioResourceType {
			targets[uint32(entry.Instance)] = true
		}
	}
	references := make([]propertyReference, 0)
	for ordinal, entry := range pkg.Entries {
		if entry.Type != prop.AudioResourceType {
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return nil, fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		document, decodeErr := prop.Decode(payload)
		if decodeErr != nil {
			return nil, fmt.Errorf("resourceDecode[%d]: %w", ordinal, decodeErr)
		}
		for _, property := range document.Properties {
			if property.Type != prop.TypeKey {
				continue
			}
			roleName, isFound := prop.Name(property.ID)
			if !isFound {
				roleName = ""
			}
			roleName = ReadablePointerName(roleName)
			for itemIndex, item := range property.Items {
				if len(item) < 4 {
					continue
				}
				targetID := binary.LittleEndian.Uint32(item[:4])
				if !targets[targetID] {
					continue
				}
				references = append(references, propertyReference{
					ownerID: uint32(entry.Instance), targetID: targetID, roleName: roleName,
					itemIndex: itemIndex, itemCount: len(property.Items),
				})
			}
		}
	}
	resolvedNames := make(map[uint32]string, len(names))
	for nameID, name := range names {
		resolvedNames[nameID] = strings.TrimSuffix(name, "~")
	}
	aliases := make(map[uint32]string)
	for {
		candidatesByInstance := make(map[uint32]map[string]bool)
		for _, reference := range references {
			if resolvedNames[reference.targetID] != "" {
				continue
			}
			ownerName := resolvedNames[reference.ownerID]
			if ownerName == "" {
				continue
			}
			alias := ownerName
			if reference.roleName != "" {
				alias += "_" + reference.roleName
			}
			if reference.itemCount > 1 {
				alias += fmt.Sprintf("_%02d", reference.itemIndex+1)
			}
			if candidatesByInstance[reference.targetID] == nil {
				candidatesByInstance[reference.targetID] = make(map[string]bool)
			}
			candidatesByInstance[reference.targetID][alias] = true
		}
		addedCount := 0
		for instanceID, candidates := range candidatesByInstance {
			if len(candidates) != 1 {
				continue
			}
			for alias := range candidates {
				resolvedNames[instanceID] = alias
				aliases[instanceID] = alias
				addedCount++
			}
		}
		if addedCount == 0 {
			break
		}
	}
	return aliases, nil
}

// IsStreamType reports whether a DBPF resource is an EA audio stream.
func IsStreamType(typeCode uint32) bool {
	return typeCode == SNRResourceType || typeCode == SNSResourceType
}

// IsPatchType reports whether a resource contains a RefPack-compressed Pure
// Data audio graph.
func IsPatchType(typeCode uint32) bool {
	return typeCode == PDResourceType || typeCode == PDRResourceType
}

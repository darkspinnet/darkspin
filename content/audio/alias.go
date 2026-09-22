package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/darkspinnet/darkspin/content/render/prop"
)

// SampleReference describes one AudioProps event pointer to an audio stream.
// The pointer and owner names are searchable provenance, not authored stream
// names, and are therefore emitted as comments by the DS renderer.
type SampleReference struct {
	ArchiveName   string
	EventInstance uint32
	EventName     string
	ContextNames  []string
	Usages        []SampleUsage
	PropertyID    uint32
	PropertyName  string
	PointerName   string
	ItemIndex     int
	ItemCount     int
	IsLooped      bool
}

// SampleAlias records the deterministic editable name chosen for a stream and
// every AudioProps pointer that contributed evidence for that choice.
type SampleAlias struct {
	Name       string
	Source     string
	References []SampleReference
}

var readablePointerNames = map[string]string{
	"foot_step":     "footstep",
	"samples":       "sample",
	"soundid":       "sound",
	"startsounds":   "start",
	"voicetemplate": "voice",
}

// ReadablePointerName maps stable property symbols to concise words used in
// inferred aliases and metadata. Unknown symbols remain intact.
func ReadablePointerName(propertyName string) string {
	readableName := readablePointerNames[strings.ToLower(propertyName)]
	if readableName != "" {
		return readableName
	}
	return propertyName
}

// SampleAliasRecords derives a deterministic alias for every referenced audio
// stream and retains all of the direct AudioProps references as provenance.
func SampleAliasRecords(sourcePath string, names map[uint32]string) (map[uint32]SampleAlias, error) {
	pkg, r, err := openPackage(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()

	documentsByInstance := make(map[uint32]*prop.Document)
	for ordinal, entry := range pkg.Entries {
		if entry.Type != prop.AudioResourceType {
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return nil, fmt.Errorf("contextOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("contextRead[%d]: %w", ordinal, readErr)
		}
		document, decodeErr := prop.Decode(payload)
		if decodeErr != nil {
			return nil, fmt.Errorf("contextDecode[%d]: %w", ordinal, decodeErr)
		}
		documentsByInstance[uint32(entry.Instance)] = document
	}

	usagesByEvent := make(map[uint32][]SampleUsage)
	if strings.EqualFold(filepath.Base(sourcePath), "AudioProps.package") {
		usagesByEvent = DarksporeUsages()
	}
	referencesByInstance := make(map[uint32][]SampleReference)
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
		contextNames, isLooped := audioEventContext(document, documentsByInstance, names)
		for _, property := range document.Properties {
			if property.ID != samplesProperty || property.Type != prop.TypeKey {
				continue
			}
			propertyName, isFound := prop.Name(property.ID)
			if !isFound {
				propertyName = fmt.Sprintf("property_%08x", property.ID)
			}
			for itemIndex, item := range property.Items {
				if len(item) < 12 {
					return nil, fmt.Errorf("sampleKey[%d:%d]: got %d bytes", ordinal, itemIndex, len(item))
				}
				if binary.LittleEndian.Uint32(item[4:8]) != 0 || binary.LittleEndian.Uint32(item[8:12]) != 0 {
					continue
				}
				instanceID := binary.LittleEndian.Uint32(item[:4])
				referencesByInstance[instanceID] = append(referencesByInstance[instanceID], SampleReference{
					ArchiveName:   filepath.Base(sourcePath),
					EventInstance: uint32(entry.Instance),
					EventName:     strings.TrimSuffix(names[uint32(entry.Instance)], "~"),
					ContextNames:  contextNames,
					Usages:        append([]SampleUsage(nil), usagesByEvent[uint32(entry.Instance)]...),
					PropertyID:    property.ID,
					PropertyName:  propertyName,
					PointerName:   ReadablePointerName(propertyName),
					ItemIndex:     itemIndex,
					ItemCount:     len(property.Items),
					IsLooped:      isLooped,
				})
			}
		}
	}

	records := make(map[uint32]SampleAlias, len(referencesByInstance))
	for instanceID, references := range referencesByInstance {
		sort.Slice(references, func(first, second int) bool {
			if references[first].EventName != references[second].EventName {
				return references[first].EventName < references[second].EventName
			}
			if references[first].EventInstance != references[second].EventInstance {
				return references[first].EventInstance < references[second].EventInstance
			}
			return references[first].ItemIndex < references[second].ItemIndex
		})
		isLooped := false
		for _, reference := range references {
			isLooped = isLooped || reference.IsLooped
		}
		name := HumanSampleAlias(names[instanceID], isLooped)
		source := "name registry"
		if name == "" {
			var isResourceReference bool
			name, isResourceReference = preferredSampleAlias(references)
			source = "AudioProps event pointer"
			if isResourceReference {
				source = "resource reference"
			}
		}
		if name == "" {
			name, source = unresolvedSampleAlias(instanceID, references)
		}
		records[instanceID] = SampleAlias{Name: name, Source: source, References: references}
	}
	return records, nil
}

// HumanSampleAlias converts a recovered authored stream name into the stable
// noun/action form used by editable DS audio aliases.
func HumanSampleAlias(name string, isLooped bool) string {
	tokens := audioAliasTokens(strings.TrimSuffix(name, "~"))
	if len(tokens) == 0 {
		return ""
	}
	if isLooped && !containsAliasToken(tokens, "loop") {
		tokens = append(tokens, "loop")
	}
	return "ds_" + strings.Join(moveAudioVerbLast(tokens), "_")
}

func unresolvedSampleAlias(instanceID uint32, references []SampleReference) (string, string) {
	if len(references) == 0 {
		return fmt.Sprintf("ds_sample_%08x", instanceID), "unreferenced sample identity"
	}
	eventInstances := make(map[uint32]bool)
	isLooped := false
	for _, reference := range references {
		eventInstances[reference.EventInstance] = true
		isLooped = isLooped || reference.IsLooped
	}
	primaryEvent := references[0].EventInstance
	tokens := []string{"event", fmt.Sprintf("%08x", primaryEvent)}
	if len(eventInstances) > 1 {
		tokens = append(tokens, fmt.Sprintf("shared_%02d", len(eventInstances)))
	}
	if references[0].ItemCount > 1 {
		tokens = append(tokens, fmt.Sprintf("variant_%02d", references[0].ItemIndex+1))
	}
	if isLooped {
		tokens = append(tokens, "loop")
	}
	return "ds_" + strings.Join(tokens, "_"), "AudioProps event identity"
}

func audioEventContext(document *prop.Document, documentsByInstance map[uint32]*prop.Document, names map[uint32]string) ([]string, bool) {
	contextNames := make([]string, 0, 4)
	seenNames := make(map[string]bool)
	isLooped := false
	visitedInstances := make(map[uint32]bool)
	var visit func(*prop.Document)
	visit = func(current *prop.Document) {
		for _, property := range current.Properties {
			propertyName, isFound := prop.Name(property.ID)
			if isFound && propertyName == "islooped" && property.Type == prop.TypeBool && len(property.Items) != 0 && len(property.Items[0]) != 0 {
				isLooped = isLooped || property.Items[0][0] != 0
			}
			if property.Type != prop.TypeKey || !isFound || propertyName != "parent" {
				continue
			}
			for _, item := range property.Items {
				if len(item) < 4 {
					continue
				}
				instanceID := binary.LittleEndian.Uint32(item[:4])
				contextName := strings.TrimSuffix(names[instanceID], "~")
				if isUsefulAudioContextName(contextName) && !seenNames[contextName] {
					contextNames = append(contextNames, contextName)
					seenNames[contextName] = true
				}
				parentDocument := documentsByInstance[instanceID]
				if parentDocument == nil || visitedInstances[instanceID] {
					continue
				}
				visitedInstances[instanceID] = true
				visit(parentDocument)
			}
		}
	}
	visit(document)
	sort.Strings(contextNames)
	return contextNames, isLooped
}

func isUsefulAudioContextName(name string) bool {
	if name == "" || strings.HasPrefix(name, "@") {
		return false
	}
	normalizedName := strings.TrimLeft(strings.ToLower(name), "_")
	return !strings.HasPrefix(normalizedName, "footsteps_parent")
}

func preferredSampleAlias(references []SampleReference) (string, bool) {
	eventTokenSets := make([][]string, 0, len(references))
	contextTokenSets := make([][]string, 0, len(references))
	usageTokenSets := make([][]string, 0, len(references))
	isLooped := false
	for _, reference := range references {
		isLooped = isLooped || reference.IsLooped
		if reference.EventName != "" && !strings.HasPrefix(reference.EventName, "@") {
			tokens := audioAliasTokens(reference.EventName)
			if len(tokens) != 0 {
				eventTokenSets = append(eventTokenSets, tokens)
			}
			continue
		}
		for _, contextName := range reference.ContextNames {
			tokens := audioAliasTokens(contextName)
			if len(tokens) != 0 {
				contextTokenSets = append(contextTokenSets, tokens)
			}
		}
		for _, usage := range reference.Usages {
			if !isUsefulUsageContext(usage) {
				continue
			}
			tokens := audioAliasTokens(usage.Context)
			if len(tokens) != 0 {
				usageTokenSets = append(usageTokenSets, tokens)
			}
		}
	}
	tokenSets := eventTokenSets
	isSharedContext := len(eventTokenSets) != 0
	isResourceReference := false
	if len(tokenSets) == 0 {
		tokenSets = contextTokenSets
	}
	if len(tokenSets) == 0 {
		tokenSets = usageTokenSets
		isSharedContext = true
		isResourceReference = len(usageTokenSets) != 0
	}
	if len(tokenSets) == 0 {
		return "", false
	}
	tokens := tokenSets[0]
	if isSharedContext && len(tokenSets) > 1 {
		tokens = sharedAliasTokens(tokenSets)
		if !isUsefulSharedAlias(tokens) {
			sort.Slice(tokenSets, func(first, second int) bool {
				if len(tokenSets[first]) != len(tokenSets[second]) {
					return len(tokenSets[first]) > len(tokenSets[second])
				}
				return strings.Join(tokenSets[first], "_") < strings.Join(tokenSets[second], "_")
			})
			tokens = tokenSets[0]
		}
	} else if len(tokenSets) > 1 {
		sort.Slice(tokenSets, func(first, second int) bool {
			if len(tokenSets[first]) != len(tokenSets[second]) {
				return len(tokenSets[first]) > len(tokenSets[second])
			}
			return strings.Join(tokenSets[first], "_") < strings.Join(tokenSets[second], "_")
		})
		tokens = tokenSets[0]
	}
	if isLooped && !containsAliasToken(tokens, "loop") {
		tokens = append(tokens, "loop")
	}
	if references[0].ItemCount > 1 {
		variant := fmt.Sprintf("variant_%02d", references[0].ItemIndex+1)
		tokens = insertBeforeAudioVerb(tokens, variant)
	}
	return "ds_" + strings.Join(moveAudioVerbLast(tokens), "_"), isResourceReference
}

func isUsefulUsageContext(usage SampleUsage) bool {
	if usage.ReferenceKind != "binary event id" || usage.Context == "" {
		return false
	}
	switch strings.ToLower(usage.Context) {
	case "base", "editors", "games":
		return false
	default:
		return true
	}
}

var ignoredAudioAliasTokens = map[string]bool{
	"aggregation": true,
	"all":         true,
	"audio":       true,
	"base":        true,
	"event":       true,
	"sample":      true,
	"samples":     true,
	"sfx":         true,
	"sound":       true,
	"sounds":      true,
	"system":      true,
}

func containsAliasToken(tokens []string, expected string) bool {
	for _, token := range tokens {
		if token == expected {
			return true
		}
	}
	return false
}

var audioAliasVerbs = map[string]bool{
	"attack":  true,
	"cast":    true,
	"charge":  true,
	"decay":   true,
	"die":     true,
	"end":     true,
	"explode": true,
	"fade":    true,
	"fire":    true,
	"hit":     true,
	"impact":  true,
	"loop":    true,
	"release": true,
	"start":   true,
	"sustain": true,
	"whoosh":  true,
}

func audioAliasTokens(name string) []string {
	words := make([]string, 0, 8)
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		lowerWord := strings.ToLower(word.String())
		word.Reset()
		if !ignoredAudioAliasTokens[lowerWord] {
			words = append(words, lowerWord)
		}
	}
	priorLower := false
	for _, character := range name {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			priorLower = false
			continue
		}
		isUpper := unicode.IsUpper(character)
		if isUpper && priorLower {
			flush()
		}
		word.WriteRune(character)
		priorLower = unicode.IsLower(character) || unicode.IsDigit(character)
	}
	flush()
	return words
}

func sharedAliasTokens(tokenSets [][]string) []string {
	counts := make(map[string]int)
	for _, tokens := range tokenSets {
		seenTokens := make(map[string]bool)
		for _, token := range tokens {
			if !seenTokens[token] {
				counts[token]++
				seenTokens[token] = true
			}
		}
	}
	sharedTokens := make([]string, 0, len(tokenSets[0]))
	for _, token := range tokenSets[0] {
		if counts[token] == len(tokenSets) {
			sharedTokens = append(sharedTokens, token)
		}
	}
	return sharedTokens
}

func isUsefulSharedAlias(tokens []string) bool {
	for _, token := range tokens {
		if len(token) >= 3 && token != "variant" && token != "shared" {
			return true
		}
	}
	return false
}

func insertBeforeAudioVerb(tokens []string, token string) []string {
	for index, candidate := range tokens {
		if audioAliasVerbs[candidate] {
			result := append([]string(nil), tokens[:index]...)
			result = append(result, token)
			return append(result, tokens[index:]...)
		}
	}
	return append(tokens, token)
}

func moveAudioVerbLast(tokens []string) []string {
	verbIndex := -1
	for index, token := range tokens {
		if audioAliasVerbs[token] {
			verbIndex = index
		}
	}
	if verbIndex < 0 || verbIndex == len(tokens)-1 {
		return tokens
	}
	verb := tokens[verbIndex]
	result := append([]string(nil), tokens[:verbIndex]...)
	result = append(result, tokens[verbIndex+1:]...)
	return append(result, verb)
}

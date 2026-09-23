package ds

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/darkspinnet/darkspin/content/animation"
	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/movie"
	"github.com/darkspinnet/darkspin/content/render/prop"
	"github.com/darkspinnet/darkspin/content/render/rw4"
	"github.com/darkspinnet/darkspin/content/scaleform"
)

const gmdlResourceType = 0x01C135DA

type resourceAlias struct {
	name     string
	category string
	lod      string
}

type resourcePairKey struct {
	family   string
	group    uint32
	instance uint64
}

func packageResourcePaths(resources []dbpf.Resource, names map[uint32]string) []string {
	aliases := packageResourceAliases(resources, names)
	paths := make([]string, len(resources))
	usedPaths := make(map[string]bool, len(resources))
	aliasOrdinalsByPath := make(map[string]int)
	typeCountsByPair := make(map[resourcePairKey]map[uint32]int)
	for _, resource := range resources {
		pairKey, isPairable := resourcePair(resource.Entry)
		if !isPairable {
			continue
		}
		if typeCountsByPair[pairKey] == nil {
			typeCountsByPair[pairKey] = make(map[uint32]int)
		}
		typeCountsByPair[pairKey][resource.Entry.Type]++
	}
	pathsByPair := make(map[resourcePairKey]string)
	for ordinal, resource := range resources {
		alias := aliases[ordinal]
		kind := resourceKind(resource.Entry.Type)
		isAudioAsset := resource.Entry.Type == prop.AudioResourceType || audio.IsStreamType(resource.Entry.Type) || audio.IsPatchType(resource.Entry.Type)
		isAudioProperty := resource.Entry.Type == prop.SubmixResourceType || resource.Entry.Type == prop.ModeResourceType || resource.Entry.Type == prop.ChildrenResourceType
		isMovieAsset := resource.Entry.Type == movie.ResourceType
		isAnimationAsset := resource.Entry.Type == animation.ResourceType
		isScaleformAsset := scaleform.IsResourceType(resource.Entry.Type)
		if isAudioAsset || isAudioProperty {
			alias.category = audioResourceCategory(resource.Entry.Type, alias.name)
		}
		if isMovieAsset {
			alias.category = "movie"
		}
		if isAnimationAsset {
			alias.category = "animation"
		}
		if isScaleformAsset {
			alias.category = filepath.Join("ui", "scaleform")
		}
		pairKey, isPairable := resourcePair(resource.Entry)
		isPaired := isPairable && isCompleteResourcePair(typeCountsByPair[pairKey])
		if isPaired {
			pairPath := pathsByPair[pairKey]
			if pairPath != "" {
				paths[ordinal] = pairPath
				continue
			}
		}
		identity := resourceIdentity(ordinal, resource.Entry)
		if alias.name == "" {
			if isAudioAsset || isAudioProperty || isMovieAsset || isScaleformAsset || isPaired {
				identity = resourceInstanceName(resource.Entry.Instance)
			}
			alias.name = identity
		}
		name := safeResourceName(alias.name)
		if alias.lod != "" {
			name += "_" + alias.lod
		}
		if strings.EqualFold(name+".dse", ManifestName) {
			name += fmt.Sprintf("__%08x_%08x_%016x", resource.Entry.Type, resource.Entry.Group, resource.Entry.Instance)
		}
		path := filepath.ToSlash(filepath.Join(alias.category, kind, name+".dse"))
		if resource.Entry.Type == prop.AudioResourceType || audio.IsStreamType(resource.Entry.Type) || audio.IsPatchType(resource.Entry.Type) || isMovieAsset || isAnimationAsset || isScaleformAsset || isPaired && pairKey.family == "render" {
			path = filepath.ToSlash(filepath.Join(alias.category, name+".dse"))
		}
		pathKey := strings.ToLower(path)
		if usedPaths[pathKey] {
			if audio.IsSampleAlias(alias.name) {
				basePathKey := pathKey
				aliasOrdinal := aliasOrdinalsByPath[basePathKey] + 2
				for {
					candidateName := fmt.Sprintf("%s_%02d", name, aliasOrdinal)
					candidatePath := filepath.ToSlash(filepath.Join(alias.category, candidateName+".dse"))
					candidatePathKey := strings.ToLower(candidatePath)
					if !usedPaths[candidatePathKey] {
						name = candidateName
						path = candidatePath
						pathKey = candidatePathKey
						aliasOrdinalsByPath[basePathKey] = aliasOrdinal - 1
						break
					}
					aliasOrdinal++
				}
			} else {
				name += fmt.Sprintf("__%08x_%08x_%016x", resource.Entry.Type, resource.Entry.Group, resource.Entry.Instance)
				path = filepath.ToSlash(filepath.Join(alias.category, kind, name+".dse"))
				if resource.Entry.Type == prop.AudioResourceType || audio.IsStreamType(resource.Entry.Type) || audio.IsPatchType(resource.Entry.Type) || isMovieAsset || isAnimationAsset || isScaleformAsset || isPaired && pairKey.family == "render" {
					path = filepath.ToSlash(filepath.Join(alias.category, name+".dse"))
				}
				pathKey = strings.ToLower(path)
			}
		}
		usedPaths[pathKey] = true
		paths[ordinal] = path
		if isPaired {
			pathsByPair[pairKey] = path
		}
	}
	return paths
}

func packageAnimationNames(reader *dbpf.Reader, resources []dbpf.Resource, names map[uint32]string) (map[uint32]string, error) {
	animationNames := make(map[uint32]string, len(names))
	for instanceID, name := range names {
		animationNames[instanceID] = name
	}
	for ordinal, resource := range resources {
		if resource.Entry.Type != animation.ResourceType || resource.Entry.Instance > uint64(^uint32(0)) {
			continue
		}
		instanceID := uint32(resource.Entry.Instance)
		if animationNames[instanceID] != "" {
			continue
		}
		payloadReader, err := reader.Open(resource.Entry)
		if err != nil {
			return nil, fmt.Errorf("animationOpen[%d]: %w", ordinal, err)
		}
		payload, err := io.ReadAll(payloadReader)
		if err != nil {
			return nil, fmt.Errorf("animationRead[%d]: %w", ordinal, err)
		}
		document, err := animation.Decode(payload)
		if err != nil {
			return nil, fmt.Errorf("animationDecode[%d]: %w", ordinal, err)
		}
		source := filepath.ToSlash(document.Source)
		name := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
		if name != "" {
			animationNames[instanceID] = name
		}
	}
	return animationNames, nil
}

func resourceInstanceName(instance uint64) string {
	if instance <= uint64(^uint32(0)) {
		return fmt.Sprintf("%08x", instance)
	}
	return fmt.Sprintf("%016x", instance)
}

// packageResourceLinkNames supplements recovered authored names with stable
// DS-root links for uniquely identifiable property-list resources. Authored
// names always win; the path form exists only when DBPF discarded the name.
func packageResourceLinkNames(resources []dbpf.Resource, names map[uint32]string, linkRoot string) map[uint32]string {
	linkedNames := make(map[uint32]string, len(names)+len(resources))
	for instanceID, name := range names {
		linkedNames[instanceID] = name
	}
	counts := make(map[uint32]int)
	paths := make(map[uint32]string)
	for _, resource := range resources {
		if !prop.IsResourceType(resource.Entry.Type) || resource.Entry.Instance > uint64(^uint32(0)) {
			continue
		}
		instanceID := uint32(resource.Entry.Instance)
		counts[instanceID]++
		link := strings.TrimRight(linkRoot, "/") + "/" + filepath.ToSlash(resource.PayloadPath)
		_, err := linkedResourceInstance(link)
		if err == nil {
			paths[instanceID] = link
		}
	}
	for instanceID, link := range paths {
		if linkedNames[instanceID] == "" && counts[instanceID] == 1 {
			linkedNames[instanceID] = link
		}
	}
	return linkedNames
}

// PackageResourceLinks returns stable links to uniquely identified resources
// under another root in the same DS bundle.
func PackageResourceLinks(sourcePath, linkRoot string, names map[uint32]string, resourceTypes ...uint32) (map[uint32]string, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return nil, fmt.Errorf("packageStat: %w", err)
	}
	reader, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("packageRead: %w", err)
	}
	manifest := reader.ArchiveManifest()
	if manifest == nil {
		return nil, fmt.Errorf("manifestMissing")
	}
	paths := packageResourcePaths(manifest.Resources, names)
	for ordinal := range manifest.Resources {
		manifest.Resources[ordinal].PayloadPath = paths[ordinal]
	}
	return packageResourceLinks(manifest.Resources, linkRoot, resourceTypes...), nil
}

func packageResourceLinks(resources []dbpf.Resource, linkRoot string, resourceTypes ...uint32) map[uint32]string {
	allowedTypes := make(map[uint32]bool, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		allowedTypes[resourceType] = true
	}
	linksByInstance := make(map[uint32]map[string]bool)
	for _, resource := range resources {
		if len(allowedTypes) != 0 && !allowedTypes[resource.Entry.Type] {
			continue
		}
		if resource.Entry.Instance > uint64(^uint32(0)) {
			continue
		}
		instanceID := uint32(resource.Entry.Instance)
		link := strings.TrimRight(linkRoot, "/") + "/" + filepath.ToSlash(resource.PayloadPath)
		_, linkErr := linkedResourceInstance(link)
		if linkErr != nil {
			continue
		}
		if linksByInstance[instanceID] == nil {
			linksByInstance[instanceID] = make(map[string]bool)
		}
		linksByInstance[instanceID][link] = true
	}
	links := make(map[uint32]string)
	for instanceID, candidates := range linksByInstance {
		if len(candidates) != 1 {
			continue
		}
		for link := range candidates {
			links[instanceID] = link
		}
	}
	return links
}

func isCompleteResourcePair(typeCounts map[uint32]int) bool {
	if len(typeCounts) != 2 {
		return false
	}
	for _, count := range typeCounts {
		if count != 1 {
			return false
		}
	}
	return true
}

func resourcePair(entry dbpf.Entry) (resourcePairKey, bool) {
	family := ""
	switch entry.Type {
	case audio.SNRResourceType, audio.SNSResourceType:
		family = "audio"
	case audio.PDResourceType, audio.PDRResourceType:
		family = "patch"
	case gmdlResourceType, rw4.ResourceType:
		family = "render"
	default:
		return resourcePairKey{}, false
	}
	return resourcePairKey{family: family, group: entry.Group, instance: entry.Instance}, true
}

func areResourceCompanions(first, second dbpf.Entry) bool {
	isFirstAudioDefinition := audio.IsStreamType(first.Type) || first.Type == prop.AudioResourceType
	isSecondAudioDefinition := audio.IsStreamType(second.Type) || second.Type == prop.AudioResourceType
	if isFirstAudioDefinition && isSecondAudioDefinition {
		return true
	}
	firstKey, isFirstPairable := resourcePair(first)
	secondKey, isSecondPairable := resourcePair(second)
	if !isFirstPairable || !isSecondPairable || firstKey != secondKey || first.Type == second.Type {
		return false
	}
	return true
}

func packageResourceAliases(resources []dbpf.Resource, names map[uint32]string) []resourceAlias {
	aliases := make([]resourceAlias, len(resources))
	groupsByName := make(map[string]map[uint32]bool)
	for ordinal, resource := range resources {
		name := resourceDisplayName(names[uint32(resource.Entry.Instance)])
		aliases[ordinal] = resourceAlias{name: name, category: resourceCategory(name)}
		if name == "" {
			continue
		}
		nameKey := strings.ToLower(name)
		if groupsByName[nameKey] == nil {
			groupsByName[nameKey] = make(map[uint32]bool)
		}
		groupsByName[nameKey][resource.Entry.Group] = true
	}
	for ordinal, resource := range resources {
		nameKey := strings.ToLower(aliases[ordinal].name)
		if resource.Entry.Group == 0x40606100 {
			aliases[ordinal].lod = "LOD1"
			continue
		}
		if resource.Entry.Group == 0x40606000 && groupsByName[nameKey][0x40606100] {
			aliases[ordinal].lod = "LOD0"
		}
	}
	return aliases
}

func resourceKind(typeCode uint32) string {
	switch typeCode {
	case rw4.ResourceType:
		return "rw4"
	case prop.ResourceType:
		return "property"
	case prop.AudioResourceType:
		return "event"
	case prop.SubmixResourceType:
		return "submix"
	case prop.ModeResourceType:
		return "mode"
	case prop.ChildrenResourceType:
		return "children"
	case audio.SNRResourceType:
		return "snr"
	case audio.SNSResourceType:
		return "sns"
	case audio.PDResourceType:
		return "pd"
	case audio.PDRResourceType:
		return "pdr"
	case gmdlResourceType:
		return "gmdl"
	case movie.ResourceType:
		return "movie"
	case scaleform.MovieResourceType:
		return "gfx"
	case scaleform.ImageResourceType, scaleform.ImageVariantResourceType:
		return "image"
	default:
		return fmt.Sprintf("type_%08x", typeCode)
	}
}

func audioCategory(name string) string {
	lowerName := strings.ToLower(name)
	categoryName := strings.ToLower(audio.StripSampleAliasSuffix(name))
	contextParts := strings.Split(categoryName, "_")
	if len(contextParts) >= 2 && contextParts[0] == "event" && isHexIdentity(contextParts[1]) {
		return filepath.Join("audio", "event", contextParts[1])
	}
	switch {
	case strings.HasPrefix(categoryName, "music_") || strings.Contains(categoryName, "_music_"):
		return filepath.Join("audio", "music")
	case strings.HasPrefix(categoryName, "vo_") || strings.Contains(categoryName, "_vo_") || strings.Contains(categoryName, "voice"):
		return filepath.Join("audio", "voice")
	case strings.HasPrefix(categoryName, "amb_") || strings.HasPrefix(categoryName, "ambience_"):
		return filepath.Join("audio", "ambience")
	case strings.HasPrefix(categoryName, "ui_") || strings.HasPrefix(categoryName, "editor_"):
		return filepath.Join("audio", "ui")
	case name != "":
		category := filepath.Join("audio", "effect")
		family := audioFamily(lowerName)
		if family != "" {
			category = filepath.Join(category, family)
		}
		return category
	default:
		return filepath.Join("audio", "unresolved")
	}
}

func audioResourceCategory(typeCode uint32, name string) string {
	if name != "" {
		return audioCategory(name)
	}
	switch typeCode {
	case prop.AudioResourceType:
		return filepath.Join("audio", "event")
	case audio.SNRResourceType, audio.SNSResourceType:
		return filepath.Join("audio", "sample")
	case audio.PDResourceType, audio.PDRResourceType:
		return filepath.Join("audio", "patch")
	case prop.SubmixResourceType:
		return filepath.Join("audio", "submix")
	case prop.ModeResourceType:
		return filepath.Join("audio", "mode")
	case prop.ChildrenResourceType:
		return filepath.Join("audio", "children")
	default:
		return filepath.Join("audio", "resource")
	}
}

func isHexIdentity(name string) bool {
	if len(name) != 8 {
		return false
	}
	for _, character := range name {
		if character >= '0' && character <= '9' || character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}

func audioFamily(name string) string {
	name = strings.ToLower(audio.StripSampleAliasSuffix(name))
	separator := strings.IndexByte(name, '_')
	if separator <= 0 {
		return ""
	}
	return safeResourceName(trimAudioFamilyIndex(name[:separator]))
}

func trimAudioFamilyIndex(family string) string {
	digitStart := len(family)
	for digitStart > 0 && family[digitStart-1] >= '0' && family[digitStart-1] <= '9' {
		digitStart--
	}
	if digitStart == 0 || digitStart == len(family) {
		return family
	}
	baseFamily := family[:digitStart]
	if !strings.HasSuffix(baseFamily, "loop") {
		return family
	}
	return baseFamily
}

func resourceDefinitionIdentity(payloadPath string) string {
	return strings.TrimSuffix(filepath.Base(filepath.FromSlash(payloadPath)), filepath.Ext(payloadPath))
}

func resourceDisplayName(name string) string {
	if strings.HasPrefix(name, "@") {
		return ""
	}
	return name
}

func resourceCategory(name string) string {
	parts := strings.Split(strings.ToLower(name), "_")
	if len(parts) == 0 || parts[0] == "" {
		return "unresolved"
	}
	switch parts[0] {
	case "ce":
		return filepath.Join("creature", resourcePartCategory(parts))
	case "cl":
		return filepath.Join("cell", resourcePartCategory(parts))
	case "ta":
		return filepath.Join("tribal", resourceRole(parts))
	case "ca":
		return filepath.Join("civilization", resourceRole(parts))
	case "sa", "space":
		return filepath.Join("space", resourceRole(parts))
	case "ap1":
		return "adventure"
	case "ep1":
		return "expansion"
	case "ui", "loading", "planet", "paint":
		return "ui"
	case "feature", "flower", "frost", "ice":
		return "world"
	case "arm01", "arm02", "arm03", "arm04", "arm05", "arm06", "arm07", "arm08", "arm09", "arm10", "arm11", "arm12",
		"leg01", "leg02", "leg03", "leg04", "leg05", "leg06", "leg07", "leg08", "leg09", "leg10", "leg11", "leg12", "axes":
		return "rig"
	default:
		if strings.Contains(strings.ToLower(name), "background") {
			return "ui"
		}
		return "misc"
	}
}

func resourcePartCategory(parts []string) string {
	if len(parts) < 2 {
		return "misc"
	}
	switch parts[1] {
	case "weapon":
		return "weapons"
	case "grasper", "graspertech":
		return "graspers"
	case "sense", "senseeye", "sensetech":
		return "senses"
	case "movement", "movementtech":
		return "movement"
	case "mouth", "mouthtech":
		return "mouths"
	case "details", "detail":
		return "details"
	case "limb", "leg", "vertebra", "vertebra-hull":
		return "limbs"
	case "wing":
		return "wings"
	case "chest", "helmet", "shoulder":
		return "body"
	case "cell":
		return "cell-parts"
	default:
		return safeResourceName(parts[1])
	}
}

func resourceRole(parts []string) string {
	if len(parts) < 2 {
		return "misc"
	}
	return safeResourceName(parts[1])
}

func safeResourceName(name string) string {
	name = strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' || character == '.' || character == '~' {
			return character
		}
		return '_'
	}, name)
	name = strings.Trim(name, ".")
	if len(name) > 96 {
		name = name[:96]
	}
	if name == "" {
		return "unnamed"
	}
	return name
}

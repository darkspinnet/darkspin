package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	server "github.com/darkspinnet/darkspin/server/runtime"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	"github.com/deepteams/webp"
	"github.com/spf13/cobra"
)

const defaultCampaignMapSize = 512

type campaignMapMarker struct {
	Category string  `json:"category"`
	Label    string  `json:"label,omitempty"`
	Name     string  `json:"name"`
	NounName string  `json:"noun_name,omitempty"`
	Section  uint32  `json:"section,omitempty"`
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
	Z        float32 `json:"z"`
}

type campaignMapSection struct {
	ComponentID uint32             `json:"component_id"`
	Image       string             `json:"image"`
	Metadata    string             `json:"metadata"`
	Map         navigation.MapInfo `json:"map"`
	MarkerCount int                `json:"marker_count"`
}

type campaignMapMetadata struct {
	Selection string               `json:"selection"`
	Level     string               `json:"level"`
	Map       navigation.MapInfo   `json:"map"`
	Legend    map[string]string    `json:"legend"`
	Labels    map[string]string    `json:"labels"`
	Notes     []string             `json:"enemy_class_notes"`
	Sections  []campaignMapSection `json:"sections,omitempty"`
	Markers   []campaignMapMarker  `json:"markers"`
}

func newMapCommand() *cobra.Command {
	var configPath string
	var outputPath string
	var metadataPath string
	var planLayer uint8
	var size int
	command := &cobra.Command{
		Use:   "map [campaign-or-level]",
		Short: "List or render prepared navigation maps as WebP",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if size < 1 {
				return errors.New("map size must be positive")
			}
			resolvedConfigPath, err := filepath.Abs(configPath)
			if err != nil {
				return fmt.Errorf("mapConfigPath: %w", err)
			}
			databasePath := filepath.Join(
				filepath.Dir(resolvedConfigPath), server.DarkspinDirectory,
				server.CacheDirectory, server.ContentDatabaseFilename,
			)
			_, err = os.Stat(databasePath)
			if err != nil {
				return fmt.Errorf("mapDatabaseStat: %w", err)
			}
			store, err := contentsqlite.New(command.Context(), databasePath)
			if err != nil {
				return fmt.Errorf("mapStoreOpen: %w", err)
			}
			defer store.Close()
			if len(arguments) == 0 {
				err = writeMapSelections(command.Context(), command.OutOrStdout(), store)
				if err != nil {
					return fmt.Errorf("mapList: %w", err)
				}
				return nil
			}
			selectionName := strings.TrimSpace(arguments[0])
			levelName, err := mapSelectionLevel(command.Context(), store, selectionName)
			if err != nil {
				return fmt.Errorf("mapLevel: %w", err)
			}
			data, err := store.LevelNavigation(command.Context(), levelName)
			if err != nil {
				return fmt.Errorf("mapNavigationRead: %w", err)
			}
			mesh, err := navigation.ParseBFX(data)
			if err != nil {
				return fmt.Errorf("mapNavigationParse: %w", err)
			}
			_, mapInfo, err := mesh.RenderTopDownMap(planLayer, size, size, 32)
			if err != nil {
				return fmt.Errorf("mapRender: %w", err)
			}
			director, err := store.LevelDirector(command.Context(), levelName)
			if err != nil {
				return fmt.Errorf("mapDirectorRead: %w", err)
			}
			markers := campaignMapMarkers(selectionName, director)
			markers = appendCampaignMapTeleportRoutes(levelName, markers)
			assignCampaignMapMarkerSections(mesh, planLayer, markers)
			if outputPath == "" {
				outputPath = campaignMapOutputPath(selectionName)
			}
			if metadataPath == "" {
				metadataPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".json"
			}
			err = os.MkdirAll(filepath.Dir(metadataPath), 0o755)
			if err != nil {
				return fmt.Errorf("mapMetadataDirectory: %w", err)
			}
			sections, err := writeCampaignMapSections(
				outputPath, selectionName, levelName, mesh, planLayer, size, markers,
			)
			if err != nil {
				return fmt.Errorf("mapSections: %w", err)
			}
			metadata := campaignMapMetadata{
				Selection: selectionName, Level: levelName, Map: mapInfo,
				Legend: campaignMapLegend(), Labels: campaignMapLabelLegend(),
				Notes:    campaignMapEnemyClassNotes(),
				Sections: sections, Markers: markers,
			}
			metadataData, err := marshalCampaignMapMetadata(metadata)
			if err != nil {
				return fmt.Errorf("mapMetadataMarshal: %w", err)
			}
			err = os.WriteFile(metadataPath, metadataData, 0o644)
			if err != nil {
				return fmt.Errorf("mapMetadataWrite: %w", err)
			}
			_, err = fmt.Fprintf(
				command.OutOrStdout(),
				"Rendered %s (%s) as %d labeled section maps with %d markers\n",
				selectionName, levelName, len(sections), len(markers),
			)
			if err != nil {
				return fmt.Errorf("mapOutput: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&configPath, "config", game.DefaultConfigFilename, "path to darkspin.toml")
	command.Flags().StringVarP(
		&outputPath, "output", "o", "",
		"section output base; defaults to map-<selection>.webp",
	)
	command.Flags().StringVar(&metadataPath, "metadata", "", "JSON coordinate metadata path")
	command.Flags().Uint8Var(&planLayer, "layer", 0, "baked footprint layer")
	command.Flags().IntVar(
		&size, "size", defaultCampaignMapSize,
		"square output size in pixels",
	)
	return command
}

func campaignMapOutputPath(selectionName string) string {
	return "map-" + strings.ToLower(selectionName) + ".webp"
}

func mapSelectionLevel(
	ctx context.Context, store *contentsqlite.Store, selectionName string,
) (string, error) {
	if strings.EqualFold(selectionName, "tutorial") {
		return store.ResolveLevelNavigationName(ctx, game.TutorialLevel)
	}
	part := strings.Split(selectionName, "-")
	if len(part) == 2 {
		_, chapterErr := strconv.Atoi(part[0])
		_, missionErr := strconv.Atoi(part[1])
		if chapterErr == nil && missionErr == nil {
			return campaignMapLevel(ctx, store, selectionName)
		}
	}
	return store.ResolveLevelNavigationName(ctx, selectionName)
}

func writeMapSelections(
	ctx context.Context, output io.Writer, store *contentsqlite.Store,
) error {
	references, err := store.ChainLevelReferences(ctx)
	if err != nil {
		return fmt.Errorf("mapListCampaigns: %w", err)
	}
	_, err = fmt.Fprintln(output, "Campaign maps:")
	if err != nil {
		return fmt.Errorf("mapListCampaignHeader: %w", err)
	}
	campaignLevel := make(map[string]struct{})
	for referenceIndex := 0; referenceIndex < len(references); referenceIndex += 4 {
		chapter := referenceIndex/4 + 1
		line := fmt.Sprintf("  %d:", chapter)
		for missionOffset := 0; missionOffset < 4 && referenceIndex+missionOffset < len(references); missionOffset++ {
			levelName := strings.TrimSuffix(references[referenceIndex+missionOffset], ".Level")
			campaignLevel[strings.ToLower(levelName)] = struct{}{}
			line += fmt.Sprintf(" %d-%d=%s", chapter, missionOffset+1, levelName)
		}
		_, err = fmt.Fprintln(output, line)
		if err != nil {
			return fmt.Errorf("mapListCampaign[%d]: %w", chapter, err)
		}
	}
	names, err := store.LevelNavigationNames(ctx)
	if err != nil {
		return fmt.Errorf("mapListLevels: %w", err)
	}
	_, err = fmt.Fprintln(output, "Misc maps:")
	if err != nil {
		return fmt.Errorf("mapListMiscHeader: %w", err)
	}
	for _, levelName := range names {
		if _, isCampaign := campaignLevel[strings.ToLower(levelName)]; isCampaign {
			continue
		}
		selectionName := levelName
		if strings.EqualFold(levelName, "Game_Tutorial_cryos_1") {
			selectionName = "tutorial"
		}
		_, err = fmt.Fprintf(output, "  %s=%s\n", selectionName, levelName)
		if err != nil {
			return fmt.Errorf("mapListMisc[%s]: %w", levelName, err)
		}
	}
	return nil
}

func campaignMapLevel(
	ctx context.Context, store *contentsqlite.Store, campaignName string,
) (string, error) {
	part := strings.Split(campaignName, "-")
	if len(part) != 2 {
		return "", errors.New("campaign must use chapter-mission syntax, such as 1-1")
	}
	chapter, err := strconv.Atoi(part[0])
	if err != nil {
		return "", fmt.Errorf("campaignChapter: %w", err)
	}
	mission, err := strconv.Atoi(part[1])
	if err != nil {
		return "", fmt.Errorf("campaignMission: %w", err)
	}
	if chapter <= 0 || mission <= 0 || mission > 4 {
		return "", errors.New("campaign selection out of range")
	}
	references, err := store.ChainLevelReferences(ctx)
	if err != nil {
		return "", fmt.Errorf("campaignReferences: %w", err)
	}
	selection := (chapter-1)*4 + mission
	if selection <= 0 || selection > len(references) {
		return "", errors.New("campaign selection unavailable")
	}
	return strings.TrimSuffix(references[selection-1], ".Level"), nil
}

func campaignMapMarkers(
	selectionName string, director contentsqlite.LevelDirector,
) []campaignMapMarker {
	markers := make([]campaignMapMarker, 0)
	markerIDSet := make(map[uint32]struct{})
	if len(director.EntryPositions) > 0 {
		entry := director.EntryPositions[0]
		markers = append(markers, campaignMapMarker{
			Category: "entry", Label: "P", Name: "player_start",
			X: entry[0], Y: entry[1], Z: entry[2],
		})
	}
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			markerIDSet[marker.MarkerID] = struct{}{}
			category := campaignMapMarkerCategory(
				markerSet.Name, marker.Name, marker.NounName,
			)
			markers = append(markers, campaignMapMarker{
				Category: category,
				Label: campaignMapMarkerLabel(
					selectionName, category, markerSet.Name, marker.Name, marker.NounName,
				),
				Name: marker.Name, NounName: marker.NounName,
				X: marker.PositionX, Y: marker.PositionY, Z: marker.PositionZ,
			})
		}
		for _, trigger := range markerSet.Triggers {
			markers = append(markers, campaignMapMarker{
				Category: "trigger", Name: trigger.Name, NounName: trigger.NounName,
				X: trigger.PositionX, Y: trigger.PositionY, Z: trigger.PositionZ,
			})
		}
	}
	for _, script := range director.Scripts {
		if _, isAdded := markerIDSet[script.MarkerID]; isAdded {
			continue
		}
		if !strings.Contains(strings.ToLower(script.NounName), "obelisk") {
			continue
		}
		markerIDSet[script.MarkerID] = struct{}{}
		markers = append(markers, campaignMapMarker{
			Category: "obelisk", Label: "O",
			Name: script.MarkerName, NounName: script.NounName,
			X: script.PositionX, Y: script.PositionY, Z: script.PositionZ,
		})
	}
	return markers
}

func appendCampaignMapTeleportRoutes(
	levelName string, markers []campaignMapMarker,
) []campaignMapMarker {
	for routeIndex, route := range zonesecurity.Routes(levelName) {
		routeNumber := routeIndex + 1
		markers = append(markers,
			campaignMapMarker{
				Category: "teleporter-route", Label: fmt.Sprintf("T%dA", routeNumber),
				Name: fmt.Sprintf("teleporter_%d_source", routeNumber),
				X:    route.Source.X, Y: route.Source.Y, Z: route.Source.Z,
			},
			campaignMapMarker{
				Category: "teleporter-route", Label: fmt.Sprintf("T%dB", routeNumber),
				Name: fmt.Sprintf("teleporter_%d_destination", routeNumber),
				X:    route.Destination.X, Y: route.Destination.Y, Z: route.Destination.Z,
			},
		)
	}
	return markers
}

func assignCampaignMapMarkerSections(
	mesh *navigation.Mesh, planLayer uint8, markers []campaignMapMarker,
) {
	for markerIndex := range markers {
		projection, err := mesh.Project(navigation.Vec3{
			X: markers[markerIndex].X,
			Y: markers[markerIndex].Y,
			Z: markers[markerIndex].Z,
		}, navigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: 20,
		})
		if err == nil {
			markers[markerIndex].Section = projection.ComponentID
		}
	}
}

func writeCampaignMapSections(
	outputPath string, selectionName string, levelName string, mesh *navigation.Mesh,
	planLayer uint8, size int, markers []campaignMapMarker,
) ([]campaignMapSection, error) {
	rendered, err := mesh.RenderTopDownSections(planLayer, size, size, 32)
	if err != nil {
		return nil, fmt.Errorf("sectionRender: %w", err)
	}
	extension := filepath.Ext(outputPath)
	basePath := strings.TrimSuffix(outputPath, extension)
	sections := make([]campaignMapSection, 0, len(rendered))
	for _, renderedSection := range rendered {
		componentID := renderedSection.Info.ComponentID
		sectionPath := fmt.Sprintf("%s-section-%02d%s", basePath, componentID, extension)
		sectionMetadataPath := fmt.Sprintf("%s-section-%02d.json", basePath, componentID)
		sectionMarkers := campaignMapMarkersForSection(markers, componentID)
		drawCampaignMapMarkers(renderedSection.Image, renderedSection.Info, sectionMarkers)
		err = encodeCampaignMap(sectionPath, renderedSection.Image)
		if err != nil {
			return nil, fmt.Errorf("sectionEncode[%d]: %w", componentID, err)
		}
		sectionMetadata := campaignMapMetadata{
			Selection: selectionName, Level: levelName, Map: renderedSection.Info,
			Legend: campaignMapLegend(), Labels: campaignMapLabelLegend(),
			Notes:   campaignMapEnemyClassNotes(),
			Markers: sectionMarkers,
		}
		sectionData, marshalErr := marshalCampaignMapMetadata(sectionMetadata)
		if marshalErr != nil {
			return nil, fmt.Errorf("sectionMarshal[%d]: %w", componentID, marshalErr)
		}
		err = os.WriteFile(sectionMetadataPath, sectionData, 0o644)
		if err != nil {
			return nil, fmt.Errorf("sectionMetadataWrite[%d]: %w", componentID, err)
		}
		sections = append(sections, campaignMapSection{
			ComponentID: componentID,
			Image:       filepath.Base(sectionPath), Metadata: filepath.Base(sectionMetadataPath),
			Map: renderedSection.Info, MarkerCount: len(sectionMarkers),
		})
	}
	return sections, nil
}

func campaignMapMarkersForSection(
	markers []campaignMapMarker, componentID uint32,
) []campaignMapMarker {
	filtered := make([]campaignMapMarker, 0)
	for _, marker := range markers {
		if marker.Section == componentID {
			filtered = append(filtered, marker)
		}
	}
	return filtered
}

func encodeCampaignMap(outputPath string, minimap image.Image) error {
	contents, err := encodeCampaignWebP(minimap)
	if err != nil {
		return fmt.Errorf("mapWebP: %w", err)
	}
	err = os.WriteFile(outputPath, contents, 0o644)
	if err != nil {
		return fmt.Errorf("mapWrite: %w", err)
	}
	return nil
}

func encodeCampaignWebP(minimap image.Image) (contents []byte, err error) {
	defer func() {
		recovered := recover()
		if recovered != nil {
			contents = nil
			err = fmt.Errorf("encoderPanic: %v", recovered)
		}
	}()
	buffer := bytes.NewBuffer(nil)
	err = webp.Encode(buffer, minimap, &webp.EncoderOptions{
		Quality: 95,
		Method:  4,
	})
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return buffer.Bytes(), nil
}

func marshalCampaignMapMetadata(metadata campaignMapMetadata) ([]byte, error) {
	metadataData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("metadataMarshal: %w", err)
	}
	return append(metadataData, '\n'), nil
}

func campaignMapLegend() map[string]string {
	return map[string]string{
		"entry":            "#f5f8fa",
		"spawn":            "#da404b",
		"horde":            "#ff973d",
		"boss":             "#f5485c",
		"teleporter":       "#42d3ff",
		"teleporter-route": "#42d3ff",
		"obelisk":          "#c079ff",
		"stationary":       "#c079ff",
		"interactable":     "#c079ff",
		"trigger":          "#ffdf5c",
	}
}

func campaignMapLabelLegend() map[string]string {
	return map[string]string{
		"P":   "player starting area",
		"M":   "authored fixed enemy marker; not a monster tier",
		"A":   "authored fixed Mutation Agent actor",
		"W":   "Director Wanderer locus; usually minions, roster selected at runtime",
		"L":   "Director Spike locus; lieutenant-centered, not an Elite marker",
		"H":   "horde director anchor; roster selected at runtime",
		"C":   "Captain boss pit on a non-X-4 campaign slot",
		"D":   "Destructor boss pit on an X-4 campaign slot",
		"B":   "boss pit with unknown tier because campaign-slot context is unavailable",
		"O":   "obelisk",
		"S":   "destructible stationary object",
		"TnA": "teleporter n source",
		"TnB": "teleporter n destination",
	}
}

func campaignMapEnemyClassNotes() []string {
	return []string{
		"Static map markers describe encounter loci, not a guaranteed spawned enemy tier.",
		"Elite is a runtime stronger variant with additional affixes; no static E marker is emitted.",
		"Operatives are co-op-only runtime enemies and therefore have no static map marker.",
		"Mutation Agent is labeled A only for an exact authored actor; campaign horde anchors do not imply one.",
		"Captain boss pits occur outside X-4; X-4 boss pits contain Destructors.",
	}
}

func campaignMapMarkerCategory(markerSetName string, markerName string, nounName string) string {
	identity := strings.ToLower(markerSetName + " " + markerName + " " + nounName)
	switch {
	case strings.Contains(identity, "teleport"):
		return "teleporter"
	case strings.Contains(identity, "boss"):
		return "boss"
	case strings.Contains(identity, "obelisk"):
		return "obelisk"
	case strings.Contains(identity, "regulator") ||
		strings.Contains(identity, "instrument_scitech"):
		return "stationary"
	case strings.Contains(identity, "interact"):
		return "interactable"
	case strings.Contains(identity, "horde"):
		return "horde"
	default:
		return "spawn"
	}
}

func campaignMapMarkerLabel(
	selectionName string, category string, markerSetName string, markerName string,
	nounName string,
) string {
	identity := strings.ToLower(markerSetName + " " + markerName + " " + nounName)
	switch {
	case category == "boss":
		return campaignMapBossLabel(selectionName)
	case strings.Contains(strings.ToLower(nounName), "mutationagent.noun"):
		return "A"
	case strings.Contains(identity, "directorspike"):
		return "L"
	case strings.Contains(identity, "directorwanderer"):
		return "W"
	case category == "horde":
		return "H"
	case category == "obelisk":
		return "O"
	case category == "stationary":
		return "S"
	case category == "spawn":
		return "M"
	default:
		return ""
	}
}

func campaignMapBossLabel(selectionName string) string {
	part := strings.Split(strings.TrimSpace(selectionName), "-")
	if len(part) != 2 {
		return "B"
	}
	chapter, err := strconv.Atoi(part[0])
	if err != nil || chapter <= 0 {
		return "B"
	}
	mission, err := strconv.Atoi(part[1])
	if err != nil || mission <= 0 || mission > 4 {
		return "B"
	}
	if mission == 4 {
		return "D"
	}
	return "C"
}

func drawCampaignMapMarkers(
	minimap *image.RGBA, mapInfo navigation.MapInfo, markers []campaignMapMarker,
) {
	const layerCount = 4
	for layer := 0; layer < layerCount; layer++ {
		for _, marker := range markers {
			if campaignMapMarkerLayer(marker) != layer {
				continue
			}
			point, isVisible := mapInfo.ProjectTopDown(navigation.Vec3{
				X: marker.X, Y: marker.Y, Z: marker.Z,
			})
			if !isVisible {
				continue
			}
			fill := campaignMapMarkerColor(marker.Category, marker.Label)
			radius := 6
			if marker.Label != "" {
				radius = 9
			}
			if marker.Label == "W" || marker.Label == "L" {
				radius = 7
			}
			outline := color.RGBA{A: 230}
			if marker.Label == "M" {
				radius = 5
				outline.A = 128
			}
			if marker.Category == "entry" || marker.Category == "boss" {
				radius = 10
			}
			drawCampaignMapDot(minimap, point, radius+2, outline)
			drawCampaignMapDot(minimap, point, radius, fill)
			if marker.Label != "" {
				drawCampaignMapLabel(minimap, point, marker.Label)
			}
		}
	}
}

func campaignMapMarkerLayer(marker campaignMapMarker) int {
	if marker.Label == "" {
		return 0
	}
	if marker.Label == "M" {
		return 1
	}
	if marker.Category == "boss" {
		return 3
	}
	return 2
}

func campaignMapMarkerColor(category string, label string) color.RGBA {
	if label == "M" {
		return color.RGBA{R: 218, G: 64, B: 75, A: 128}
	}
	if label == "W" || label == "L" {
		return color.RGBA{R: 218, G: 64, B: 75, A: 255}
	}
	switch category {
	case "entry":
		return color.RGBA{R: 245, G: 248, B: 250, A: 255}
	case "boss":
		return color.RGBA{R: 245, G: 72, B: 92, A: 255}
	case "teleporter", "teleporter-route":
		return color.RGBA{R: 66, G: 211, B: 255, A: 255}
	case "obelisk", "stationary", "interactable":
		return color.RGBA{R: 192, G: 121, B: 255, A: 255}
	case "trigger":
		return color.RGBA{R: 255, G: 223, B: 92, A: 255}
	case "horde":
		return color.RGBA{R: 255, G: 151, B: 61, A: 255}
	default:
		return color.RGBA{R: 104, G: 245, B: 147, A: 255}
	}
}

var campaignMapGlyphs = map[rune][7]uint8{
	'0': {0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110},
	'1': {0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
	'2': {0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b01000, 0b11111},
	'3': {0b11110, 0b00001, 0b00001, 0b01110, 0b00001, 0b00001, 0b11110},
	'4': {0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010},
	'5': {0b11111, 0b10000, 0b10000, 0b11110, 0b00001, 0b00001, 0b11110},
	'6': {0b01110, 0b10000, 0b10000, 0b11110, 0b10001, 0b10001, 0b01110},
	'7': {0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000},
	'8': {0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110},
	'9': {0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00001, 0b01110},
	'A': {0b01110, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
	'B': {0b11110, 0b10001, 0b10001, 0b11110, 0b10001, 0b10001, 0b11110},
	'C': {0b01111, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b01111},
	'D': {0b11110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b11110},
	'E': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b11111},
	'H': {0b10001, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
	'L': {0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b11111},
	'M': {0b10001, 0b11011, 0b10101, 0b10101, 0b10001, 0b10001, 0b10001},
	'O': {0b01110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
	'P': {0b11110, 0b10001, 0b10001, 0b11110, 0b10000, 0b10000, 0b10000},
	'S': {0b01111, 0b10000, 0b10000, 0b01110, 0b00001, 0b00001, 0b11110},
	'T': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100},
	'W': {0b10001, 0b10001, 0b10001, 0b10101, 0b10101, 0b11011, 0b10001},
}

func drawCampaignMapLabel(canvas *image.RGBA, center image.Point, label string) {
	const glyphWidth = 5
	const glyphHeight = 7
	const glyphGap = 1
	glyphScale := 2
	if label == "M" || label == "W" || label == "L" {
		glyphScale = 1
	}
	labelWidth := len(label)*(glyphWidth*glyphScale+glyphGap) - glyphGap
	origin := image.Pt(center.X-labelWidth/2, center.Y-glyphHeight*glyphScale/2)
	points := make([]image.Point, 0, len(label)*glyphWidth*glyphHeight*glyphScale)
	for runeIndex, character := range label {
		glyph, isFound := campaignMapGlyphs[character]
		if !isFound {
			continue
		}
		glyphX := origin.X + runeIndex*(glyphWidth*glyphScale+glyphGap)
		for row, bits := range glyph {
			for column := 0; column < glyphWidth; column++ {
				if bits&(1<<uint(glyphWidth-column-1)) == 0 {
					continue
				}
				for y := 0; y < glyphScale; y++ {
					for x := 0; x < glyphScale; x++ {
						points = append(points, image.Pt(
							glyphX+column*glyphScale+x,
							origin.Y+row*glyphScale+y,
						))
					}
				}
			}
		}
	}
	for _, point := range points {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				shadow := image.Pt(point.X+x, point.Y+y)
				if shadow.In(canvas.Bounds()) {
					canvas.SetRGBA(shadow.X, shadow.Y, color.RGBA{A: 255})
				}
			}
		}
	}
	for _, point := range points {
		if point.In(canvas.Bounds()) {
			canvas.SetRGBA(point.X, point.Y, color.RGBA{
				R: 255, G: 255, B: 255, A: 255,
			})
		}
	}
}

func drawCampaignMapDot(canvas *image.RGBA, center image.Point, radius int, fill color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y > radius*radius {
				continue
			}
			point := image.Pt(center.X+x, center.Y+y)
			if point.In(canvas.Bounds()) {
				blendCampaignMapPixel(canvas, point, fill)
			}
		}
	}
}

func blendCampaignMapPixel(canvas *image.RGBA, point image.Point, fill color.RGBA) {
	if fill.A == 255 {
		canvas.SetRGBA(point.X, point.Y, fill)
		return
	}
	background := color.NRGBAModel.Convert(canvas.At(point.X, point.Y)).(color.NRGBA)
	sourceAlpha := uint32(fill.A)
	backgroundAlpha := uint32(background.A)
	inverseAlpha := uint32(255) - sourceAlpha
	outputAlpha := sourceAlpha + backgroundAlpha*inverseAlpha/255
	if outputAlpha == 0 {
		canvas.SetRGBA(point.X, point.Y, color.RGBA{})
		return
	}
	blendChannel := func(source uint8, destination uint8) uint8 {
		premultiplied := uint32(source)*sourceAlpha +
			uint32(destination)*backgroundAlpha*inverseAlpha/255
		return uint8(premultiplied / outputAlpha)
	}
	canvas.Set(point.X, point.Y, color.NRGBA{
		R: blendChannel(fill.R, background.R),
		G: blendChannel(fill.G, background.G),
		B: blendChannel(fill.B, background.B),
		A: uint8(outputAlpha),
	})
}

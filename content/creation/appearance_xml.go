package creation

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
)

var appearanceColorPattern = regexp.MustCompile(`(<skincolor[123]>)([^<]+)(</skincolor[123]>)`)
var appearanceSeedPattern = regexp.MustCompile(`(<skineffectseed[123]>)[^<]+(</skineffectseed[123]>)`)
var appearanceBlockPattern = regexp.MustCompile(`(?s)<blockref>.*?</blockref>`)
var appearanceScalePattern = regexp.MustCompile(`(<scale>)([^<]+)(</scale>)`)
var appearanceWeightPattern = regexp.MustCompile(`(<weight\s+channel="([^"]+)">)([^<]+)(</weight>)`)

type appearanceBlock struct {
	ID        string `xml:"blockid"`
	ChildIDs  []int  `xml:"childlist>childid"`
	Symmetric *int   `xml:"symmetric"`
	LimbType  int    `xml:"transform>limbtype"`
}

type appearanceVariation struct {
	random         *rand.Rand
	hueShift       float64
	blocks         []appearanceBlock
	blockIndex     int
	partKey        int
	scalesByParts  map[int]float64
	offsetsByParts map[int]map[string]float64
	err            error
}

func varyAppearanceXML(model []byte, seed uint64) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(model))
	isRootFound, isVersionFound := false, false
	blocks := []appearanceBlock{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("modelXML: %w", err)
		}
		start, isStart := token.(xml.StartElement)
		if !isStart {
			continue
		}
		if !isRootFound {
			if start.Name.Local != "sporemodel" {
				return nil, errors.New("unsupported appearance root")
			}
			isRootFound = true
		}
		if start.Name.Local == "formatversion" {
			var version int
			err = decoder.DecodeElement(&version, &start)
			if err != nil {
				return nil, fmt.Errorf("modelVersion: %w", err)
			}
			if version != 21 {
				return nil, errors.New("appearance requires build-103 format 21")
			}
			isVersionFound = true
		}
		if start.Name.Local == "blockref" {
			var block appearanceBlock
			err = decoder.DecodeElement(&block, &start)
			if err != nil {
				return nil, fmt.Errorf("modelPart: %w", err)
			}
			blocks = append(blocks, block)
		}
	}
	if !isRootFound || !isVersionFound {
		return nil, errors.New("appearance format missing")
	}
	random := rand.New(rand.NewPCG(seed, seed^0x6d6f64656c))
	variation := appearanceVariation{random: random, hueShift: 0.12 + random.Float64()*0.76,
		blocks: blocks, scalesByParts: make(map[int]float64), offsetsByParts: make(map[int]map[string]float64)}
	model = appearanceColorPattern.ReplaceAllFunc(model, variation.color)
	model = appearanceSeedPattern.ReplaceAllFunc(model, variation.detail)
	model = appearanceBlockPattern.ReplaceAllFunc(model, variation.block)
	if variation.err != nil {
		return nil, fmt.Errorf("modelVariation: %w", variation.err)
	}
	if variation.blockIndex != len(blocks) {
		return nil, errors.New("appearance block layout unsupported")
	}
	return model, nil
}

func (e *appearanceVariation) color(match []byte) []byte {
	groups := appearanceColorPattern.FindSubmatch(match)
	channels := strings.Split(string(groups[2]), ",")
	if len(channels) != 3 {
		e.err = errors.New("skin color requires three channels")
		return match
	}
	colors := [3]float64{}
	for index, channel := range channels {
		number, err := strconv.ParseFloat(strings.TrimSpace(channel), 64)
		if err != nil {
			e.err = fmt.Errorf("skinChannel: %w", err)
			return match
		}
		if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1 {
			e.err = errors.New("skin channel outside range")
			return match
		}
		colors[index] = number
	}
	colors = e.palette(colors)
	for index, channel := range colors {
		channels[index] = strconv.FormatFloat(channel, 'f', 6, 64)
	}
	return []byte(string(groups[1]) + strings.Join(channels, ",") + string(groups[3]))
}

func (e *appearanceVariation) detail(match []byte) []byte {
	groups := appearanceSeedPattern.FindSubmatch(match)
	return []byte(string(groups[1]) + strconv.Itoa(e.random.IntN(32768)) + string(groups[2]))
}

func (e *appearanceVariation) block(match []byte) []byte {
	index := e.blockIndex
	e.blockIndex++
	if index >= len(e.blocks) {
		e.err = errors.New("unexpected appearance block")
		return match
	}
	block := e.blocks[index]
	// Preserve the spine, limbs, and attachment parents. Authored symmetry
	// links share variations even when mirrored parts have different IDs.
	if isStructuralAppearanceBlock(block) {
		return match
	}
	key := index
	if block.Symmetric != nil && *block.Symmetric >= 0 && *block.Symmetric < len(e.blocks) {
		paired := e.blocks[*block.Symmetric]
		if paired.Symmetric != nil && *paired.Symmetric == index {
			if isStructuralAppearanceBlock(paired) {
				return match
			}
			key = min(index, *block.Symmetric)
		}
	}
	groups := appearanceScalePattern.FindSubmatch(match)
	if len(groups) != 4 {
		return match
	}
	scale, err := strconv.ParseFloat(strings.TrimSpace(string(groups[2])), 64)
	if err != nil {
		e.err = fmt.Errorf("partScale: %w", err)
		return match
	}
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 {
		e.err = errors.New("invalid part scale")
		return match
	}
	multiplier, isFound := e.scalesByParts[key]
	if !isFound {
		multiplier = 0.75 + e.random.Float64()*0.50
		e.scalesByParts[key] = multiplier
	}
	replacement := []byte(string(groups[1]) + strconv.FormatFloat(max(0.000016, scale*multiplier), 'f', 6, 64) + string(groups[3]))
	match = bytes.Replace(match, groups[0], replacement, 1)
	e.partKey = key
	return appearanceWeightPattern.ReplaceAllFunc(match, e.morph)
}

func (e *appearanceVariation) morph(match []byte) []byte {
	groups := appearanceWeightPattern.FindSubmatch(match)
	weight, err := strconv.ParseFloat(strings.TrimSpace(string(groups[3])), 64)
	if err != nil {
		e.err = fmt.Errorf("partWeight: %w", err)
		return match
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 || weight > 1 {
		e.err = errors.New("part weight outside range")
		return match
	}
	offsets := e.offsetsByParts[e.partKey]
	if offsets == nil {
		offsets = make(map[string]float64)
		e.offsetsByParts[e.partKey] = offsets
	}
	channel := string(groups[2])
	offset, isFound := offsets[channel]
	if !isFound {
		offset = (e.random.Float64() - 0.5) * 0.60
		offsets[channel] = offset
	}
	weight = min(1, max(0, weight+offset))
	return []byte(string(groups[1]) + strconv.FormatFloat(weight, 'f', 6, 64) + string(groups[4]))
}

func isStructuralAppearanceBlock(block appearanceBlock) bool {
	return len(block.ChildIDs) > 0 || block.LimbType != 0 || strings.Contains(strings.ToLower(block.ID), "0xd05c53a3")
}

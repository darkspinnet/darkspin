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

type appearanceBlock struct {
	ID       string `xml:"blockid"`
	ChildIDs []int  `xml:"childlist>childid"`
}

type appearanceVariation struct {
	random        *rand.Rand
	scalesByParts map[string]float64
	err           error
}

func varyAppearanceXML(model []byte, seed uint64) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(model))
	isRootFound, isVersionFound := false, false
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
	}
	if !isRootFound || !isVersionFound {
		return nil, errors.New("appearance format missing")
	}
	variation := appearanceVariation{random: rand.New(rand.NewPCG(seed, seed^0x6d6f64656c)), scalesByParts: make(map[string]float64)}
	model = appearanceColorPattern.ReplaceAllFunc(model, variation.color)
	model = appearanceSeedPattern.ReplaceAllFunc(model, variation.detail)
	model = appearanceBlockPattern.ReplaceAllFunc(model, variation.block)
	if variation.err != nil {
		return nil, fmt.Errorf("modelVariation: %w", variation.err)
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
		// Preserve the authored palette's character while varying its tint.
		number = min(1, max(0, number*(0.8+e.random.Float64()*0.4)+(e.random.Float64()-0.5)*0.08))
		channels[index] = strconv.FormatFloat(number, 'f', 6, 64)
	}
	return []byte(string(groups[1]) + strings.Join(channels, ",") + string(groups[3]))
}

func (e *appearanceVariation) detail(match []byte) []byte {
	groups := appearanceSeedPattern.FindSubmatch(match)
	return []byte(string(groups[1]) + strconv.Itoa(e.random.IntN(32768)) + string(groups[2]))
}

func (e *appearanceVariation) block(match []byte) []byte {
	var block appearanceBlock
	err := xml.Unmarshal(match, &block)
	if err != nil {
		e.err = fmt.Errorf("partXML: %w", err)
		return match
	}
	// Keep the spine and every attachment parent unchanged. Matching part
	// identities share a scale multiplier to preserve paired equipment.
	if len(block.ChildIDs) > 0 || strings.Contains(strings.ToLower(block.ID), "0xd05c53a3") {
		return match
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
	multiplier, isFound := e.scalesByParts[block.ID]
	if !isFound {
		multiplier = 0.90 + e.random.Float64()*0.20
		e.scalesByParts[block.ID] = multiplier
	}
	replacement := []byte(string(groups[1]) + strconv.FormatFloat(max(0.000016, scale*multiplier), 'f', 6, 64) + string(groups[3]))
	return bytes.Replace(match, groups[0], replacement, 1)
}

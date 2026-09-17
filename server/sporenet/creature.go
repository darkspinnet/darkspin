package sporenet

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

type CreatureType uint16

const (
	CreatureBio CreatureType = iota
	CreatureCyber
	CreaturePlasma
	CreatureNecro
	CreatureChrono
	CreatureAll
	CreatureTypeUnknown
)

type CreatureClass uint16

const (
	CreatureRavager CreatureClass = iota
	CreatureSentinel
	CreatureTempest
	CreatureClassAll
	CreatureClassUnknown
)

type CreatureParts uint16

const minimumCreatureGearScore float32 = 0

const (
	CreaturePartsAll CreatureParts = iota
	CreatureNoHands
	CreatureNoFeet
	CreaturePartsUnknown
)

type Stat struct {
	Name    string
	Maximum uint32
	Current uint32
}

type AbilityStat struct {
	Key   string
	Token string
	Value string
}

type AbilityLocale struct {
	LocalizationTableID uint32
	NameLocaleID        string
	DescriptionLocaleID string
}

type AbilityProperty struct {
	Name            string
	SourceTableName string
	SourceName      string
	Minimum         float64
	Maximum         float64
	AuthoredMinimum float64
	AuthoredMaximum float64
	Coefficient     float64
	Evidence        string
}

// TemplateCreature is one immutable creature definition.
type TemplateCreature struct {
	Noun                 uint32                       `json:"id"`
	LocalizationTableID  uint32                       `json:"localizationTableId"`
	NameLocaleID         string                       `json:"nameLocaleId"`
	DescriptionLocaleID  string                       `json:"descLocaleId"`
	Name                 string                       `json:"name"`
	ElementType          string                       `json:"elementType"`
	WeaponMinDamage      float64                      `json:"weaponMinDamage"`
	WeaponMaxDamage      float64                      `json:"weaponMaxDamage"`
	GearScore            float32                      `json:"gearScore"`
	ClassType            string                       `json:"classType"`
	StatsTemplate        string                       `json:"statsTemplate"`
	AbilityPassive       uint32                       `json:"abilityPassive"`
	AbilityBasic         uint32                       `json:"abilityBasic"`
	AbilityRandom        uint32                       `json:"abilityRandom"`
	AbilitySpecial1      uint32                       `json:"abilitySpecial1"`
	AbilitySpecial2      uint32                       `json:"abilitySpecial2"`
	AbilityPassiveAsset  string                       `json:"abilityPassiveAsset"`
	AbilityBasicAsset    string                       `json:"abilityBasicAsset"`
	AbilityRandomAsset   string                       `json:"abilityRandomAsset"`
	AbilitySpecial1Asset string                       `json:"abilitySpecial1Asset"`
	AbilitySpecial2Asset string                       `json:"abilitySpecial2Asset"`
	AbilityLocale        map[uint32]AbilityLocale     `json:"-"`
	AbilityProperty      map[uint32][]AbilityProperty `json:"-"`
	AreHandsPresent      bool                         `json:"hasHands"`
	AreFeetPresent       bool                         `json:"hasFeet"`
	Type                 CreatureType
	Class                CreatureClass
	EquipableParts       CreatureParts
	Stats                []Stat
}

// TemplateDatabase indexes creature definitions by name and noun ID.
type TemplateDatabase struct {
	mu              sync.RWMutex
	templatesByName map[string]*TemplateCreature
	templatesByNoun map[uint32]*TemplateCreature
}

func NewTemplateDatabase() *TemplateDatabase {
	return &TemplateDatabase{templatesByName: make(map[string]*TemplateCreature), templatesByNoun: make(map[uint32]*TemplateCreature)}
}

// Load replaces the template database from a legacy creature_templates.json.
func (d *TemplateDatabase) Load(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("templateRead: %w", err)
	}
	var templates []*TemplateCreature
	err = json.Unmarshal(contents, &templates)
	if err != nil {
		return fmt.Errorf("templateDecode: %w", err)
	}
	return d.Replace(templates)
}

// Replace atomically replaces every indexed creature template.
func (d *TemplateDatabase) Replace(templates []*TemplateCreature) error {
	byName := make(map[string]*TemplateCreature, len(templates))
	byNoun := make(map[uint32]*TemplateCreature, len(templates))
	for _, template := range templates {
		if template == nil {
			return fmt.Errorf("templateNil: creature template is nil")
		}
		if _, isFound := byName[template.Name]; isFound {
			return fmt.Errorf("templateName: duplicate %q", template.Name)
		}
		if _, isFound := byNoun[template.Noun]; isFound {
			return fmt.Errorf("templateNoun: duplicate %d", template.Noun)
		}
		template.normalize()
		byName[template.Name] = template
		byNoun[template.Noun] = template
	}
	d.mu.Lock()
	d.templatesByName = byName
	d.templatesByNoun = byNoun
	d.mu.Unlock()
	return nil
}

func (d *TemplateDatabase) ByName(name string) *TemplateCreature {
	d.mu.RLock()
	template := d.templatesByName[name]
	d.mu.RUnlock()
	return template
}

func (d *TemplateDatabase) ByNoun(noun uint32) *TemplateCreature {
	d.mu.RLock()
	template := d.templatesByNoun[noun]
	d.mu.RUnlock()
	return template
}

func (d *TemplateDatabase) List() []*TemplateCreature {
	d.mu.RLock()
	templates := make([]*TemplateCreature, 0, len(d.templatesByName))
	for _, template := range d.templatesByName {
		templates = append(templates, template)
	}
	d.mu.RUnlock()
	return templates
}

func (t *TemplateCreature) normalize() {
	t.Type = parseCreatureType(t.ElementType)
	t.Class = parseCreatureClass(t.ClassType)
	switch {
	case !t.AreHandsPresent:
		t.EquipableParts = CreatureNoHands
	case !t.AreFeetPresent:
		t.EquipableParts = CreatureNoFeet
	default:
		t.EquipableParts = CreaturePartsAll
	}
	t.Stats = parseStats(t.StatsTemplate)
}

// Creature is a player's mutable instance of a template.
type Creature struct {
	Template      *TemplateCreature
	TemplateName  string
	ID            uint32
	Version       uint32
	GearScore     float32
	ItemPoints    float32
	LargeImageURL string
	ThumbImageURL string
	CreatorID     int64
	Stats         []Stat
	AbilityStats  []AbilityStat
}

func NewCreature(template *TemplateCreature) *Creature {
	creature := &Creature{Template: template, Version: 1, GearScore: minimumCreatureGearScore, ItemPoints: 300}
	if template != nil {
		creature.TemplateName = template.Name
		if template.GearScore > creature.GearScore {
			creature.GearScore = template.GearScore
		}
	}
	return creature
}

func (c *Creature) Normalize(templateDatabase *TemplateDatabase) {
	if templateDatabase != nil {
		c.Template = templateDatabase.ByName(c.TemplateName)
	}
	if c.GearScore < minimumCreatureGearScore {
		c.GearScore = minimumCreatureGearScore
	}
}

func (c *Creature) Name() string {
	if c.Template == nil {
		return ""
	}
	return c.Template.Name
}

func (c *Creature) Noun() uint32 {
	if c.Template == nil {
		return 0
	}
	return c.Template.Noun
}

func (c *Creature) Ability(index int) uint32 {
	if c.Template == nil {
		return 0
	}
	abilities := [...]uint32{c.Template.AbilityBasic, c.Template.AbilitySpecial1, c.Template.AbilitySpecial2, c.Template.AbilityRandom, c.Template.AbilityPassive}
	if index < 0 || index >= len(abilities) {
		return 0
	}
	return abilities[index]
}

func (c *Creature) FlattenedGearScore() float32 {
	return float32(math.Floor(float64(c.GearScore)))
}

func (c *Creature) Update(gearScore, itemPoints float32, stats, abilityStats string) {
	c.GearScore = gearScore
	if c.GearScore < minimumCreatureGearScore {
		c.GearScore = minimumCreatureGearScore
	}
	c.ItemPoints = itemPoints
	c.Stats = parseStats(stats)
	c.AbilityStats = nil
	for _, encoded := range strings.Split(abilityStats, ";") {
		if encoded == "" {
			continue
		}
		values := strings.Split(encoded, "!")
		if len(values) < 2 {
			continue
		}
		c.AbilityStats = append(c.AbilityStats, AbilityStat{Token: values[0], Value: values[1]})
	}
}

func parseStats(encoded string) []Stat {
	stats := make([]Stat, 0)
	for _, item := range strings.Split(encoded, ";") {
		if item == "" {
			continue
		}
		values := strings.Split(item, ",")
		if len(values) < 3 {
			continue
		}
		maximum, maxErr := strconv.ParseUint(values[1], 10, 32)
		current, currentErr := strconv.ParseUint(values[2], 10, 32)
		if maxErr != nil || currentErr != nil {
			continue
		}
		stats = append(stats, Stat{Name: values[0], Maximum: uint32(maximum), Current: uint32(current)})
	}
	return stats
}

func parseCreatureType(value string) CreatureType {
	switch strings.ToLower(value) {
	case "bio":
		return CreatureBio
	case "cyber":
		return CreatureCyber
	case "plasma":
		return CreaturePlasma
	case "necro":
		return CreatureNecro
	case "chrono":
		return CreatureChrono
	case "all":
		return CreatureAll
	default:
		return CreatureTypeUnknown
	}
}

func parseCreatureClass(value string) CreatureClass {
	switch strings.ToLower(value) {
	case "ravager":
		return CreatureRavager
	case "sentinel":
		return CreatureSentinel
	case "tempest":
		return CreatureTempest
	case "all":
		return CreatureClassAll
	default:
		return CreatureClassUnknown
	}
}

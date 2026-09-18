// Package sqlite owns the reference-content SQLite database and compiler.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

// Store owns the SQLite connection used for immutable lookup content.
// Content tables will be added here as JSON-backed catalogs are normalized.
type Store struct {
	database *sql.DB
}

// CreatureTemplate is the storage representation of one immutable playable
// creature definition.
type CreatureTemplate struct {
	ID                   uint32
	LocalizationTableID  uint32
	NameLocaleKey        string
	DescriptionLocaleKey string
	Name                 string
	ElementType          string
	ClassType            string
	WeaponMinDamage      float64
	WeaponMaxDamage      float64
	GearScore            float32
	StatTemplate         string
	AbilityPassive       uint32
	AbilityBasic         uint32
	AbilityRandom        uint32
	AbilitySpecial1      uint32
	AbilitySpecial2      uint32
	AbilityPassiveAsset  string
	AbilityBasicAsset    string
	AbilityRandomAsset   string
	AbilitySpecial1Asset string
	AbilitySpecial2Asset string
	AbilityLocale        map[string]CreatureAbilityLocale
	AbilityProperty      map[string][]CreatureAbilityProperty
	IsHandPresent        bool
	IsFootPresent        bool
}

// CreatureAbilityLocale identifies the authored localized name and description
// attached to one creature ability slot.
type CreatureAbilityLocale struct {
	LocalizationTableID  uint32
	NameLocaleKey        string
	DescriptionLocaleKey string
}

// CreatureAbilityProperty is a numeric value proven from an authored Lua
// ability table without executing runtime-dependent expressions.
type CreatureAbilityProperty struct {
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

// NonPlayerClass is the proven runtime combat-stat projection for one
// non-player noun base instance.
type NonPlayerClass struct {
	InstanceID             uint32
	NounName               string
	DisplayName            string
	DisplayNameLocaleKey   string
	Description            string
	DescriptionLocaleKey   string
	NPCAffixNames          [NonPlayerAffixLimit]string
	ChallengeValue         int32
	NPCRank                int32
	IsTargetable           bool
	IsPlayerPet            bool
	PlayerCountHealthScale float32
	HitPoint               float32
	PowerPoint             float32
	Strength               float32
	Dexterity              float32
	Mind                   float32
	DodgeRating            float32
	ResistRating           float32
	CriticalRating         float32
}

// NonPlayerNounProfile joins one noun to its authored non-player class and
// physical scale without exposing storage IDs to the game feature.
type NonPlayerNounProfile struct {
	NounName               string
	DisplayName            string
	DisplayNameLocaleKey   string
	Description            string
	DescriptionLocaleKey   string
	NPCAffixNames          [NonPlayerAffixLimit]string
	ChallengeValue         int32
	NPCRank                int32
	IsTargetable           bool
	IsPlayerPet            bool
	PlayerCountHealthScale float32
	HitPoint               float32
	PowerPoint             float32
	Strength               float32
	Dexterity              float32
	Mind                   float32
	DodgeRating            float32
	ResistRating           float32
	CriticalRating         float32
	GraphicsScale          float32
	FootprintRadius        float32
}

// NounPhysics is the storage representation of proven noun collision and
// footprint content used by the simulation.
type NounPhysics struct {
	AssetName              string
	CreatureType           uint32
	IsCreatureTypeKnown    bool
	OrdinaryDeathAnimation string
	DanceAnimation         string
	LifetimeSeconds        float32
	GraphicsScale          float32
	FootprintRadius        float32
	BoundMinimumX          float32
	BoundMinimumY          float32
	BoundMinimumZ          float32
	BoundMaximumX          float32
	BoundMaximumY          float32
	BoundMaximumZ          float32
}

// NounPhysicsShape is one proven inline collision-query shape.
type NounPhysicsShape struct {
	AssetName  string
	ShapeRole  string
	ShapeKind  string
	DimensionX float32
	DimensionY float32
	DimensionZ float32
}

// CrystalDefinition is one authored CrystalTuning selection entry. The source
// reference is the serialized relocation resolved to NounReference by the
// content loader.
type CrystalDefinition struct {
	Ordinal         int
	MinimumLevel    int32
	MaximumLevel    int32
	Weight          uint32
	SourceReference uint32
	NounReference   string
}

// CrystalLevelOffset is one authored weighted adjustment applied after the
// executable derives a crystal level from encounter difficulty.
type CrystalLevelOffset struct {
	Ordinal int
	Offset  int32
	Weight  float32
}

// New opens an empty content database without migration/version bookkeeping.
func New(ctx context.Context, path string) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if path == "" {
		return nil, errors.New("empty path")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(absolutePath), 0o755)
	if err != nil {
		return nil, fmt.Errorf("pathMkdir: %w", err)
	}
	parameters := url.Values{}
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "foreign_keys(1)")
	parameters.Add("_pragma", "journal_mode(WAL)")
	parameters.Add("_pragma", "synchronous(NORMAL)")
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absolutePath)+"?"+parameters.Encode())
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(2)
	err = database.PingContext(ctx)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("databasePing: %w", err)
	}
	return &Store{database: database}, nil
}

// Close releases the content database.
func (s *Store) Close() error {
	if s == nil || s.database == nil {
		return nil
	}
	err := s.database.Close()
	if err != nil {
		return fmt.Errorf("databaseClose: %w", err)
	}
	return nil
}

// CreatureTemplates reads the complete playable-creature projection.
func (s *Store) CreatureTemplates(ctx context.Context) ([]CreatureTemplate, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, name_locale_key, description_locale_key, name, element_type, class_type,
		       weapon_min_damage, weapon_max_damage, gear_score, stat_template,
		       ability_passive, ability_basic, ability_random, ability_special_1, ability_special_2,
		       is_hand_present, is_foot_present
		FROM creature_template
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("creatureQuery: %w", err)
	}
	defer rows.Close()
	templates := make([]CreatureTemplate, 0, 100)
	for rows.Next() {
		var template CreatureTemplate
		err = rows.Scan(&template.ID, &template.NameLocaleKey, &template.DescriptionLocaleKey, &template.Name,
			&template.ElementType, &template.ClassType, &template.WeaponMinDamage, &template.WeaponMaxDamage,
			&template.GearScore, &template.StatTemplate, &template.AbilityPassive, &template.AbilityBasic,
			&template.AbilityRandom, &template.AbilitySpecial1, &template.AbilitySpecial2,
			&template.IsHandPresent, &template.IsFootPresent)
		if err != nil {
			return nil, fmt.Errorf("creatureScan: %w", err)
		}
		templates = append(templates, template)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("creatureRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("creatureClose: %w", err)
	}
	err = s.enrichCreatureTemplates(ctx, templates)
	if err != nil {
		return nil, fmt.Errorf("creatureEnrich: %w", err)
	}
	return templates, nil
}

func (s *Store) enrichCreatureTemplates(ctx context.Context, templates []CreatureTemplate) error {
	isLocalizationStored, err := hasTable(ctx, s.database, "localization_text")
	if err != nil {
		return fmt.Errorf("localeTable: %w", err)
	}
	if isLocalizationStored {
		for index := range templates {
			err = s.database.QueryRowContext(ctx, `
				SELECT table_id FROM localization_text
				WHERE locale = 'en-us' AND locale_key = ?
				LIMIT 1`, templates[index].NameLocaleKey).Scan(&templates[index].LocalizationTableID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("localeRead[%d]: %w", templates[index].ID, err)
			}
		}
	}
	isAbilityStored, err := hasTable(ctx, s.database, "creature_template_ability")
	if err != nil {
		return fmt.Errorf("abilityTable: %w", err)
	}
	if !isAbilityStored {
		return nil
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT creature_template_id, slot, asset_name
		FROM creature_template_ability
		ORDER BY creature_template_id, id`)
	if err != nil {
		return fmt.Errorf("abilityQuery: %w", err)
	}
	defer rows.Close()
	byID := make(map[uint32]*CreatureTemplate, len(templates))
	for index := range templates {
		byID[templates[index].ID] = &templates[index]
	}
	for rows.Next() {
		var creatureTemplateID uint32
		var slot string
		var assetName string
		err = rows.Scan(&creatureTemplateID, &slot, &assetName)
		if err != nil {
			return fmt.Errorf("abilityScan: %w", err)
		}
		template := byID[creatureTemplateID]
		if template == nil {
			continue
		}
		switch slot {
		case "passive":
			template.AbilityPassiveAsset = assetName
		case "basic":
			template.AbilityBasicAsset = assetName
		case "random":
			template.AbilityRandomAsset = assetName
		case "special_1":
			template.AbilitySpecial1Asset = assetName
		case "special_2":
			template.AbilitySpecial2Asset = assetName
		}
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("abilityRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return fmt.Errorf("abilityClose: %w", err)
	}
	err = s.enrichCreatureAbilityLocale(ctx, byID)
	if err != nil {
		return fmt.Errorf("abilityLocale: %w", err)
	}
	err = s.enrichCreatureAbilityProperty(ctx, byID)
	if err != nil {
		return fmt.Errorf("abilityProperty: %w", err)
	}
	err = s.enrichCreatureAbilityToken(ctx, byID)
	if err != nil {
		return fmt.Errorf("abilityToken: %w", err)
	}
	return nil
}

func (s *Store) enrichCreatureAbilityLocale(ctx context.Context, templates map[uint32]*CreatureTemplate) error {
	isLocalizationStored, err := hasTable(ctx, s.database, "localization_text")
	if err != nil {
		return fmt.Errorf("localeTable: %w", err)
	}
	isLuaChunkStored, err := hasTable(ctx, s.database, "lua_chunk")
	if err != nil {
		return fmt.Errorf("luaTable: %w", err)
	}
	isLuaStringStored, err := hasTable(ctx, s.database, "lua_string_constant")
	if err != nil {
		return fmt.Errorf("luaStringTable: %w", err)
	}
	if !isLocalizationStored || !isLuaChunkStored || !isLuaStringStored {
		return nil
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT DISTINCT creature_template_ability.creature_template_id,
		       creature_template_ability.slot,
		       COALESCE((
		         SELECT localization_text.table_id
		         FROM localization_text
		         WHERE localization_text.locale = 'en-us'
		           AND localization_text.locale_key = name_value.string_constant
		         LIMIT 1
		       ), 0),
		       name_value.string_constant,
		       description_value.string_constant
		FROM creature_template_ability
		JOIN lua_string_constant AS ability_identity
		  ON ability_identity.string_constant = creature_template_ability.asset_name COLLATE NOCASE
		JOIN lua_chunk ON lua_chunk.id = ability_identity.lua_chunk_id
		JOIN lua_string_constant AS name_marker
		  ON name_marker.lua_chunk_id = lua_chunk.id
		 AND name_marker.string_constant = 'localizedName'
		JOIN lua_string_constant AS name_value
		  ON name_value.lua_chunk_id = lua_chunk.id
		 AND name_value.ordinal = name_marker.ordinal + 2
		JOIN lua_string_constant AS description_marker
		  ON description_marker.lua_chunk_id = lua_chunk.id
		 AND description_marker.string_constant = 'localizedDescription'
		JOIN lua_string_constant AS description_value
		  ON description_value.lua_chunk_id = lua_chunk.id
		 AND description_value.ordinal = description_marker.ordinal + 1
		WHERE name_value.string_constant LIKE '0x%'
		  AND description_value.string_constant LIKE '0x%'
		ORDER BY creature_template_ability.creature_template_id, creature_template_ability.id`)
	if err != nil {
		return fmt.Errorf("abilityLocaleQuery: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var creatureTemplateID uint32
		var slot string
		var abilityLocale CreatureAbilityLocale
		err = rows.Scan(
			&creatureTemplateID, &slot, &abilityLocale.LocalizationTableID,
			&abilityLocale.NameLocaleKey, &abilityLocale.DescriptionLocaleKey,
		)
		if err != nil {
			return fmt.Errorf("abilityLocaleScan: %w", err)
		}
		template := templates[creatureTemplateID]
		if template == nil {
			continue
		}
		if template.AbilityLocale == nil {
			template.AbilityLocale = make(map[string]CreatureAbilityLocale, len(creatureAbilitySlots))
		}
		if _, isFound := template.AbilityLocale[slot]; !isFound {
			template.AbilityLocale[slot] = abilityLocale
		}
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("abilityLocaleRows: %w", err)
	}
	return nil
}

func (s *Store) enrichCreatureAbilityProperty(ctx context.Context, templates map[uint32]*CreatureTemplate) error {
	isPropertyStored, err := hasTable(ctx, s.database, "lua_static_property")
	if err != nil {
		return fmt.Errorf("propertyTable: %w", err)
	}
	if !isPropertyStored {
		return nil
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT DISTINCT creature_template_ability.creature_template_id,
		       creature_template_ability.slot,
		       lua_static_property.table_name,
		       lua_static_property.property_name,
		       lua_static_property.minimum,
		       lua_static_property.maximum,
		       lua_static_property.evidence
		FROM creature_template_ability
		JOIN lua_string_constant AS ability_identity
		  ON ability_identity.string_constant = creature_template_ability.asset_name COLLATE NOCASE
		JOIN lua_static_property
		  ON lua_static_property.lua_chunk_id = ability_identity.lua_chunk_id
		WHERE lua_static_property.table_name LIKE 'nAbility_%'
		   OR (creature_template_ability.slot = 'passive'
		       AND lua_static_property.table_name LIKE 'nModifier_%')
		ORDER BY creature_template_ability.creature_template_id,
		         creature_template_ability.id, lua_static_property.property_name`)
	if err != nil {
		return fmt.Errorf("propertyQuery: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var creatureTemplateID uint32
		var slot string
		var property CreatureAbilityProperty
		err = rows.Scan(&creatureTemplateID, &slot, &property.SourceTableName, &property.Name,
			&property.Minimum, &property.Maximum, &property.Evidence)
		if err != nil {
			return fmt.Errorf("propertyScan: %w", err)
		}
		template := templates[creatureTemplateID]
		if template == nil {
			continue
		}
		property.SourceName = property.Name
		property.AuthoredMinimum = property.Minimum
		property.AuthoredMaximum = property.Maximum
		identity := fmt.Sprintf("%d/%s/%s/%s", creatureTemplateID, slot, property.SourceTableName, property.Name)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		if template.AbilityProperty == nil {
			template.AbilityProperty = make(map[string][]CreatureAbilityProperty, len(creatureAbilitySlots))
		}
		template.AbilityProperty[slot] = append(template.AbilityProperty[slot], property)
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("propertyRows: %w", err)
	}
	return nil
}

func (s *Store) enrichCreatureAbilityToken(ctx context.Context, templates map[uint32]*CreatureTemplate) error {
	isBindingStored, err := hasTable(ctx, s.database, "lua_token_binding")
	if err != nil {
		return fmt.Errorf("bindingTable: %w", err)
	}
	if !isBindingStored {
		return nil
	}
	coefficients, err := s.creatureAbilityCoefficients(ctx)
	if err != nil {
		return fmt.Errorf("coefficientRead: %w", err)
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT DISTINCT creature_template_ability.creature_template_id,
		       creature_template_ability.slot,
		       lua_token_binding.token_name,
		       lua_token_binding.source_table_name,
		       lua_token_binding.property_name,
		       (CASE lua_token_binding.element_index
		          WHEN 1 THEN lua_static_property.minimum
		          WHEN 2 THEN lua_static_property.maximum
		          ELSE lua_static_property.minimum
		        END) * lua_token_binding.multiplier,
		       lua_static_property.minimum,
		       lua_static_property.maximum,
		       lua_token_binding.evidence
		FROM creature_template_ability
		JOIN lua_string_constant AS ability_identity
		  ON ability_identity.string_constant = creature_template_ability.asset_name COLLATE NOCASE
		JOIN lua_token_binding
		  ON lua_token_binding.lua_chunk_id = ability_identity.lua_chunk_id
		JOIN lua_static_property
		  ON lua_static_property.table_name = lua_token_binding.source_table_name
		 AND lua_static_property.property_name = lua_token_binding.property_name
		ORDER BY creature_template_ability.creature_template_id,
		         creature_template_ability.id, lua_token_binding.token_name,
		         lua_static_property.id`)
	if err != nil {
		return fmt.Errorf("tokenQuery: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var creatureTemplateID uint32
		var slot string
		var property CreatureAbilityProperty
		err = rows.Scan(&creatureTemplateID, &slot, &property.Name, &property.SourceTableName, &property.SourceName,
			&property.Minimum, &property.AuthoredMinimum, &property.AuthoredMaximum,
			&property.Evidence)
		if err != nil {
			return fmt.Errorf("tokenScan: %w", err)
		}
		property.Maximum = property.Minimum
		coefficient, isFound := coefficients[[2]string{property.SourceTableName, property.SourceName + "Coefficient"}]
		if !isFound {
			coefficientName := "damageCoefficient"
			switch strings.ToLower(property.Name) {
			case "minhealing", "maxhealing", "healing":
				coefficientName = "healingCoefficient"
			}
			coefficient = coefficients[[2]string{property.SourceTableName, coefficientName}]
		}
		property.Coefficient = coefficient
		template := templates[creatureTemplateID]
		if template == nil {
			continue
		}
		identity := fmt.Sprintf("%d/%s/%s/%s", creatureTemplateID, slot, property.SourceTableName, property.Name)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		if template.AbilityProperty == nil {
			template.AbilityProperty = make(map[string][]CreatureAbilityProperty, len(creatureAbilitySlots))
		}
		template.AbilityProperty[slot] = append(template.AbilityProperty[slot], property)
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("tokenRows: %w", err)
	}
	return nil
}

// creatureAbilityCoefficients avoids unindexed, correlated property scans for
// every token. Load once so existing content caches need no schema migration.
func (e *Store) creatureAbilityCoefficients(ctx context.Context) (map[[2]string]float64, error) {
	rows, err := e.database.QueryContext(ctx, `
		SELECT table_name, property_name, minimum
		FROM lua_static_property
		WHERE property_name LIKE '%Coefficient'
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("coefficientQuery: %w", err)
	}
	defer rows.Close()
	coefficients := make(map[[2]string]float64)
	for rows.Next() {
		var tableName, propertyName string
		var coefficient float64
		err = rows.Scan(&tableName, &propertyName, &coefficient)
		if err != nil {
			return nil, fmt.Errorf("coefficientScan: %w", err)
		}
		// Preserve the first stored property when multiple chunks define it.
		key := [2]string{tableName, propertyName}
		if _, isFound := coefficients[key]; !isFound {
			coefficients[key] = coefficient
		}
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("coefficientRows: %w", err)
	}
	return coefficients, nil
}

// NonPlayerClasses reads the immutable non-player combat-stat projection.
func (s *Store) NonPlayerClasses(ctx context.Context) ([]NonPlayerClass, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT instance_id, noun_name, display_name, display_name_locale_key,
			description, description_locale_key,
			challenge_value, npc_rank, is_targetable, is_player_pet,
			player_count_health_scale, hit_point, power_point, strength, dexterity, mind,
			dodge_rating, resist_rating, critical_rating
		FROM non_player_class ORDER BY instance_id`)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerClassQuery: %w", err)
	}
	defer rows.Close()
	classes := make([]NonPlayerClass, 0, 100)
	for rows.Next() {
		var class NonPlayerClass
		err = rows.Scan(
			&class.InstanceID, &class.NounName, &class.DisplayName,
			&class.DisplayNameLocaleKey, &class.Description, &class.DescriptionLocaleKey,
			&class.ChallengeValue, &class.NPCRank,
			&class.IsTargetable, &class.IsPlayerPet, &class.PlayerCountHealthScale,
			&class.HitPoint, &class.PowerPoint,
			&class.Strength, &class.Dexterity, &class.Mind,
			&class.DodgeRating, &class.ResistRating, &class.CriticalRating,
		)
		if err != nil {
			return nil, fmt.Errorf("nonPlayerClassScan: %w", err)
		}
		classes = append(classes, class)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("nonPlayerClassRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("nonPlayerClassClose: %w", err)
	}
	classesByInstance := make(map[uint32]int, len(classes))
	for index, class := range classes {
		classesByInstance[class.InstanceID] = index
	}
	affixRows, err := s.database.QueryContext(ctx, `
		SELECT non_player_class.instance_id, non_player_class_affix.ordinal,
		       non_player_class_affix.asset_name
		FROM non_player_class_affix
		JOIN non_player_class
		  ON non_player_class.content_source_resource_id=
		     non_player_class_affix.non_player_class_resource_id
		ORDER BY non_player_class.instance_id, non_player_class_affix.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerAffixQuery: %w", err)
	}
	defer affixRows.Close()
	for affixRows.Next() {
		var instanceID uint32
		var ordinal int
		var affixName string
		err = affixRows.Scan(&instanceID, &ordinal, &affixName)
		if err != nil {
			return nil, fmt.Errorf("nonPlayerAffixScan: %w", err)
		}
		classIndex, isClassFound := classesByInstance[instanceID]
		if !isClassFound || ordinal < 0 || ordinal >= NonPlayerAffixLimit {
			return nil, fmt.Errorf("nonPlayerAffixOwner[%d:%d]: missing", instanceID, ordinal)
		}
		classes[classIndex].NPCAffixNames[ordinal] = affixName
	}
	err = affixRows.Err()
	if err != nil {
		return nil, fmt.Errorf("nonPlayerAffixRows: %w", err)
	}
	return classes, nil
}

// NonPlayerNounProfiles reads every authored ClassAttributes identity and
// overlays exact noun physics where that narrower catalog is available.
func (s *Store) NonPlayerNounProfiles(ctx context.Context) ([]NonPlayerNounProfile, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT noun_physics.asset_name,
		       non_player_class.display_name,
		       non_player_class.display_name_locale_key,
		       non_player_class.description,
		       non_player_class.description_locale_key,
		       non_player_class.challenge_value, non_player_class.npc_rank,
		       non_player_class.is_targetable, non_player_class.is_player_pet,
		       non_player_class.player_count_health_scale,
		       non_player_class.hit_point, non_player_class.power_point,
		       non_player_class.strength, non_player_class.dexterity, non_player_class.mind,
		       non_player_class.dodge_rating, non_player_class.resist_rating,
		       non_player_class.critical_rating,
		       noun_physics.graphics_scale, noun_physics.footprint_radius
		FROM noun_physics
		JOIN content_source_resource AS non_player_resource
		  ON non_player_resource.id=noun_physics.class_attribute_resource_id
		JOIN non_player_class
		  ON non_player_class.instance_id=non_player_resource.instance_id
		ORDER BY noun_physics.asset_name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerNounQuery: %w", err)
	}
	profiles := make([]NonPlayerNounProfile, 0, 100)
	profileIndexesByNoun := make(map[string]int, 100)
	for rows.Next() {
		var profile NonPlayerNounProfile
		err = rows.Scan(
			&profile.NounName, &profile.DisplayName, &profile.DisplayNameLocaleKey,
			&profile.Description, &profile.DescriptionLocaleKey,
			&profile.ChallengeValue, &profile.NPCRank,
			&profile.IsTargetable, &profile.IsPlayerPet, &profile.PlayerCountHealthScale,
			&profile.HitPoint, &profile.PowerPoint,
			&profile.Strength, &profile.Dexterity, &profile.Mind,
			&profile.DodgeRating, &profile.ResistRating, &profile.CriticalRating,
			&profile.GraphicsScale, &profile.FootprintRadius,
		)
		if err != nil {
			return nil, fmt.Errorf("nonPlayerNounScan: %w", err)
		}
		profiles = append(profiles, profile)
		profileIndexesByNoun[strings.ToLower(profile.NounName)] = len(profiles) - 1
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("nonPlayerNounRows: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("nonPlayerNounClose: %w", closeErr)
	}
	classes, err := s.NonPlayerClasses(ctx)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerNounClasses: %w", err)
	}
	classesByNoun := make(map[string]NonPlayerClass, len(classes))
	for _, class := range classes {
		nounKey := strings.ToLower(class.NounName)
		_, isClassFound := classesByNoun[nounKey]
		classStem := strings.TrimSuffix(class.NounName, ".Noun")
		isCanonicalClass := class.InstanceID == hashID(classStem)
		if !isClassFound || isCanonicalClass {
			classesByNoun[nounKey] = class
		}
	}
	for index := range profiles {
		class, isClassFound := classesByNoun[strings.ToLower(profiles[index].NounName)]
		if !isClassFound {
			continue
		}
		profiles[index].NPCAffixNames = class.NPCAffixNames
	}
	// Named captains are packaged as a second ClassAttributes identity for
	// the ordinary noun family, while the director refers to the promoted
	// actor through its synthetic _Captain noun. Preserve that distinct
	// identity and combine it with the canonical family's combat/physics
	// profile instead of collapsing it into the ordinary actor.
	for _, class := range classes {
		nounKey := strings.ToLower(class.NounName)
		canonicalClass, isCanonicalClassFound := classesByNoun[nounKey]
		if !isCanonicalClassFound || !isNamedCaptainClass(class, canonicalClass) {
			continue
		}
		captainNounName := namedCaptainNounName(class.NounName)
		captainNounKey := strings.ToLower(captainNounName)
		_, isCaptainProfileFound := profileIndexesByNoun[captainNounKey]
		if isCaptainProfileFound {
			continue
		}
		profile := nonPlayerClassProfile(captainNounName, canonicalClass)
		baseProfileIndex, isBaseProfileFound := profileIndexesByNoun[nounKey]
		if isBaseProfileFound {
			profile = profiles[baseProfileIndex]
			profile.NounName = captainNounName
		}
		profile.DisplayName = class.DisplayName
		profile.DisplayNameLocaleKey = class.DisplayNameLocaleKey
		profile.Description = class.Description
		profile.DescriptionLocaleKey = class.DescriptionLocaleKey
		profile.NPCAffixNames = class.NPCAffixNames
		profile.ChallengeValue = class.ChallengeValue
		profile.NPCRank = class.NPCRank
		profile.IsTargetable = class.IsTargetable
		profile.IsPlayerPet = class.IsPlayerPet
		profile.PlayerCountHealthScale = class.PlayerCountHealthScale
		profiles = append(profiles, profile)
		profileIndexesByNoun[captainNounKey] = len(profiles) - 1
	}
	for _, class := range classesByNoun {
		nounKey := strings.ToLower(class.NounName)
		_, isProfileFound := profileIndexesByNoun[nounKey]
		if isProfileFound {
			continue
		}
		profiles = append(profiles, nonPlayerClassProfile(class.NounName, class))
		profileIndexesByNoun[nounKey] = len(profiles) - 1
	}
	slices.SortFunc(profiles, func(left NonPlayerNounProfile, right NonPlayerNounProfile) int {
		return strings.Compare(strings.ToLower(left.NounName), strings.ToLower(right.NounName))
	})
	return profiles, nil
}

func nonPlayerClassProfile(nounName string, class NonPlayerClass) NonPlayerNounProfile {
	return NonPlayerNounProfile{
		NounName: nounName, DisplayName: class.DisplayName,
		DisplayNameLocaleKey: class.DisplayNameLocaleKey,
		Description:          class.Description,
		DescriptionLocaleKey: class.DescriptionLocaleKey,
		NPCAffixNames:        class.NPCAffixNames,
		ChallengeValue:       class.ChallengeValue, NPCRank: class.NPCRank,
		IsTargetable: class.IsTargetable, IsPlayerPet: class.IsPlayerPet,
		PlayerCountHealthScale: class.PlayerCountHealthScale,
		HitPoint:               class.HitPoint, PowerPoint: class.PowerPoint,
		Strength: class.Strength, Dexterity: class.Dexterity, Mind: class.Mind,
		DodgeRating: class.DodgeRating, ResistRating: class.ResistRating,
		CriticalRating: class.CriticalRating,
		// The client owns authored noun rendering. A neutral scale and
		// conservative footprint keep server simulation usable until the
		// complete noun-physics catalog is imported.
		GraphicsScale: 1, FootprintRadius: 0.5,
	}
}

func isNamedCaptainClass(class NonPlayerClass, canonicalClass NonPlayerClass) bool {
	if class.InstanceID == canonicalClass.InstanceID || class.HitPoint > 0 {
		return false
	}
	className := strings.TrimSpace(class.DisplayName)
	canonicalName := strings.TrimSpace(canonicalClass.DisplayName)
	return className != "" && !strings.EqualFold(className, canonicalName)
}

func namedCaptainNounName(nounName string) string {
	baseName := strings.TrimSpace(nounName)
	extension := ""
	if strings.HasSuffix(strings.ToLower(baseName), ".noun") {
		extension = baseName[len(baseName)-len(".Noun"):]
		baseName = baseName[:len(baseName)-len(".Noun")]
	}
	for _, rankSuffix := range []string{"_2", "_3"} {
		if strings.HasSuffix(strings.ToLower(baseName), rankSuffix) {
			baseName = baseName[:len(baseName)-len(rankSuffix)] + "_Captain" + rankSuffix
			return baseName + extension
		}
	}
	return baseName + "_Captain" + extension
}

// NounPhysicsCatalog reads the immutable tutorial noun physics projection.
func (s *Store) NounPhysicsCatalog(ctx context.Context) ([]NounPhysics, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT asset_name, creature_type, ordinary_death_animation, dance_animation, lifetime_seconds,
		       graphics_scale, footprint_radius,
		       bound_min_x, bound_min_y, bound_min_z, bound_max_x, bound_max_y, bound_max_z
		FROM noun_physics ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("nounPhysicsQuery: %w", err)
	}
	defer rows.Close()
	catalog := make([]NounPhysics, 0, len(tutorialNounPhysicsSpec))
	for rows.Next() {
		var noun NounPhysics
		var creatureType sql.NullInt64
		var ordinaryDeathAnimation sql.NullString
		var danceAnimation sql.NullString
		err = rows.Scan(&noun.AssetName, &creatureType, &ordinaryDeathAnimation, &danceAnimation, &noun.LifetimeSeconds,
			&noun.GraphicsScale, &noun.FootprintRadius,
			&noun.BoundMinimumX, &noun.BoundMinimumY, &noun.BoundMinimumZ,
			&noun.BoundMaximumX, &noun.BoundMaximumY, &noun.BoundMaximumZ)
		if err != nil {
			return nil, fmt.Errorf("nounPhysicsScan: %w", err)
		}
		if creatureType.Valid {
			noun.CreatureType = uint32(creatureType.Int64)
			noun.IsCreatureTypeKnown = true
		}
		if ordinaryDeathAnimation.Valid {
			noun.OrdinaryDeathAnimation = ordinaryDeathAnimation.String
		}
		if danceAnimation.Valid {
			noun.DanceAnimation = danceAnimation.String
		}
		catalog = append(catalog, noun)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("nounPhysicsRows: %w", err)
	}
	return catalog, nil
}

// NounPhysicsShapes reads the proven inline query shapes.
func (s *Store) NounPhysicsShapes(ctx context.Context) ([]NounPhysicsShape, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT noun_physics.asset_name, noun_physics_shape.shape_role,
		       noun_physics_shape.shape_kind, noun_physics_shape.dimension_x,
		       noun_physics_shape.dimension_y, noun_physics_shape.dimension_z
		FROM noun_physics_shape
		JOIN noun_physics ON noun_physics.id=noun_physics_shape.noun_physics_id
		ORDER BY noun_physics.id, noun_physics_shape.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("nounShapeQuery: %w", err)
	}
	defer rows.Close()
	shapes := make([]NounPhysicsShape, 0, 1)
	for rows.Next() {
		var shape NounPhysicsShape
		err = rows.Scan(&shape.AssetName, &shape.ShapeRole, &shape.ShapeKind,
			&shape.DimensionX, &shape.DimensionY, &shape.DimensionZ)
		if err != nil {
			return nil, fmt.Errorf("nounShapeScan: %w", err)
		}
		shapes = append(shapes, shape)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("nounShapeRows: %w", err)
	}
	return shapes, nil
}

// CrystalDefinitions reads the complete authored CrystalTuning projection in
// stable source order.
func (s *Store) CrystalDefinitions(ctx context.Context) ([]CrystalDefinition, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT ordinal, minimum_level, maximum_level, weight, source_reference, noun_reference
		FROM crystal_definition
		ORDER BY ordinal`)
	if err != nil {
		return nil, fmt.Errorf("crystalDefinitionQuery: %w", err)
	}
	defer rows.Close()
	definitions := make([]CrystalDefinition, 0, crystalTuningDefinitionCount)
	for rows.Next() {
		var definition CrystalDefinition
		err = rows.Scan(&definition.Ordinal, &definition.MinimumLevel, &definition.MaximumLevel,
			&definition.Weight, &definition.SourceReference, &definition.NounReference)
		if err != nil {
			return nil, fmt.Errorf("crystalDefinitionScan: %w", err)
		}
		definitions = append(definitions, definition)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("crystalDefinitionRows: %w", err)
	}
	return definitions, nil
}

// CrystalLevelOffsets reads the authored CrystalTuning adjustment choices in
// stable source order.
func (s *Store) CrystalLevelOffsets(ctx context.Context) ([]CrystalLevelOffset, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT ordinal, level_offset, weight
		FROM crystal_level_offset
		ORDER BY ordinal`)
	if err != nil {
		return nil, fmt.Errorf("crystalLevelOffsetQuery: %w", err)
	}
	defer rows.Close()
	offsets := make([]CrystalLevelOffset, 0, crystalTuningOffsetCount)
	for rows.Next() {
		var offset CrystalLevelOffset
		err = rows.Scan(&offset.Ordinal, &offset.Offset, &offset.Weight)
		if err != nil {
			return nil, fmt.Errorf("crystalLevelOffsetScan: %w", err)
		}
		offsets = append(offsets, offset)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("crystalLevelOffsetRows: %w", err)
	}
	return offsets, nil
}

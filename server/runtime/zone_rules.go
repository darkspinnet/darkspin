package runtime

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/game"
)

//go:embed zone-rules.json
var embeddedZoneRules []byte

type zoneRuleDocument struct {
	Default   zoneRuleConfiguration            `json:"default"`
	Campaigns map[string]zoneRuleConfiguration `json:"campaign"`
	Replays   map[string]zoneRuleConfiguration `json:"replay"`
	Tutorial  *zoneRuleConfiguration           `json:"tutorial,omitempty"`
	Arena     *zoneRuleConfiguration           `json:"arena,omitempty"`
}

type zonePopulationConfiguration struct {
	AgentSelection              string `json:"agent_selection"`
	AgentsPerSection            uint32 `json:"agents_per_section"`
	RosterSelection             string `json:"roster_selection"`
	ExternalRosterWeightPercent uint32 `json:"external_roster_weight_percent"`
	AmbientEliteChancePercent   uint32 `json:"ambient_elite_chance_percent"`
	OwnedSummonHealthScaling    string `json:"owned_summon_health_scaling"`
	FixtureHealthScaling        string `json:"fixture_health_scaling"`
}

type zoneAllyAlertConfiguration struct {
	RangePercent uint32 `json:"range_percent"`
	RangeOwner   string `json:"range_owner"`
	MaxHops      uint32 `json:"max_hops"`
}

type zoneMutationConfiguration struct {
	MinimumChainLevel         uint32 `json:"minimum_chain_level"`
	WaveCount                 int    `json:"wave_count"`
	SpawnWave                 int    `json:"spawn_wave"`
	InterWaveDelayMillisecond uint32 `json:"inter_wave_delay_millisecond"`
	Completion                string `json:"completion"`
}

type zoneBossConfiguration struct {
	OpeningAddsPerAnchor    uint32 `json:"opening_adds_per_anchor"`
	InitialBossAddCount     uint32 `json:"initial_boss_add_count"`
	CaptainDelayMillisecond uint32 `json:"captain_delay_millisecond"`
}

type zoneRuleConfiguration struct {
	EnemiesPerSection uint32                      `json:"enemies_per_section"`
	EnemyPopulation   zonePopulationConfiguration `json:"enemy_population"`
	AllyAlert         *zoneAllyAlertConfiguration `json:"ally_alert,omitempty"`
	BossSpawning      zoneBossConfiguration       `json:"boss_spawning"`
	MutationAgent     *zoneMutationConfiguration  `json:"mutation_agent,omitempty"`
}

func (e zoneRuleConfiguration) rules(key string) game.ZoneRules {
	boss := game.DefaultBossSpawnRules()
	boss.OpeningAddsPerAnchor = e.BossSpawning.OpeningAddsPerAnchor
	boss.InitialBossAddCount = e.BossSpawning.InitialBossAddCount
	boss.CaptainDelayMillisecond = e.BossSpawning.CaptainDelayMillisecond
	rules := game.ZoneRules{
		Key: key, EnemyTargetPerSection: e.EnemiesPerSection,
		EnemyPopulation: game.EnemyPopulationRules{
			AgentSelection: e.EnemyPopulation.AgentSelection, AgentsPerSection: e.EnemyPopulation.AgentsPerSection,
			RosterSelection: e.EnemyPopulation.RosterSelection, ExternalRosterWeightPercent: e.EnemyPopulation.ExternalRosterWeightPercent,
			IsAmbientEliteEnabled:     e.EnemyPopulation.AmbientEliteChancePercent > 0,
			AmbientEliteChancePercent: e.EnemyPopulation.AmbientEliteChancePercent,
			OwnedSummonHealthScaling:  e.EnemyPopulation.OwnedSummonHealthScaling,
			FixtureHealthScaling:      e.EnemyPopulation.FixtureHealthScaling,
		}, BossSpawning: boss,
		AllyAlert: game.ZoneAllyAlertRules{RangePercent: 100, RangeOwner: "recipient", MaxHops: 1},
		MutationAgent: game.ZoneMutationAgentRules{MinimumChainLevel: 6, WaveCount: 3, SpawnWave: 3,
			InterWaveDelayMillisecond: 500, IsAgentDefeatCompleting: true},
	}
	if e.AllyAlert != nil {
		rules.AllyAlert = game.ZoneAllyAlertRules{IsEnabled: true,
			RangePercent: e.AllyAlert.RangePercent, RangeOwner: e.AllyAlert.RangeOwner, MaxHops: e.AllyAlert.MaxHops}
	}
	if e.MutationAgent != nil {
		rules.MutationAgent = game.ZoneMutationAgentRules{IsEnabled: true,
			MinimumChainLevel: e.MutationAgent.MinimumChainLevel, WaveCount: e.MutationAgent.WaveCount,
			SpawnWave: e.MutationAgent.SpawnWave, InterWaveDelayMillisecond: e.MutationAgent.InterWaveDelayMillisecond,
			IsAgentDefeatCompleting: e.MutationAgent.Completion == "agent_defeat"}
	}
	return rules
}

func (e zoneRuleConfiguration) validate(key string) error {
	if e.MutationAgent != nil && e.MutationAgent.Completion != "agent_defeat" && e.MutationAgent.Completion != "all_waves" {
		return fmt.Errorf("zoneCompletion[%s]: expected agent_defeat or all_waves", key)
	}
	err := e.rules(key).Validate()
	if err != nil {
		return fmt.Errorf("zoneValidate[%s]: %w", key, err)
	}
	return nil
}

func (e zoneRuleDocument) configuration(key string) zoneRuleConfiguration {
	if key == "tutorial" && e.Tutorial != nil {
		return *e.Tutorial
	}
	if key == "arena" && e.Arena != nil {
		return *e.Arena
	}
	mode, mission, isMission := strings.Cut(key, ":")
	if isMission {
		entries := e.Campaigns
		if mode == "replay" {
			entries = e.Replays
		}
		configuration, isFound := entries[mission]
		if isFound {
			return configuration
		}
	}
	return e.Default
}

func decodeZoneRules(payload []byte) (zoneRuleDocument, error) {
	var document zoneRuleDocument
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&document)
	if err != nil {
		return zoneRuleDocument{}, fmt.Errorf("zoneJSON: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON documents")
		}
		return zoneRuleDocument{}, fmt.Errorf("zoneTrailing: %w", err)
	}
	err = document.Default.validate("default")
	if err != nil {
		return zoneRuleDocument{}, fmt.Errorf("zoneDefault: %w", err)
	}
	missions := make(map[string]bool, 72)
	for stage := uint32(1); stage <= 72; stage++ {
		missions[fmt.Sprintf("%d-%d", (stage-1)/4+1, (stage-1)%4+1)] = true
	}
	groups := map[string]map[string]zoneRuleConfiguration{"campaign": document.Campaigns, "replay": document.Replays}
	for mode, entries := range groups {
		for mission, configuration := range entries {
			if !missions[mission] {
				return zoneRuleDocument{}, fmt.Errorf("zoneMission[%s:%s]: outside campaign", mode, mission)
			}
			err = configuration.validate(mode + ":" + mission)
			if err != nil {
				return zoneRuleDocument{}, fmt.Errorf("zoneOverride: %w", err)
			}
		}
	}
	if document.Tutorial != nil {
		err = document.Tutorial.validate("tutorial")
		if err != nil {
			return zoneRuleDocument{}, fmt.Errorf("zoneTutorial: %w", err)
		}
	}
	if document.Arena != nil {
		err = document.Arena.validate("arena")
		if err != nil {
			return zoneRuleDocument{}, fmt.Errorf("zoneArena: %w", err)
		}
	}
	return document, nil
}

func loadServerRuleStore(ctx context.Context, configPath string) (*serverRuleStore, error) {
	path := filepath.Join(filepath.Dir(configPath), "server.toml")
	legacyPayload, err := os.ReadFile(path)
	isLegacyMissing := errors.Is(err, os.ErrNotExist)
	if err != nil && !isLegacyMissing {
		return nil, fmt.Errorf("legacyRulesRead: %w", err)
	}
	sections := make(map[string]any)
	if !isLegacyMissing {
		metadata, decodeErr := toml.Decode(string(legacyPayload), &sections)
		if decodeErr != nil {
			return nil, fmt.Errorf("legacySections: %w", decodeErr)
		}
		if len(metadata.Keys()) == 0 {
			sections = make(map[string]any)
		}
	}
	shopRules, isShopDefined, err := decodePartShopRules(legacyPayload)
	if err != nil {
		return nil, fmt.Errorf("zoneShop: %w", err)
	}
	document, err := decodeZoneRules(embeddedZoneRules)
	if err != nil {
		return nil, fmt.Errorf("embeddedZoneRules: %w", err)
	}
	// Retire the former global gameplay sections after validating embedded rules.
	isCleanupNeeded := !isShopDefined || isLegacyMissing
	for _, name := range []string{"enemy_population", "ally_alert", "boss_spawning", "mutation_agent"} {
		if _, isFound := sections[name]; isFound {
			delete(sections, name)
			isCleanupNeeded = true
		}
	}
	sections["part_shop"] = partShopSection(shopRules)
	if isCleanupNeeded {
		err = writeServerRuleSections(ctx, path, sections)
		if err != nil {
			return nil, fmt.Errorf("zoneLegacyCleanup: %w", err)
		}
	}
	return &serverRuleStore{
		shopRules: shopRules,
		zoneRules: document,
	}, nil
}

// ZoneRules selects server-owned build data. No runtime file or client endpoint
// can override it; a zone and its checkpoint retain the selected snapshot.
func (e *serverRuleStore) ZoneRules(ctx context.Context, binding game.GameplayBinding) (game.ZoneRules, error) {
	err := ctx.Err()
	if err != nil {
		return game.ZoneRules{}, fmt.Errorf("zoneLoadContext: %w", err)
	}
	key, err := game.ZoneRuleKey(binding)
	if err != nil {
		return game.ZoneRules{}, fmt.Errorf("zoneKey: %w", err)
	}
	configuration := e.zoneRules.configuration(key)
	return configuration.rules(key), nil
}

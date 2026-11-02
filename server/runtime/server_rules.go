package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Bump only when upstream defaults supersede this section's local overrides.
const allyAlertSectionVersion uint32 = 1

// ServerRulesConfiguration is the explicit launcher and persistence DTO.
type ServerRulesConfiguration struct {
	EnemyPopulation EnemyPopulationConfiguration `json:"enemyPopulation" toml:"enemy_population"`
	AllyAlert       AllyAlertConfiguration       `json:"allyAlert" toml:"ally_alert"`
	BossSpawning    BossSpawningConfiguration    `json:"bossSpawning" toml:"boss_spawning"`
	MutationAgent   MutationAgentConfiguration   `json:"mutationAgent" toml:"mutation_agent"`
}

type BossSpawningConfiguration struct {
	LeaderSelection         string `json:"leaderSelection" toml:"leader_selection"`
	ReplayLeaderSelection   string `json:"replayLeaderSelection" toml:"replay_leader_selection"`
	Version                 uint32 `json:"version" toml:"version"`
	OpeningAddsPerAnchor    uint32 `json:"openingAddsPerAnchor" toml:"opening_adds_per_anchor"`
	InitialBossAddCount     uint32 `json:"initialBossAddCount" toml:"initial_boss_add_count"`
	CaptainDelayMillisecond uint32 `json:"captainDelayMillisecond" toml:"captain_delay_millisecond"`
}

func (e BossSpawningConfiguration) rules() game.BossSpawnRules {
	return game.BossSpawnRules{OpeningAddsPerAnchor: e.OpeningAddsPerAnchor,
		InitialBossAddCount: e.InitialBossAddCount, CaptainDelayMillisecond: e.CaptainDelayMillisecond, LeaderSelection: e.LeaderSelection, ReplayLeaderSelection: e.ReplayLeaderSelection}
}

func bossSpawningConfiguration(rules game.BossSpawnRules) BossSpawningConfiguration {
	return BossSpawningConfiguration{Version: 1, OpeningAddsPerAnchor: rules.OpeningAddsPerAnchor,
		InitialBossAddCount: rules.InitialBossAddCount, CaptainDelayMillisecond: rules.CaptainDelayMillisecond, LeaderSelection: rules.LeaderSelection, ReplayLeaderSelection: rules.ReplayLeaderSelection}
}

type AllyAlertConfiguration struct {
	Version                    uint32 `json:"version" toml:"version"`
	IsEnabled                  bool   `json:"isEnabled" toml:"is_enabled"`
	RangePercent               uint32 `json:"rangePercent" toml:"range_percent"`
	RangeOwner                 string `json:"rangeOwner" toml:"range_owner"`
	MaxHops                    uint32 `json:"maxHops" toml:"max_hops"`
	IsDiagnosticLoggingEnabled bool   `json:"isDiagnosticLoggingEnabled" toml:"is_diagnostic_logging_enabled"`
}

func (e AllyAlertConfiguration) rules() zonenpc.AllyAlertRules {
	return zonenpc.AllyAlertRules{
		IsEnabled: e.IsEnabled, RangePercent: e.RangePercent, RangeOwner: e.RangeOwner,
		MaxHops: e.MaxHops, IsDiagnosticLoggingEnabled: e.IsDiagnosticLoggingEnabled,
	}
}

func serverRulesConfiguration(rules zonenpc.AllyAlertRules) ServerRulesConfiguration {
	return ServerRulesConfiguration{EnemyPopulation: enemyPopulationConfiguration(game.DefaultEnemyPopulationRules()), MutationAgent: mutationAgentConfiguration(zonehorde.DefaultPolicy()), BossSpawning: bossSpawningConfiguration(game.DefaultBossSpawnRules()), AllyAlert: AllyAlertConfiguration{
		Version:   allyAlertSectionVersion,
		IsEnabled: rules.IsEnabled, RangePercent: rules.RangePercent, RangeOwner: rules.RangeOwner,
		MaxHops: rules.MaxHops, IsDiagnosticLoggingEnabled: rules.IsDiagnosticLoggingEnabled,
	}}
}

// A single store is shared by every gameplay runtime copy and existing zone.
// Readers snapshot a whole policy; a failed save never changes live rules.
type serverRuleStore struct {
	mu              sync.RWMutex
	path            string
	rules           zonenpc.AllyAlertRules
	spawnRules      game.BossSpawnRules
	populationRules game.EnemyPopulationRules
	hordePolicy     zonehorde.Policy
	shopRules       sporenet.PartShopRules
	sections        map[string]any
}

func (e *serverRuleStore) BossSpawnRules() game.BossSpawnRules {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.spawnRules
}

func (e *serverRuleStore) AllyAlertRules() zonenpc.AllyAlertRules {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.rules
}

func loadServerRuleStore(ctx context.Context, configPath string) (*serverRuleStore, error) {
	path := filepath.Join(filepath.Dir(configPath), "server.toml")
	payload, err := os.ReadFile(path)
	isNew := errors.Is(err, os.ErrNotExist)
	if err != nil && !isNew {
		return nil, fmt.Errorf("rulesRead: %w", err)
	}
	if isNew {
		// Import the earlier [ally_alert] section once. Thereafter server.toml
		// is authoritative, even if the old section remains in darkspin.toml.
		payload, err = os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("legacyRead: %w", err)
		}
	}
	document, isReplaced, err := decodeServerRules(payload, isNew)
	if err != nil {
		return nil, fmt.Errorf("rulesDecode: %w", err)
	}
	mutationConfiguration, isMutationAdded, err := decodeMutationAgentConfiguration(payload)
	if err != nil {
		return nil, fmt.Errorf("mutationRules: %w", err)
	}
	document.MutationAgent = mutationConfiguration
	isReplaced = isReplaced || isMutationAdded
	populationConfiguration, isPopulationAdded, err := decodeEnemyPopulationConfiguration(payload)
	if err != nil {
		return nil, fmt.Errorf("populationLoad: %w", err)
	}
	document.EnemyPopulation = populationConfiguration
	isReplaced = isReplaced || isPopulationAdded
	sections := make(map[string]any)
	if !isNew {
		metadata, decodeErr := toml.Decode(string(payload), &sections)
		if decodeErr != nil {
			return nil, fmt.Errorf("sectionsDecode: %w", decodeErr)
		}
		if !metadata.IsDefined("ally_alert") {
			isReplaced = true
		}
	}
	shopRules, isShopDefined, err := decodePartShopRules(payload)
	if err != nil {
		return nil, fmt.Errorf("partShop: %w", err)
	}
	sections["part_shop"] = partShopSection(shopRules)
	if !isShopDefined {
		isReplaced = true
	}
	rules := document.AllyAlert.rules()
	err = rules.Validate()
	if err != nil {
		return nil, fmt.Errorf("rulesValidate: %w", err)
	}
	if isNew || isReplaced {
		err = writeServerRules(ctx, path, document, sections)
		if err != nil {
			return nil, fmt.Errorf("rulesCreate: %w", err)
		}
	}
	err = document.BossSpawning.rules().Validate()
	if err != nil {
		return nil, fmt.Errorf("spawnValidate: %w", err)
	}
	return &serverRuleStore{path: path, rules: rules, spawnRules: document.BossSpawning.rules(), populationRules: document.EnemyPopulation.rules(), hordePolicy: document.MutationAgent.policy(), shopRules: shopRules, sections: sections}, nil
}

func decodeAllyAlertRules(payload []byte, isLegacy bool) (ServerRulesConfiguration, bool, error) {
	document := serverRulesConfiguration(zonenpc.DefaultAllyAlertRules())
	if isLegacy {
		metadata, err := toml.Decode(string(payload), &document)
		if err != nil {
			return ServerRulesConfiguration{}, false, fmt.Errorf("legacyDecode: %w", err)
		}
		// Other legacy darkspin.toml sections are deliberately left alone.
		if metadata.IsDefined("ally_alert", "version") && document.AllyAlert.Version > allyAlertSectionVersion {
			return ServerRulesConfiguration{}, false, errors.New("ally alert settings require a newer server")
		}
		document.AllyAlert.Version = allyAlertSectionVersion
		return document, true, nil
	}
	var raw struct {
		AllyAlert toml.Primitive `toml:"ally_alert"`
	}
	metadata, err := toml.Decode(string(payload), &raw)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("documentDecode: %w", err)
	}
	if !metadata.IsDefined("ally_alert") {
		return document, true, nil
	}
	var header struct {
		Version uint32 `toml:"version"`
	}
	err = metadata.PrimitiveDecode(raw.AllyAlert, &header)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("versionDecode: %w", err)
	}
	if header.Version > allyAlertSectionVersion {
		return ServerRulesConfiguration{}, false, errors.New("ally alert settings require a newer server")
	}
	if header.Version < allyAlertSectionVersion {
		// Discard the complete obsolete section before decoding its fields:
		// removed keys and old field types must not prevent an upstream reset.
		return document, true, nil
	}
	err = metadata.PrimitiveDecode(raw.AllyAlert, &document.AllyAlert)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("allyAlertDecode: %w", err)
	}
	return document, false, nil
}

func decodeServerRules(payload []byte, isLegacy bool) (ServerRulesConfiguration, bool, error) {
	document, isReplaced, err := decodeAllyAlertRules(payload, isLegacy)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("alertDecode: %w", err)
	}
	var raw struct {
		BossSpawning toml.Primitive `toml:"boss_spawning"`
	}
	metadata, err := toml.Decode(string(payload), &raw)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("spawnDecode: %w", err)
	}
	document.BossSpawning = bossSpawningConfiguration(game.DefaultBossSpawnRules())
	if !metadata.IsDefined("boss_spawning") {
		return document, true, nil
	}
	var header struct {
		Version uint32 `toml:"version"`
	}
	err = metadata.PrimitiveDecode(raw.BossSpawning, &header)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("spawnVersion: %w", err)
	}
	if header.Version > 1 {
		return ServerRulesConfiguration{}, false, errors.New("boss spawning settings require a newer server")
	}
	if header.Version < 1 {
		return document, true, nil
	}
	err = metadata.PrimitiveDecode(raw.BossSpawning, &document.BossSpawning)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("spawnFields: %w", err)
	}
	err = document.BossSpawning.rules().Validate()
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("spawnValidate: %w", err)
	}
	return document, isReplaced || !metadata.IsDefined("boss_spawning", "leader_selection"), nil
}

func writeServerRules(ctx context.Context, path string, document ServerRulesConfiguration, sections map[string]any) (err error) {
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("rulesContext: %w", err)
	}
	// Preserve unrelated sections when resetting or saving this owned section.
	mergedSections := make(map[string]any, len(sections)+1)
	for name, section := range sections {
		mergedSections[name] = section
	}
	mergedSections["ally_alert"] = document.AllyAlert
	mergedSections["boss_spawning"] = document.BossSpawning
	mergedSections["mutation_agent"] = document.MutationAgent
	mergedSections["enemy_population"] = document.EnemyPopulation
	var payload bytes.Buffer
	err = toml.NewEncoder(&payload).Encode(mergedSections)
	if err != nil {
		return fmt.Errorf("rulesEncode: %w", err)
	}
	w, err := os.CreateTemp(filepath.Dir(path), "server-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("rulesTemp: %w", err)
	}
	temporaryPath := w.Name()
	isCommitted := false
	defer func() {
		if isCommitted {
			return
		}
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("rulesCleanup: %w", removeErr))
		}
	}()
	writtenByteCount, writeErr := w.Write(payload.Bytes())
	if writeErr == nil && writtenByteCount != payload.Len() {
		writeErr = errors.New("incomplete server configuration write")
	}
	if writeErr == nil {
		writeErr = w.Sync()
	}
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("rulesWrite: %w", errors.Join(writeErr, closeErr))
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("rulesCommitContext: %w", err)
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("rulesReplace: %w", err)
	}
	isCommitted = true
	return nil
}

func (e *Server) ServerRulesConfiguration() (ServerRulesConfiguration, error) {
	if e == nil || e.serverRuleStore == nil {
		return ServerRulesConfiguration{}, errors.New("server rules unavailable")
	}
	store := e.serverRuleStore
	store.mu.RLock()
	defer store.mu.RUnlock()
	document := serverRulesConfiguration(store.rules)
	document.BossSpawning = bossSpawningConfiguration(store.spawnRules)
	document.MutationAgent = mutationAgentConfiguration(store.hordePolicy)
	document.EnemyPopulation = enemyPopulationConfiguration(store.populationRules)
	return document, nil
}

// SetServerRulesConfiguration persists before publishing the new policy.
// Alerts update immediately; spawn policies are captured when a zone is created.
func (e *Server) SetServerRulesConfiguration(
	ctx context.Context, req ServerRulesConfiguration,
) (ServerRulesConfiguration, error) {
	if e == nil || e.serverRuleStore == nil {
		return ServerRulesConfiguration{}, errors.New("server rules unavailable")
	}
	if req.AllyAlert.Version != allyAlertSectionVersion || req.BossSpawning.Version != 1 {
		return ServerRulesConfiguration{}, errors.New("server settings version changed; reload before saving")
	}
	rules := req.AllyAlert.rules()
	err := rules.Validate()
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("rulesValidate: %w", err)
	}
	store := e.serverRuleStore
	store.mu.Lock()
	defer store.mu.Unlock()
	if req.BossSpawning.LeaderSelection == "" {
		req.BossSpawning.LeaderSelection = store.spawnRules.LeaderSelection
		req.BossSpawning.ReplayLeaderSelection = store.spawnRules.ReplayLeaderSelection
	}
	spawnErr := req.BossSpawning.rules().Validate()
	if spawnErr != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("spawnValidate: %w", spawnErr)
	}
	// Compatibility with an older form must not erase an unsupported version.
	if req.EnemyPopulation.Version != 0 && req.EnemyPopulation.Version != 1 {
		return ServerRulesConfiguration{}, errors.New("enemy population settings version changed; reload before saving")
	}
	if req.EnemyPopulation.Version == 1 && req.EnemyPopulation.AgentSelection == "" {
		enemiesPerSection := req.EnemyPopulation.EnemiesPerSection
		req.EnemyPopulation = enemyPopulationConfiguration(store.populationRules)
		req.EnemyPopulation.EnemiesPerSection = enemiesPerSection
	}
	if req.EnemyPopulation.Version == 0 {
		req.EnemyPopulation = enemyPopulationConfiguration(store.populationRules)
	}
	if req.EnemyPopulation.Version != 1 {
		return ServerRulesConfiguration{}, errors.New("enemy population settings version changed; reload before saving")
	}
	err = req.EnemyPopulation.rules().Validate()
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("populationValidate: %w", err)
	}
	// Older launcher forms do not submit this section. Preserve it rather
	// than resetting operator choices when unrelated settings are saved.
	if req.MutationAgent.Version == 0 {
		req.MutationAgent = mutationAgentConfiguration(store.hordePolicy)
	}
	if req.MutationAgent.Version != 1 {
		return ServerRulesConfiguration{}, errors.New("mutation agent settings version changed; reload before saving")
	}
	err = req.MutationAgent.policy().Validate()
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("mutationValidate: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("rulesContext: %w", err)
	}
	err = writeServerRules(ctx, store.path, req, store.sections)
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("rulesSave: %w", err)
	}
	store.rules = rules
	store.spawnRules = req.BossSpawning.rules()
	store.hordePolicy = req.MutationAgent.policy()
	store.populationRules = req.EnemyPopulation.rules()
	e.logger.Printf("Enemy population policy saved for new missions enemies_per_section=%d",
		store.populationRules.EnemiesPerSection)
	e.logger.Printf("Mutation Agent horde policy saved for new missions enabled=%t minimum_chain_level=%d waves=%d spawn_wave=%d delay_ms=%d agent_defeat_completes=%t",
		store.hordePolicy.IsMutationAgentEnabled, store.hordePolicy.MutationAgentMinChainLevel,
		store.hordePolicy.WaveCount, store.hordePolicy.MutationAgentWaveOrdinal,
		req.MutationAgent.InterWaveDelayMillisecond, store.hordePolicy.IsMutationAgentDefeatCompleting)
	e.logger.Printf("Boss spawn policy saved for new missions opening_adds_per_anchor=%d initial_boss_add_count=%d captain_delay_ms=%d",
		store.spawnRules.OpeningAddsPerAnchor, store.spawnRules.InitialBossAddCount,
		store.spawnRules.CaptainDelayMillisecond)
	e.logger.Printf("NPC ally alert policy applied enabled=%t range_percent=%d range_owner=%s max_hops=%d diagnostics=%t",
		rules.IsEnabled, rules.RangePercent, rules.RangeOwner, rules.MaxHops, rules.IsDiagnosticLoggingEnabled)
	return req, nil
}

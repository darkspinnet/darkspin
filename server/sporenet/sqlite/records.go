package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

const insertUserQuery = `
	INSERT INTO user (
		login_name, display_name, password, create_dt, last_connection_dt,
		is_tutorial_completion_pending,
		is_all_access_granted, is_online_access_granted,
		is_overdrive_unlocked,
		chain_progression, creature_reward, current_game_id,
		current_playgroup_id, default_deck_pve_id, default_deck_pvp_id,
		level, xp, dna, avatar_id, new_player_inventory, onboarding_progress,
		cashout_bonus_time, star_level, unlock_catalyst,
		unlock_diagonal_catalyst, unlock_inventory, unlock_fuel_tank,
		unlock_pve_deck, unlock_pvp_deck, unlock_stat,
		unlock_inventory_identify, unlock_editor_flair_slot, upsell,
		cap_level, cap_progression,
		limited_edition_miss_count, limited_edition_used_mask
	) VALUES (
		:login_name, :display_name, :password, :create_dt, :last_connection_dt,
		:is_tutorial_completion_pending,
		:is_all_access_granted, :is_online_access_granted,
		:is_overdrive_unlocked,
		:chain_progression, :creature_reward, :current_game_id,
		:current_playgroup_id, :default_deck_pve_id, :default_deck_pvp_id,
		:level, :xp, :dna, :avatar_id, :new_player_inventory, :onboarding_progress,
		:cashout_bonus_time, :star_level, :unlock_catalyst,
		:unlock_diagonal_catalyst, :unlock_inventory, :unlock_fuel_tank,
		:unlock_pve_deck, :unlock_pvp_deck, :unlock_stat,
		:unlock_inventory_identify, :unlock_editor_flair_slot, :upsell,
		:cap_level, :cap_progression,
		:limited_edition_miss_count, :limited_edition_used_mask
	)`

const updateUserQuery = `
	UPDATE user SET
		display_name = :display_name,
		password = :password,
		last_connection_dt = :last_connection_dt,
		is_tutorial_completion_pending = :is_tutorial_completion_pending,
		is_all_access_granted = :is_all_access_granted,
		is_online_access_granted = :is_online_access_granted,
		is_overdrive_unlocked = :is_overdrive_unlocked,
		chain_progression = :chain_progression,
		creature_reward = :creature_reward,
		current_game_id = :current_game_id,
		current_playgroup_id = :current_playgroup_id,
		default_deck_pve_id = :default_deck_pve_id,
		default_deck_pvp_id = :default_deck_pvp_id,
		level = :level,
		xp = :xp,
		dna = :dna,
		avatar_id = :avatar_id,
		new_player_inventory = :new_player_inventory,
		onboarding_progress = :onboarding_progress,
		cashout_bonus_time = :cashout_bonus_time,
		star_level = :star_level,
		unlock_catalyst = :unlock_catalyst,
		unlock_diagonal_catalyst = :unlock_diagonal_catalyst,
		unlock_inventory = :unlock_inventory,
		unlock_fuel_tank = :unlock_fuel_tank,
		unlock_pve_deck = :unlock_pve_deck,
		unlock_pvp_deck = :unlock_pvp_deck,
		unlock_stat = :unlock_stat,
		unlock_inventory_identify = :unlock_inventory_identify,
		unlock_editor_flair_slot = :unlock_editor_flair_slot,
		upsell = :upsell,
		cap_level = :cap_level,
		cap_progression = :cap_progression,
		limited_edition_miss_count = :limited_edition_miss_count,
		limited_edition_used_mask = :limited_edition_used_mask
	WHERE login_name = :login_name`

func clearDependents(ctx context.Context, tx *sqlx.Tx, userID int64) error {
	tables := []string{
		"setting", "user_stat", "user_event", "campaign_experience",
		"user_association", "squad", "creature", "part", "extension",
	}
	for index, table := range tables {
		_, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE user_id = ?`, userID)
		if err != nil {
			return fmt.Errorf("clearTable[%d]: %w", index, err)
		}
	}
	return nil
}

func writeDependents(ctx context.Context, tx *sqlx.Tx, record sporenet.UserRecord) error {
	var err error
	_, err = tx.NamedExecContext(ctx, `
		INSERT INTO user_stat (
			user_id, pve_play_time_second, pve_minion_kill, pve_special_kill,
			pve_boss_kill, pve_total_kill, pve_death, pve_damage_dealt,
			pve_damage_taken, pve_damage_maximum, pve_healing,
			pve_healing_received, pve_healing_maximum, pvp_play_time_second,
			pvp_win, pvp_loss, pvp_player_kill, pvp_death, pvp_damage_dealt,
			pvp_damage_taken, pvp_damage_maximum, pvp_healing,
			pvp_healing_received, pvp_healing_maximum
		) VALUES (
			:user_id, :pve_play_time_second, :pve_minion_kill, :pve_special_kill,
			:pve_boss_kill, :pve_total_kill, :pve_death, :pve_damage_dealt,
			:pve_damage_taken, :pve_damage_maximum, :pve_healing,
			:pve_healing_received, :pve_healing_maximum, :pvp_play_time_second,
			:pvp_win, :pvp_loss, :pvp_player_kill, :pvp_death, :pvp_damage_dealt,
			:pvp_damage_taken, :pvp_damage_maximum, :pvp_healing,
			:pvp_healing_received, :pvp_healing_maximum
		)`, userStatRowFromRecord(record))
	if err != nil {
		return fmt.Errorf("userStatInsert: %w", err)
	}
	for key, value := range record.Settings {
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO setting (user_id, key, value)
			VALUES (:user_id, :key, :value)`, settingRow{
			UserID: record.Account.ID, Key: key, Value: value,
		})
		if err != nil {
			return fmt.Errorf("settingInsert[%q]: %w", key, err)
		}
	}
	for index, event := range record.Events {
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO user_event (user_id, key, message_id, metadata, occurred_at, is_public)
			VALUES (:user_id, :key, :message_id, :metadata, :occurred_at, :is_public)`, userEventRow{
			UserID: record.Account.ID, Key: event.Key, MessageID: event.MessageID,
			Metadata: event.Metadata, OccurredAt: event.OccurredAt, IsPublic: boolInteger(event.IsPublic),
		})
		if err != nil {
			return fmt.Errorf("userEventInsert[%d]: %w", index, err)
		}
	}
	for index, receipt := range record.CampaignExperiences {
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO campaign_experience (
				user_id, result_id, amount, cumulative_xp, level
			) VALUES (
				:user_id, :result_id, :amount, :cumulative_xp, :level
			)`, campaignExperienceRow{
			UserID: record.Account.ID, ResultID: receipt.ResultID,
			Amount: receipt.Amount, CumulativeXP: receipt.CumulativeXP,
			Level: receipt.Level,
		})
		if err != nil {
			return fmt.Errorf("campaignExperienceInsert[%d]: %w", index, err)
		}
	}
	for listType, members := range record.Associations {
		for index, member := range members {
			_, err = tx.NamedExecContext(ctx, `
				INSERT INTO user_association (user_id, list_type, member_id, name, associated_at)
				VALUES (:user_id, :list_type, :member_id, :name, :associated_at)`, userAssociationRow{
				UserID: record.Account.ID, ListType: listType, MemberID: member.ID,
				Name: member.Name, AssociatedAt: member.Time,
			})
			if err != nil {
				return fmt.Errorf("userAssociationInsert[%d/%d]: %w", listType, index, err)
			}
		}
	}
	for squadIndex, squad := range record.Squads {
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO squad (id, user_id, name, category, slot, is_locked)
			VALUES (:id, :user_id, :name, :category, :slot, :is_locked)`, squadRow{
			ID: squad.ID, UserID: record.Account.ID, Name: squad.Name,
			Category: squad.Category, Slot: squad.Slot, IsLocked: boolInteger(squad.IsLocked),
		})
		if err != nil {
			return fmt.Errorf("squadInsert[%d]: %w", squadIndex, err)
		}
		for position, creatureID := range squad.CreatureIDs {
			if creatureID == 0 {
				continue
			}
			_, err = tx.NamedExecContext(ctx, `
				INSERT INTO squad_creature (user_id, squad_id, creature_id, position)
				VALUES (:user_id, :squad_id, :creature_id, :position)`, squadCreatureRow{
				UserID: record.Account.ID, SquadID: squad.ID,
				CreatureID: creatureID, Position: uint32(position),
			})
			if err != nil {
				return fmt.Errorf("squadCreature[%d/%d]: %w", squadIndex, position, err)
			}
		}
	}
	for index, creature := range record.Creatures {
		if creature == nil {
			continue
		}
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO creature (
				id, user_id, template_name, version, gear_score,
				item_point, large_image_url, thumb_image_url, creator_id
			) VALUES (
				:id, :user_id, :template_name, :version, :gear_score,
				:item_point, :large_image_url, :thumb_image_url, :creator_id
			)`, creatureRow{
			ID: creature.ID, UserID: record.Account.ID, TemplateName: creature.TemplateName,
			Version: creature.Version, GearScore: creature.GearScore, ItemPoints: creature.ItemPoints,
			LargeImage: creature.LargeImageURL, ThumbImage: creature.ThumbImageURL, CreatorID: creature.CreatorID,
		})
		if err != nil {
			return fmt.Errorf("creatureInsert[%d]: %w", index, err)
		}
		for statPosition, stat := range creature.Stats {
			_, err = tx.NamedExecContext(ctx, `
				INSERT INTO creature_stat (
					user_id, creature_id, position, name, maximum, current
				) VALUES (
					:user_id, :creature_id, :position, :name, :maximum, :current
				)`, creatureStatRow{
				UserID: record.Account.ID, CreatureID: creature.ID, Position: statPosition,
				Name: stat.Name, Maximum: stat.Maximum, Current: stat.Current,
			})
			if err != nil {
				return fmt.Errorf("creatureStat[%d/%d]: %w", index, statPosition, err)
			}
		}
		for statPosition, stat := range creature.AbilityStats {
			_, err = tx.NamedExecContext(ctx, `
				INSERT INTO creature_ability_stat (
					user_id, creature_id, position, ability_key, token, token_value
				) VALUES (
					:user_id, :creature_id, :position, :ability_key, :token, :token_value
				)`, creatureAbilityStatRow{
				UserID: record.Account.ID, CreatureID: creature.ID, Position: statPosition,
				AbilityKey: stat.Key, Token: stat.Token, TokenValue: stat.Value,
			})
			if err != nil {
				return fmt.Errorf("creatureAbilityStat[%d/%d]: %w", index, statPosition, err)
			}
		}
	}
	for index, part := range record.Parts {
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO part (
				user_id, position, item_id, reference_id, is_flair, cost, creature_id, level,
				market_status, rarity, status, usage, creation_date,
				rigblock_asset_id, prefix_asset_id, prefix_secondary_asset_id,
				suffix_asset_id
			) VALUES (
				:user_id, :position, :item_id, :reference_id, :is_flair, :cost, :creature_id, :level,
				:market_status, :rarity, :status, :usage, :creation_date,
				:rigblock_asset_id, :prefix_asset_id, :prefix_secondary_asset_id,
				:suffix_asset_id
			)`, partRow{
			UserID: record.Account.ID, Position: index, ItemID: part.ID,
			ReferenceID: part.ReferenceID, IsFlair: boolInteger(part.IsFlair),
			Cost: part.Cost, CreatureID: part.EquippedToCreatureID, Level: part.Level,
			MarketStatus: part.MarketStatus, Rarity: part.Rarity, Status: part.Status,
			Usage: part.Usage, CreationDate: part.CreationDate, RigblockAssetID: part.RigblockAssetID,
			PrefixAssetID: part.PrefixAssetID, PrefixSecondaryAssetID: part.PrefixSecondaryAssetID,
			SuffixAssetID: part.SuffixAssetID,
		})
		if err != nil {
			return fmt.Errorf("partInsert[%d]: %w", index, err)
		}
	}
	for index, extension := range record.Extensions {
		_, err = tx.NamedExecContext(ctx, `
			INSERT INTO extension (user_id, position, name, value)
			VALUES (:user_id, :position, :name, :value)`, extensionRow{
			UserID: record.Account.ID, Position: index, Name: extension.Name, Value: extension.Value,
		})
		if err != nil {
			return fmt.Errorf("extensionInsert[%d]: %w", index, err)
		}
	}
	return nil
}

func loadRecord(ctx context.Context, tx *sqlx.Tx, loginName string) (sporenet.UserRecord, error) {
	user := userRow{}
	err := tx.GetContext(ctx, &user, `SELECT * FROM user WHERE login_name = ? COLLATE NOCASE`, loginName)
	if errors.Is(err, sql.ErrNoRows) {
		return sporenet.UserRecord{}, fmt.Errorf("userMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("userSelect: %w", err)
	}
	createDT, err := time.Parse(time.RFC3339Nano, user.CreateDT)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("userCreateDT: %w", err)
	}
	lastConnectionDT := time.Time{}
	if user.LastConnectionDT != "" {
		lastConnectionDT, err = time.Parse(time.RFC3339Nano, user.LastConnectionDT)
		if err != nil {
			return sporenet.UserRecord{}, fmt.Errorf("userLastConnectionDT: %w", err)
		}
	}
	record := sporenet.UserRecord{
		DisplayName: user.DisplayName, LoginName: user.LoginName, Password: user.Password,
		CreateDT:                    createDT,
		LastConnectionDT:            lastConnectionDT,
		IsTutorialCompletionPending: user.IsTutorialCompletionPending != 0,
		Account:                     user.account(), Associations: make(map[uint32][]sporenet.AssociationMember),
		Settings: make(map[string]string),
	}
	stat := userStatRow{}
	err = tx.GetContext(ctx, &stat, `SELECT * FROM user_stat WHERE user_id = ?`, user.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sporenet.UserRecord{}, fmt.Errorf("userStatSelect: %w", err)
	}
	if err == nil {
		record.Stats = stat.stats()
	}
	settings := []settingRow{}
	err = tx.SelectContext(ctx, &settings, `SELECT user_id, key, value FROM setting WHERE user_id = ? ORDER BY key`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("settingsSelect: %w", err)
	}
	for _, setting := range settings {
		record.Settings[setting.Key] = setting.Value
	}
	events := []userEventRow{}
	err = tx.SelectContext(ctx, &events, `
		SELECT user_id, key, message_id, metadata, occurred_at, is_public
		FROM user_event WHERE user_id = ? ORDER BY occurred_at DESC, key`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("userEventsSelect: %w", err)
	}
	for _, event := range events {
		record.Events = append(record.Events, sporenet.UserEvent{
			Key: event.Key, MessageID: event.MessageID, Metadata: event.Metadata,
			OccurredAt: event.OccurredAt, IsPublic: event.IsPublic != 0,
		})
	}
	campaignExperiences := []campaignExperienceRow{}
	err = tx.SelectContext(ctx, &campaignExperiences, `
		SELECT user_id, result_id, amount, cumulative_xp, level
		FROM campaign_experience WHERE user_id = ? ORDER BY result_id`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("campaignExperienceSelect: %w", err)
	}
	for _, receipt := range campaignExperiences {
		record.CampaignExperiences = append(
			record.CampaignExperiences, sporenet.CampaignExperience{
				ResultID: receipt.ResultID, Amount: receipt.Amount,
				CumulativeXP: receipt.CumulativeXP, Level: receipt.Level,
			},
		)
	}
	associations := []userAssociationRow{}
	err = tx.SelectContext(ctx, &associations, `
		SELECT user_id, list_type, member_id, name, associated_at
		FROM user_association WHERE user_id = ? ORDER BY list_type, associated_at, member_id`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("userAssociationsSelect: %w", err)
	}
	for _, association := range associations {
		record.Associations[association.ListType] = append(
			record.Associations[association.ListType], sporenet.AssociationMember{
				ID: association.MemberID, Name: association.Name, Time: association.AssociatedAt,
			},
		)
	}
	squads := []squadRow{}
	err = tx.SelectContext(ctx, &squads, `SELECT * FROM squad WHERE user_id = ? ORDER BY slot, id`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("squadsSelect: %w", err)
	}
	squadIndexes := make(map[uint32]int, len(squads))
	for _, row := range squads {
		squadIndexes[row.ID] = len(record.Squads)
		record.Squads = append(record.Squads, sporenet.Squad{
			Name: row.Name, Category: row.Category, ID: row.ID,
			Slot: row.Slot, IsLocked: row.IsLocked != 0,
		})
	}
	squadCreatures := []squadCreatureRow{}
	err = tx.SelectContext(ctx, &squadCreatures, `
		SELECT user_id, squad_id, creature_id, position
		FROM squad_creature WHERE user_id = ? ORDER BY squad_id, position`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("squadCreatures: %w", err)
	}
	for _, row := range squadCreatures {
		index, isFound := squadIndexes[row.SquadID]
		if !isFound || row.Position >= uint32(len(record.Squads[index].CreatureIDs)) {
			continue
		}
		record.Squads[index].CreatureIDs[row.Position] = row.CreatureID
	}
	creatures := []creatureRow{}
	err = tx.SelectContext(ctx, &creatures, `SELECT * FROM creature WHERE user_id = ? ORDER BY id`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("creaturesSelect: %w", err)
	}
	creatureByID := make(map[uint32]*sporenet.Creature, len(creatures))
	for _, row := range creatures {
		creature := &sporenet.Creature{
			TemplateName: row.TemplateName, ID: row.ID, Version: row.Version,
			GearScore: row.GearScore, ItemPoints: row.ItemPoints,
			LargeImageURL: row.LargeImage, ThumbImageURL: row.ThumbImage, CreatorID: row.CreatorID,
		}
		record.Creatures = append(record.Creatures, creature)
		creatureByID[row.ID] = creature
	}
	creatureStats := []creatureStatRow{}
	err = tx.SelectContext(ctx, &creatureStats, `
		SELECT user_id, creature_id, position, name, maximum, current
		FROM creature_stat WHERE user_id = ? ORDER BY creature_id, position`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("creatureStats: %w", err)
	}
	for _, row := range creatureStats {
		creature := creatureByID[row.CreatureID]
		if creature == nil {
			continue
		}
		creature.Stats = append(creature.Stats, sporenet.Stat{
			Name: row.Name, Maximum: row.Maximum, Current: row.Current,
		})
	}
	creatureAbilityStats := []creatureAbilityStatRow{}
	err = tx.SelectContext(ctx, &creatureAbilityStats, `
		SELECT user_id, creature_id, position, ability_key, token, token_value
		FROM creature_ability_stat WHERE user_id = ? ORDER BY creature_id, position`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("creatureAbilityStats: %w", err)
	}
	for _, row := range creatureAbilityStats {
		creature := creatureByID[row.CreatureID]
		if creature == nil {
			continue
		}
		creature.AbilityStats = append(creature.AbilityStats, sporenet.AbilityStat{
			Key: row.AbilityKey, Token: row.Token, Value: row.TokenValue,
		})
	}
	parts := []partRow{}
	err = tx.SelectContext(ctx, &parts, `SELECT * FROM part WHERE user_id = ? ORDER BY position`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("partsSelect: %w", err)
	}
	for _, row := range parts {
		record.Parts = append(record.Parts, sporenet.Part{
			ID: row.ItemID, ReferenceID: row.ReferenceID,
			IsFlair: row.IsFlair != 0, Cost: row.Cost, EquippedToCreatureID: row.CreatureID,
			Level: row.Level, MarketStatus: row.MarketStatus, Rarity: row.Rarity,
			Status: row.Status, Usage: row.Usage, CreationDate: row.CreationDate,
			RigblockAssetID: row.RigblockAssetID, PrefixAssetID: row.PrefixAssetID,
			PrefixSecondaryAssetID: row.PrefixSecondaryAssetID, SuffixAssetID: row.SuffixAssetID,
		})
	}
	extensions := []extensionRow{}
	err = tx.SelectContext(ctx, &extensions, `SELECT * FROM extension WHERE user_id = ? ORDER BY position`, user.ID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("extensionsSelect: %w", err)
	}
	for _, row := range extensions {
		record.Extensions = append(record.Extensions, sporenet.OpaqueField{
			Name: row.Name, Value: append([]byte(nil), row.Value...),
		})
	}
	return record, nil
}

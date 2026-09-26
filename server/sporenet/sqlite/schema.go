package sqlite

const chatLogTimeFormat = "2006-01-02T15:04:05.999999999Z07:00"

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS user (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		login_name TEXT NOT NULL COLLATE NOCASE UNIQUE,
		display_name TEXT NOT NULL,
		password TEXT NOT NULL,
		create_dt TEXT NOT NULL,
		last_connection_dt TEXT NOT NULL DEFAULT '',
		is_tutorial_completion_pending INTEGER NOT NULL DEFAULT 0,
		is_all_access_granted INTEGER NOT NULL DEFAULT 1,
		is_online_access_granted INTEGER NOT NULL DEFAULT 1,
		is_overdrive_unlocked INTEGER NOT NULL DEFAULT 0,
		chain_progression INTEGER NOT NULL DEFAULT 0,
		creature_reward INTEGER NOT NULL DEFAULT 0,
		current_game_id INTEGER NOT NULL DEFAULT 0,
		current_playgroup_id INTEGER NOT NULL DEFAULT 0,
		default_deck_pve_id INTEGER NOT NULL DEFAULT 1,
		default_deck_pvp_id INTEGER NOT NULL DEFAULT 1,
		level INTEGER NOT NULL DEFAULT 1,
		xp INTEGER NOT NULL DEFAULT 0,
		dna INTEGER NOT NULL DEFAULT 0,
		avatar_id INTEGER NOT NULL DEFAULT 0,
		new_player_inventory INTEGER NOT NULL DEFAULT 0,
		onboarding_progress INTEGER NOT NULL DEFAULT 0,
		cashout_bonus_time INTEGER NOT NULL DEFAULT 0,
		star_level INTEGER NOT NULL DEFAULT 0,
		unlock_catalyst INTEGER NOT NULL DEFAULT 0,
		unlock_diagonal_catalyst INTEGER NOT NULL DEFAULT 0,
		unlock_inventory INTEGER NOT NULL DEFAULT 0,
		unlock_fuel_tank INTEGER NOT NULL DEFAULT 0,
		unlock_pve_deck INTEGER NOT NULL DEFAULT 0,
		unlock_pvp_deck INTEGER NOT NULL DEFAULT 0,
		unlock_stat INTEGER NOT NULL DEFAULT 0,
		unlock_inventory_identify INTEGER NOT NULL DEFAULT 180,
		unlock_editor_flair_slot INTEGER NOT NULL DEFAULT 0,
		upsell INTEGER NOT NULL DEFAULT 0,
		cap_level INTEGER NOT NULL DEFAULT 0,
		cap_progression INTEGER NOT NULL DEFAULT 0,
		limited_edition_miss_count INTEGER NOT NULL DEFAULT 0,
		limited_edition_used_mask INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS setting (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (user_id, key)
	)`,
	`CREATE TABLE IF NOT EXISTS user_stat (
		user_id INTEGER PRIMARY KEY REFERENCES user(id) ON DELETE CASCADE,
		pve_play_time_second INTEGER NOT NULL DEFAULT 0,
		pve_minion_kill INTEGER NOT NULL DEFAULT 0,
		pve_special_kill INTEGER NOT NULL DEFAULT 0,
		pve_boss_kill INTEGER NOT NULL DEFAULT 0,
		pve_total_kill INTEGER NOT NULL DEFAULT 0,
		pve_death INTEGER NOT NULL DEFAULT 0,
		pve_damage_dealt REAL NOT NULL DEFAULT 0,
		pve_damage_taken REAL NOT NULL DEFAULT 0,
		pve_damage_maximum REAL NOT NULL DEFAULT 0,
		pve_healing REAL NOT NULL DEFAULT 0,
		pve_healing_received REAL NOT NULL DEFAULT 0,
		pve_healing_maximum REAL NOT NULL DEFAULT 0,
		pvp_play_time_second INTEGER NOT NULL DEFAULT 0,
		pvp_win INTEGER NOT NULL DEFAULT 0,
		pvp_loss INTEGER NOT NULL DEFAULT 0,
		pvp_player_kill INTEGER NOT NULL DEFAULT 0,
		pvp_death INTEGER NOT NULL DEFAULT 0,
		pvp_damage_dealt REAL NOT NULL DEFAULT 0,
		pvp_damage_taken REAL NOT NULL DEFAULT 0,
		pvp_damage_maximum REAL NOT NULL DEFAULT 0,
		pvp_healing REAL NOT NULL DEFAULT 0,
		pvp_healing_received REAL NOT NULL DEFAULT 0,
		pvp_healing_maximum REAL NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS user_event (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		message_id INTEGER NOT NULL,
		metadata TEXT NOT NULL,
		occurred_at INTEGER NOT NULL,
		is_public INTEGER NOT NULL,
		PRIMARY KEY (user_id, key)
	)`,
	`CREATE TABLE IF NOT EXISTS campaign_experience (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		result_id INTEGER NOT NULL,
		amount INTEGER NOT NULL,
		cumulative_xp INTEGER NOT NULL,
		level INTEGER NOT NULL,
		PRIMARY KEY (user_id, result_id)
	)`,
	`CREATE TABLE IF NOT EXISTS user_association (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		list_type INTEGER NOT NULL,
		member_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		associated_at INTEGER NOT NULL,
		PRIMARY KEY (user_id, list_type, member_id)
	)`,
	`CREATE TABLE IF NOT EXISTS squad (
		id INTEGER NOT NULL,
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		slot INTEGER NOT NULL,
		is_locked INTEGER NOT NULL,
		PRIMARY KEY (id, user_id)
	)`,
	`CREATE TABLE IF NOT EXISTS squad_creature (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		squad_id INTEGER NOT NULL,
		creature_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		PRIMARY KEY (user_id, squad_id, position),
		FOREIGN KEY (squad_id, user_id) REFERENCES squad(id, user_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS creature (
		id INTEGER NOT NULL,
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		template_name TEXT NOT NULL,
		version INTEGER NOT NULL,
		gear_score REAL NOT NULL,
		item_point REAL NOT NULL,
		large_image_url TEXT NOT NULL,
		thumb_image_url TEXT NOT NULL,
		creator_id INTEGER NOT NULL,
		PRIMARY KEY (id, user_id)
	)`,
	`CREATE TABLE IF NOT EXISTS creature_stat (
		user_id INTEGER NOT NULL,
		creature_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		name TEXT NOT NULL,
		maximum INTEGER NOT NULL,
		current INTEGER NOT NULL,
		PRIMARY KEY (user_id, creature_id, position),
		FOREIGN KEY (creature_id, user_id) REFERENCES creature(id, user_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS creature_ability_stat (
		user_id INTEGER NOT NULL,
		creature_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		ability_key TEXT NOT NULL,
		token TEXT NOT NULL,
		token_value TEXT NOT NULL,
		PRIMARY KEY (user_id, creature_id, position),
		FOREIGN KEY (creature_id, user_id) REFERENCES creature(id, user_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS part (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		position INTEGER NOT NULL,
		item_id INTEGER NOT NULL DEFAULT 0,
		reference_id INTEGER NOT NULL DEFAULT 0,
		is_flair INTEGER NOT NULL,
		cost INTEGER NOT NULL,
		creature_id INTEGER NOT NULL,
		level INTEGER NOT NULL,
		market_status INTEGER NOT NULL,
		rarity INTEGER NOT NULL,
		status INTEGER NOT NULL,
		usage INTEGER NOT NULL,
		creation_date INTEGER NOT NULL,
		rigblock_asset_id INTEGER NOT NULL,
		prefix_asset_id INTEGER NOT NULL,
		prefix_secondary_asset_id INTEGER NOT NULL,
		suffix_asset_id INTEGER NOT NULL,
		PRIMARY KEY (user_id, position)
	)`,
	`CREATE TABLE IF NOT EXISTS extension (
		user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
		position INTEGER NOT NULL,
		name TEXT NOT NULL,
		value BLOB NOT NULL,
		PRIMARY KEY (user_id, position)
	)`,
	`CREATE TABLE IF NOT EXISTS chat_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id INTEGER NOT NULL,
		sender_id INTEGER NOT NULL,
		sender_name TEXT NOT NULL,
		scope INTEGER NOT NULL,
		target_id INTEGER NOT NULL,
		body TEXT NOT NULL,
		recipient TEXT NOT NULL,
		sent_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS creature_user_id ON creature(user_id)`,
	`CREATE INDEX IF NOT EXISTS squad_user_id ON squad(user_id)`,
	`CREATE INDEX IF NOT EXISTS user_event_occurred_at ON user_event(user_id, occurred_at)`,
	`CREATE INDEX IF NOT EXISTS user_association_member ON user_association(member_id, list_type)`,
	`INSERT OR IGNORE INTO user_event (user_id, key, message_id, metadata, occurred_at, is_public)
		SELECT id, 'level:4', 4, 'Reached Crogenitor level 4.', CAST(strftime('%s', 'now') AS INTEGER), 1
		FROM user WHERE level >= 4`,
	`INSERT OR IGNORE INTO user_event (user_id, key, message_id, metadata, occurred_at, is_public)
		SELECT id, 'hero-available:arakna', 4, 'Arakna, the Scout Collector, is now available.', CAST(strftime('%s', 'now') AS INTEGER), 1
		FROM user WHERE level >= 4`,
	`INSERT OR IGNORE INTO user_event (user_id, key, message_id, metadata, occurred_at, is_public)
		SELECT id, 'hero-available:vex', 4, 'Vex, the Chrono Shifter, is now available.', CAST(strftime('%s', 'now') AS INTEGER), 1
		FROM user WHERE level >= 4`,
	`INSERT OR IGNORE INTO user_event (user_id, key, message_id, metadata, occurred_at, is_public)
		SELECT id, 'hero-available:viper', 4, 'Viper, the Toxic Ravager, is now available.', CAST(strftime('%s', 'now') AS INTEGER), 1
		FROM user WHERE level >= 4`,
	`INSERT OR IGNORE INTO user_event (user_id, key, message_id, metadata, occurred_at, is_public)
		SELECT id, 'upgrade-available:hero-genetic', 4, 'A hero genetic upgrade is now available in the Upgrade Store.', CAST(strftime('%s', 'now') AS INTEGER), 1
		FROM user WHERE level >= 4`,
	`CREATE INDEX IF NOT EXISTS chat_log_sent_at ON chat_log(sent_at)`,
	`CREATE INDEX IF NOT EXISTS chat_log_sender_id ON chat_log(sender_id)`,
	`CREATE INDEX IF NOT EXISTS chat_log_target ON chat_log(scope, target_id)`,
}

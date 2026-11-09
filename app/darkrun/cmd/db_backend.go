package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
	"github.com/jmoiron/sqlx"
)

func (e *databaseTarget) bind(query string) string {
	if e.database.DriverName() == "pgx" {
		query = strings.ReplaceAll(query, " LIKE ?", " ILIKE ?")
	}
	return e.database.Rebind(query)
}

func openConfiguredDatabaseTarget(
	ctx context.Context, runtimePath, contentPath, table string,
) (*databaseTarget, error) {
	if !playerstore.IsEnvironmentConfigured() {
		target, err := openDatabaseTarget(ctx, runtimePath, contentPath, table)
		if err != nil {
			return nil, fmt.Errorf("sqliteTarget: %w", err)
		}
		return target, nil
	}
	configuration, err := playerstore.FromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("postgresEnvironment: %w", err)
	}
	// Explicit routing keeps old local saves from shadowing remote profiles.
	if !isPlayerTable(table) {
		database, openErr := openExistingSQLite(ctx, filepath.Join(contentPath, server.ContentDatabaseFilename))
		if openErr != nil {
			return nil, fmt.Errorf("contentOpen: %w", openErr)
		}
		return &databaseTarget{database: sqlx.NewDb(database, "sqlite"), name: "content"}, nil
	}
	openContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	database, err := playerstore.OpenPostgresPool(openContext, configuration)
	if err != nil {
		return nil, fmt.Errorf("postgresOpen: %w", err)
	}
	return &databaseTarget{database: database, name: "user"}, nil
}

func isPlayerTable(table string) bool {
	switch table {
	case "user", "setting", "user_stat", "user_event", "campaign_experience",
		"pvp_match_result", "user_association", "squad", "squad_creature",
		"creature", "creature_stat", "creature_ability_stat", "part", "part_shop",
		"extension", "chat_log", "zone_checkpoint", "zone_checkpoint_member",
		"account_access", "admin_event", "user_login_ip", "server_identity", "matchmaking_configuration", "host_configuration", "admission_configuration",
		"activity_collection", "activity_maintenance", "activity_signup_sample",
		"activity_hour_distribution", "activity_day_distribution",
		"activity_hour_queue", "activity_day_queue",
		"activity_campaign_first_completion", "activity_campaign_completion", "activity_campaign_day", "activity_campaign_sample",
		"activity_campaign_level",
		"activity_mission", "activity_mission_day",
		"activity_population_sample", "activity_first_admission", "activity_level_sample",
		"activity_progression_sample", "activity_user_sample", "activity_sample", "activity_presence",
		"activity_hour_sample", "activity_hour_presence", "activity_day_sample", "activity_day_presence",
		"activity_hour_dna", "activity_day_dna", "activity_user_hour_dna", "activity_user_day_dna",
		"activity_user_day_presence":
		return true
	default:
		return false
	}
}

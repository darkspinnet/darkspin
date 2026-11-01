package game

import "github.com/darkspinnet/darkspin/server/sporenet"

// Log only account progression and roster counts, never credentials or tokens.
// Paired snapshots distinguish rejected purchases from unexpected changes in
// the account fields that the client uses to lock heroes and squad slots.
func (e *API) logAccountUpgrade(
	unlockID uint32, before sporenet.UserView, after sporenet.UserView, err error,
) {
	if e.logger == nil {
		return
	}
	result := "accepted"
	if err != nil {
		result = "rejected"
	}
	e.logger.Printf(
		"account_upgrade account=%q user_id=%d unlock_id=%d result=%q "+
			"dna_before=%d dna_after=%d level_before=%d level_after=%d "+
			"unlock_stats_before=%d unlock_stats_after=%d "+
			"unlock_pve_decks_before=%d unlock_pve_decks_after=%d "+
			"unlock_pvp_decks_before=%d unlock_pvp_decks_after=%d "+
			"creature_rewards_before=%d creature_rewards_after=%d "+
			"creature_count_before=%d creature_count_after=%d "+
			"squad_count_before=%d squad_count_after=%d error=%v",
		before.LoginName, before.Account.ID, unlockID, result,
		before.Account.DNA, after.Account.DNA, before.Account.Level, after.Account.Level,
		before.Account.UnlockStats, after.Account.UnlockStats,
		before.Account.UnlockPVEDecks, after.Account.UnlockPVEDecks,
		before.Account.UnlockPVPDecks, after.Account.UnlockPVPDecks,
		before.Account.CreatureRewards, after.Account.CreatureRewards,
		len(before.Creatures), len(after.Creatures), len(before.Squads), len(after.Squads), err,
	)
}

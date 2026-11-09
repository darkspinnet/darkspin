package sqlstore

import matchmakingsql "github.com/darkspinnet/darkspin/server/matchmaking/sqlstore"

func (e *Repository) MatchmakingStore() *matchmakingsql.Store {
	return matchmakingsql.New(e.db)
}

package sqlstore

import loginhistorysql "github.com/darkspinnet/darkspin/server/loginhistory/sqlstore"

// LoginIPStore shares the profile database without enabling local IP collection.
// Dedicated server composition explicitly creates its schema and wires recording.
func (e *Repository) LoginIPStore() *loginhistorysql.Store {
	return loginhistorysql.New(e.db)
}

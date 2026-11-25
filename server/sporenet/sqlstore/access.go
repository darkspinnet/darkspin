package sqlstore

import (
	accesssql "github.com/darkspinnet/darkspin/server/accountaccess/sqlstore"
)

// AccountAccess shares the profile pool without sharing profile-save ownership.
// The profile repository retains ownership of the connection pool.
func (e *Repository) AccountAccess() *accesssql.Store {
	return accesssql.New(e.db, e.isPostgres)
}

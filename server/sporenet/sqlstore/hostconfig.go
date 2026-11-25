package sqlstore

import hostconfigsql "github.com/darkspinnet/darkspin/server/hostconfig/sqlstore"

func (e *Repository) HostConfigurationStore() *hostconfigsql.Store {
	return hostconfigsql.New(e.db)
}

package sqlstore

import capacitysql "github.com/darkspinnet/darkspin/server/admissionconfig/sqlstore"

func (e *Repository) CapacityStore() *capacitysql.Store {
	return capacitysql.New(e.db)
}

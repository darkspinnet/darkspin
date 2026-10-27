package sporenet

import "time"

// UserRecord is the storage-neutral representation consumed by the feature's
// repository port. It deliberately excludes active session and transport state.
// Storage adapters translate this data to XML documents, SQL rows, or another
// private representation.
type UserRecord struct {
	DisplayName                 string
	LoginName                   string
	Password                    string
	CreateDT                    time.Time
	LastConnectionDT            time.Time
	IsTutorialCompletionPending bool
	Account                     Account
	Stats                       PlayerStats
	Squads                      []Squad
	Creatures                   []*Creature
	Parts                       []Part
	PartShop                    PartShopState
	Events                      []UserEvent
	CampaignExperiences         []CampaignExperience
	PVPResults                  []PVPMatchResult
	Associations                map[uint32][]AssociationMember
	Settings                    map[string]string
	Extensions                  []OpaqueField
}

// OpaqueField preserves legacy data that the server does not understand yet.
// It remains private persistence data and must never be included in a client
// projection without first being promoted to a typed feature field.
type OpaqueField struct {
	Name  string
	Value []byte
}

// Clone detaches a save snapshot from all caller-owned mutable collections.
func (e UserRecord) Clone() UserRecord {
	e.Squads = append([]Squad(nil), e.Squads...)
	e.Creatures = cloneCreatures(e.Creatures)
	e.Parts = append([]Part(nil), e.Parts...)
	e.Events = append([]UserEvent(nil), e.Events...)
	e.CampaignExperiences = append([]CampaignExperience(nil), e.CampaignExperiences...)
	e.PVPResults = append([]PVPMatchResult(nil), e.PVPResults...)
	e.Associations = cloneAssociations(e.Associations)
	e.Settings = cloneSettings(e.Settings)
	e.Extensions = cloneOpaqueFields(e.Extensions)
	return e
}

// Record returns a detached, consistent copy suitable for persistence.
func (u *User) Record() UserRecord {
	if u == nil {
		return UserRecord{}
	}
	u.mu.RLock()
	record := UserRecord{
		DisplayName:                 u.DisplayName,
		LoginName:                   u.LoginName,
		Password:                    u.Password,
		CreateDT:                    u.CreateDT,
		LastConnectionDT:            u.LastConnectionDT,
		IsTutorialCompletionPending: u.IsTutorialCompletionPending,
		Account:                     u.Account,
		Stats:                       u.Stats,
		Squads:                      append([]Squad(nil), u.Squads...),
		Creatures:                   cloneCreatures(u.Creatures),
		Parts:                       append([]Part(nil), u.Parts...),
		PartShop:                    u.PartShop,
		Events:                      append([]UserEvent(nil), u.Events...),
		CampaignExperiences:         append([]CampaignExperience(nil), u.CampaignExperiences...),
		PVPResults:                  append([]PVPMatchResult(nil), u.PVPResults...),
		Associations:                cloneAssociations(u.Associations),
		Settings:                    cloneSettings(u.Settings),
		Extensions:                  cloneOpaqueFields(u.extensions),
	}
	u.mu.RUnlock()
	return record
}

// NewUserFromRecord restores a durable record as an inactive aggregate.
func NewUserFromRecord(record UserRecord, templateDatabase *TemplateDatabase) *User {
	user := &User{
		DisplayName:                 record.DisplayName,
		LoginName:                   record.LoginName,
		Password:                    record.Password,
		CreateDT:                    record.CreateDT,
		LastConnectionDT:            record.LastConnectionDT,
		IsTutorialCompletionPending: record.IsTutorialCompletionPending,
		Account:                     normalizeAccount(record.Account),
		Stats:                       record.Stats,
		Squads:                      append([]Squad(nil), record.Squads...),
		Creatures:                   cloneCreatures(record.Creatures),
		Parts:                       append([]Part(nil), record.Parts...),
		PartShop:                    record.PartShop,
		Events:                      append([]UserEvent(nil), record.Events...),
		CampaignExperiences:         append([]CampaignExperience(nil), record.CampaignExperiences...),
		PVPResults:                  append([]PVPMatchResult(nil), record.PVPResults...),
		Associations:                cloneAssociations(record.Associations),
		Settings:                    cloneSettings(record.Settings),
		extensions:                  cloneOpaqueFields(record.Extensions),
	}
	for _, creature := range user.Creatures {
		if creature != nil {
			creature.Normalize(templateDatabase)
		}
	}
	for index := range user.Parts {
		user.Parts[index].Normalize()
	}
	return user
}

func cloneCreatures(creatures []*Creature) []*Creature {
	result := make([]*Creature, len(creatures))
	for index, creature := range creatures {
		if creature == nil {
			continue
		}
		copy := *creature
		copy.Stats = append([]Stat(nil), creature.Stats...)
		copy.AbilityStats = append([]AbilityStat(nil), creature.AbilityStats...)
		result[index] = &copy
	}
	return result
}

func cloneSettings(settings map[string]string) map[string]string {
	result := make(map[string]string, len(settings))
	for key, value := range settings {
		result[key] = value
	}
	return result
}

func cloneOpaqueFields(fields []OpaqueField) []OpaqueField {
	result := make([]OpaqueField, len(fields))
	for index, field := range fields {
		result[index] = OpaqueField{Name: field.Name, Value: append([]byte(nil), field.Value...)}
	}
	return result
}

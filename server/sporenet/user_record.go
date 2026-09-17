package sporenet

// UserRecord is the storage-neutral representation consumed by the feature's
// repository port. It deliberately excludes active session and transport state.
// Storage adapters translate this data to XML documents, SQL rows, or another
// private representation.
type UserRecord struct {
	DisplayName                 string
	LoginName                   string
	Password                    string
	IsTutorialCompletionPending bool
	Account                     Account
	Stats                       PlayerStats
	Squads                      []Squad
	Creatures                   []*Creature
	Parts                       []Part
	Events                      []UserEvent
	CampaignExperiences         []CampaignExperience
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
		IsTutorialCompletionPending: u.IsTutorialCompletionPending,
		Account:                     u.Account,
		Stats:                       u.Stats,
		Squads:                      append([]Squad(nil), u.Squads...),
		Creatures:                   cloneCreatures(u.Creatures),
		Parts:                       append([]Part(nil), u.Parts...),
		Events:                      append([]UserEvent(nil), u.Events...),
		CampaignExperiences:         append([]CampaignExperience(nil), u.CampaignExperiences...),
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
		IsTutorialCompletionPending: record.IsTutorialCompletionPending,
		Account:                     normalizeAccount(record.Account),
		Stats:                       record.Stats,
		Squads:                      append([]Squad(nil), record.Squads...),
		Creatures:                   cloneCreatures(record.Creatures),
		Parts:                       append([]Part(nil), record.Parts...),
		Events:                      append([]UserEvent(nil), record.Events...),
		CampaignExperiences:         append([]CampaignExperience(nil), record.CampaignExperiences...),
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

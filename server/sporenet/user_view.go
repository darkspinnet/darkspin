package sporenet

import "time"

// UserIdentity is the stable profile identity shown by local management tools.
type UserIdentity struct {
	LoginName                   string
	DisplayName                 string
	AvatarID                    uint32
	Level                       uint32
	XP                          uint32
	ChainProgression            uint32
	IsTutorialCompleted         bool
	IsTutorialCompletionPending bool
}

// UserView is transport-neutral player-visible data. It excludes credentials,
// opaque persistence extensions, moderation data, and active server state.
// Protocol adapters must still explicitly select the fields their client
// response requires instead of serializing this value directly.
type UserView struct {
	DisplayName  string
	LoginName    string
	AuthToken    string
	Account      Account
	Stats        PlayerStats
	Squads       []Squad
	Creatures    []*Creature
	Parts        []Part
	Events       []UserEvent
	Associations map[uint32][]AssociationMember
	Settings     map[string]string
}

// View returns a detached and internally consistent client-safe projection.
func (u *User) View() UserView {
	if u == nil {
		return UserView{}
	}
	u.mu.RLock()
	stats := u.Stats
	if !u.playStartedAt.IsZero() {
		elapsed := time.Since(u.playStartedAt)
		if elapsed > 0 {
			stats.PVEPlayTimeSecond += uint64(elapsed / time.Second)
		}
	}
	view := UserView{
		DisplayName:  u.DisplayName,
		LoginName:    u.LoginName,
		AuthToken:    u.AuthToken,
		Account:      u.Account,
		Stats:        stats,
		Squads:       append([]Squad(nil), u.Squads...),
		Creatures:    cloneCreatures(u.Creatures),
		Parts:        append([]Part(nil), u.Parts...),
		Events:       append([]UserEvent(nil), u.Events...),
		Associations: cloneAssociations(u.Associations),
		Settings:     cloneSettings(u.Settings),
	}
	u.mu.RUnlock()
	return view
}

func cloneAssociations(associations map[uint32][]AssociationMember) map[uint32][]AssociationMember {
	result := make(map[uint32][]AssociationMember, len(associations))
	for listType, members := range associations {
		result[listType] = append([]AssociationMember(nil), members...)
	}
	return result
}

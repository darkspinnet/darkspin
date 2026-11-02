package blaze

import (
	"math/rand/v2"

	contentcache "github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/server/game"
)

// Pick once at matchmaking so lobby, gameplay and results share the same
// portrait. Prefer a portrait distinct from the owner and existing bots.
func randomBotAvatarID(instance *game.Instance, ownerAvatarID uint32) uint32 {
	avatars := contentcache.SelectableProfileAvatars()
	usedIDs := map[uint32]bool{ownerAvatarID: true}
	for _, bot := range instance.Bots() {
		usedIDs[bot.AvatarID] = true
	}
	avatarIDs := make([]uint32, 0, len(avatars))
	for _, avatar := range avatars {
		if !usedIDs[avatar.ID] {
			avatarIDs = append(avatarIDs, avatar.ID)
		}
	}
	if len(avatarIDs) > 0 {
		return avatarIDs[rand.IntN(len(avatarIDs))]
	}
	if len(avatars) > 0 {
		return avatars[rand.IntN(len(avatars))].ID
	}
	return ownerAvatarID
}

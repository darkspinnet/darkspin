package content

import (
	"context"
	"fmt"
)

// ProfileAvatar identifies one retail account portrait and its cached target.
type ProfileAvatar struct {
	ID     uint32
	Target string
}

var profileAvatarRecipe = []ProfileAvatar{
	{ID: 0, Target: "assets/images/avatars/626285B6.png"},
	{ID: 1, Target: "assets/images/avatars/626285B7.png"},
	{ID: 2, Target: "assets/images/avatars/626285B4.png"},
	{ID: 3, Target: "assets/images/avatars/626285B5.png"},
	{ID: 4, Target: "assets/images/avatars/626285B2.png"},
	{ID: 5, Target: "assets/images/avatars/626285B3.png"},
	{ID: 6, Target: "assets/images/avatars/626285B0.png"},
	{ID: 7, Target: "assets/images/avatars/626285B1.png"},
	{ID: 8, Target: "assets/images/avatars/626285BE.png"},
	{ID: 9, Target: "assets/images/avatars/626285BF.png"},
	{ID: 10, Target: "assets/images/avatars/98187F25.png"},
	{ID: 11, Target: "assets/images/avatars/98187F24.png"},
	{ID: 12, Target: "assets/images/avatars/98187F27.png"},
	{ID: 13, Target: "assets/images/avatars/98187F26.png"},
	{ID: 14, Target: "assets/images/avatars/98187F21.png"},
	{ID: 15, Target: "assets/images/avatars/98187F20.png"},
	{ID: 16, Target: "assets/images/avatars/98187F23.png"},
}

// ProfileAvatars returns the stable retail portrait choices in account-ID order.
func ProfileAvatars() []ProfileAvatar {
	return append([]ProfileAvatar(nil), profileAvatarRecipe...)
}

// SelectableProfileAvatars returns the retail registration choices. ID zero is
// reserved for fallback recovery, and ID 16 is not offered by the build-103
// registration flow.
func SelectableProfileAvatars() []ProfileAvatar {
	avatars := make([]ProfileAvatar, 0, 15)
	for _, avatar := range profileAvatarRecipe {
		if avatar.ID < 1 || avatar.ID > 15 {
			continue
		}
		avatars = append(avatars, avatar)
	}
	return avatars
}

// ProfileAvatarByID resolves a persisted account portrait, including fallback
// and legacy IDs which are not offered during new registration.
func ProfileAvatarByID(avatarID uint32) (ProfileAvatar, bool) {
	for _, avatar := range profileAvatarRecipe {
		if avatar.ID == avatarID {
			return avatar, true
		}
	}
	return ProfileAvatar{}, false
}

// IsSelectableProfileAvatar reports whether an ID is valid for registration.
func IsSelectableProfileAvatar(avatarID uint32) bool {
	return avatarID >= 1 && avatarID <= 15
}

// PrepareProfileAvatars reconstructs the small account portrait set before the
// longer static and content database build.
func PrepareProfileAvatars(ctx context.Context, options WebOptions) error {
	assets := make([]webAsset, 0, len(profileAvatarRecipe))
	assetByTarget := make(map[string]webAsset, len(webAssetRecipe))
	for _, asset := range webAssetRecipe {
		assetByTarget[asset.Target] = asset
	}
	for index, avatar := range profileAvatarRecipe {
		asset, isFound := assetByTarget[avatar.Target]
		if !isFound {
			return fmt.Errorf("avatarRecipe[%d]: missing target %s", index, avatar.Target)
		}
		assets = append(assets, asset)
	}
	err := prepareWebAssets(ctx, options, assets)
	if err != nil {
		return fmt.Errorf("avatarPrepare: %w", err)
	}
	return nil
}

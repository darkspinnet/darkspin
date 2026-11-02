package game

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Project only a bound hero's images. Template names, identities and gameplay
// fields keep their normal values, and standard servers never use this path.
func (e *API) modPortraitCreature(ctx context.Context, user *sporenet.User, noun uint32) *sporenet.Creature {
	if !e.isModded || user == nil {
		return nil
	}
	view := user.View()
	key := "mod.binding." + strconv.FormatUint(uint64(noun), 10)
	if view.Settings[key] == "" {
		return nil
	}
	for _, creature := range view.Creatures {
		if creature == nil || creature.Noun() != noun {
			continue
		}
		portrait, err := e.appearanceCreature(ctx, creature)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("mod portrait unavailable noun=%d: %v", noun, err)
			}
			return nil
		}
		if portrait.ThumbImageURL != "" {
			return portrait
		}
	}
	return nil
}

func (e *API) serveModPortrait(writer http.ResponseWriter, request *http.Request, noun uint32) bool {
	if !e.isModded || e.userManager == nil {
		return false
	}
	user := e.userFromRequest(request, request.URL.Query())
	if user == nil {
		// Native image GETs omit the session token. A local Mod server with one
		// active profile has an unambiguous owner; never guess between profiles.
		users := e.userManager.Users()
		if len(users) != 1 {
			return false
		}
		user = users[0]
	}
	portrait := e.modPortraitCreature(request.Context(), user, noun)
	if portrait == nil {
		return false
	}
	uri, err := url.Parse(portrait.ThumbImageURL)
	if err != nil || !strings.HasPrefix(uri.Path, "/creature_png/") {
		return false
	}
	name := strings.TrimPrefix(uri.Path, "/creature_png/")
	if name == "" || strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, "_thumb.png") {
		return false
	}
	resource, err := gameplayAppearanceResource(uri.Path)
	if err != nil {
		return false
	}
	writer.Header().Set("Cache-Control", "private, no-cache")
	e.serveStorageFile(writer, request, e.storage, resource)
	return true
}

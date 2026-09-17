package game

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	contentcache "github.com/darkspinnet/darkspin/content"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// UserManager is the HTTP adapter's application boundary. It is intentionally
// defined by the consumer and contains no filesystem or database operations.
type UserManager interface {
	Register(context.Context, sporenet.Registration) (*sporenet.User, error)
	Logout(context.Context, *sporenet.User) error
	UnlockCreature(context.Context, *sporenet.User, uint32) (uint32, error)
	UpdateDecks(context.Context, *sporenet.User, sporenet.DeckUpdate) error
	UnlockUpgrade(context.Context, *sporenet.User, uint32) error
	SetSettings(context.Context, *sporenet.User, map[string]string) error
	UpdateOnboarding(context.Context, *sporenet.User, *uint32, *uint32) error
	UpdateCreature(context.Context, *sporenet.User, sporenet.CreatureUpdate) (*sporenet.Creature, error)
	ApplyVendorTransactions(context.Context, *sporenet.User, []sporenet.VendorTransaction, *sporenet.Vendor, sporenet.VendorPartPolicy) (sporenet.VendorTransactionResult, error)
	ConvertPartsToDetail(context.Context, *sporenet.User, []uint64) ([]sporenet.Part, error)
	PublicProfile(context.Context, int64, string) (sporenet.UserView, error)
	UserByAuthToken(string) *sporenet.User
	Users() []*sporenet.User
}

// ResumeActivator restores authenticated game membership before the account
// adapter publishes current_game_id to the native client.
type ResumeActivator interface {
	ActivateResume(context.Context, *sporenet.User) (bool, error)
}

// API serves the legacy launcher, account, inventory, creature, and QoS HTTP APIs.
type API struct {
	userManager         UserManager
	template            *sporenet.TemplateDatabase
	partCatalog         *PartCatalog
	vendor              *sporenet.Vendor
	storage             string
	staticFileSystem    fs.FS
	host                string
	httpPort            uint16
	qosPort             uint16
	logger              *log.Logger
	resumeActivator     ResumeActivator
	broadcastMutex      sync.Mutex
	broadcastRecipients map[string]struct{}
}

type APIOptions struct {
	UserManager      UserManager
	Template         *sporenet.TemplateDatabase
	PartCatalog      *PartCatalog
	Vendor           *sporenet.Vendor
	Storage          string
	StaticFileSystem fs.FS
	Host             string
	HTTPPort         uint16
	QoSPort          uint16
	Logger           *log.Logger
	ResumeActivator  ResumeActivator
}

func RegisterAPI(router *recaphttp.Router, options APIOptions) error {
	logger := options.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	api := &API{
		userManager: options.UserManager, template: options.Template, partCatalog: options.PartCatalog, vendor: options.Vendor,
		storage: options.Storage, staticFileSystem: options.StaticFileSystem,
		host: options.Host, httpPort: options.HTTPPort, qosPort: options.QoSPort, logger: logger,
		resumeActivator:     options.ResumeActivator,
		broadcastRecipients: make(map[string]struct{}),
	}
	routes := []struct {
		path    string
		handler recaphttp.Handler
	}{
		{`/api`, api.empty},
		{`/telemetryevent`, api.empty},
		{`/recap/api`, api.recap},
		// Game 5.3.0.103 references these three API paths directly.
		// Their handlers provide the client-facing compatibility surface; method
		// coverage within each endpoint is documented in the switch below.
		{`/bootstrap/api`, api.bootstrap},
		{`/game/api`, api.game},
		{`/survey/api`, api.survey},
		{`/qos/qos`, api.qos},
		{`/qos/firewall`, api.firewall},
		{`/qos/firetype`, api.firetype},
		{`/bootstrap/launcher/?`, api.empty},
		{`/bootstrap/launcher/.*`, api.empty},
		{`/game/service/png`, api.profileImage},
		{`/web/sporelabsgame/manual[a-zA-Z]*`, api.manual},
		{`/template_png/[a-zA-Z0-9_.]+`, api.static},
		{`/creature_png/[a-zA-Z0-9_.]+`, api.creatureImage},
		{`/assets/.*`, api.static},
	}
	for _, route := range routes {
		err := router.Add(route.path, []string{http.MethodGet, http.MethodPost}, route.handler)
		if err != nil {
			return fmt.Errorf("routeRegister[%q]: %w", route.path, err)
		}
	}
	return nil
}

func (a *API) manual(writer http.ResponseWriter, _ *http.Request, _ *recaphttp.URI) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, `<!doctype html>
<html><head><meta charset="utf-8"><style>
body{margin:0;background:#050a0e;color:#d7e6ec;font:16px Arial,sans-serif}
main{padding:28px 40px}h1{font-size:24px;letter-spacing:2px;color:#fff}
h2{font-size:16px;color:#9ed9e8;margin-top:24px}p,li{line-height:1.5}
code{color:#fff;background:#15242b;padding:2px 5px}
</style></head><body><main><h1>FIELD MANUAL</h1>
<p>This local restoration is still rebuilding the original online manual.</p>
<h2>Core controls</h2><ul><li>Left click to move or attack.</li>
<li>Hold Shift while attacking to remain in place.</li>
<li>Use the squad portraits to deploy another available hero.</li></ul>
<h2>Testing commands</h2><p><code>/help</code> lists available chat commands.
Use <code>/dna #</code>, <code>/damage #</code>, <code>/heal</code>, <code>/power</code>, or
<code>/event</code> while testing a campaign. If transient movement, ability,
or squad state becomes stuck, use <code>/reset</code>.</p></main></body></html>`)
}

func (a *API) empty(writer http.ResponseWriter, _ *http.Request, _ *recaphttp.URI) {
	writer.WriteHeader(http.StatusOK)
}

func (a *API) recap(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	values := requestValues(request, uri)
	switch values.Get("method") {
	case "api.game.registration":
		if !isLoopbackRequest(request.RemoteAddr) && values.Get("pass") == "" {
			writeJSON(writer, http.StatusBadRequest, map[string]any{
				"success": false, "error": "password is required for remote registration",
			})
			return
		}
		avatar, err := strconv.ParseUint(values.Get("avatar"), 10, 32)
		if err != nil {
			avatar = 0
		}
		_, err = a.userManager.Register(request.Context(), sporenet.Registration{
			DisplayName: values.Get("name"), LoginName: values.Get("email"), Password: values.Get("pass"),
			AvatarID: uint32(avatar),
		})
		if err != nil {
			writeJSON(writer, http.StatusConflict, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"success": true})
	case "api.game.status":
		writeJSON(writer, http.StatusOK, map[string]any{"status": "ok", "users": len(a.userManager.Users())})
	case "api.panel.listUsers":
		writeJSON(writer, http.StatusOK, map[string]any{"users": a.userManager.Users()})
	default:
		writeJSON(writer, http.StatusOK, map[string]any{"stat": "ok"})
	}
}

func isLoopbackRequest(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *API) bootstrap(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	values := requestValues(request, uri)
	build := values.Get("build")
	if build == "" {
		build = "5.3.0.127"
	}
	response := newBootstrapAPIResponse(
		a.host,
		a.httpPort,
		build,
		queryBool(values, "include_settings", "includeSettings"),
		queryBool(values, "include_patches", "includePatches"),
	)
	writeXMLDocument(writer, http.StatusOK, response)
}

func (a *API) game(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	values := requestValues(request, uri)
	method := values.Get("method")
	if strings.EqualFold(method, "api.status.getStatus") {
		isBroadcastIncluded := queryBool(values, "include_broadcasts", "includeBroadcasts") &&
			a.claimBroadcast(request.RemoteAddr)
		response := newGameStatusAPIResponse(true, isBroadcastIncluded)
		writeXMLDocument(writer, http.StatusOK, response)
		return
	}
	if strings.EqualFold(method, "api.status.getBroadcastList") {
		response := newGameStatusAPIResponse(false, a.claimBroadcast(request.RemoteAddr))
		writeXMLDocument(writer, http.StatusOK, response)
		return
	}
	user := a.userFromRequest(request, values)
	if method == "" {
		if user == nil {
			method = "api.account.auth"
		} else {
			method = "api.account.getAccount"
		}
	}
	switch method {
	// These methods have dedicated handlers that read or mutate Dark Spin state.
	case "api.account.auth":
		a.accountResponse(request.Context(), writer, user, values)
	case "api.account.getAccount":
		targetView, isFound := a.accountProfileTarget(request.Context(), user, values)
		if !isFound {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		isPublic := targetView.Account.ID != user.View().Account.ID
		a.accountProfileViewResponse(request.Context(), writer, user, targetView, values, isPublic)
	case "api.account.logout":
		if user != nil {
			err := a.userManager.Logout(request.Context(), user)
			if err != nil {
				writeXML(writer, http.StatusInternalServerError, xmlResponse(false))
				return
			}
		}
		writeXML(writer, http.StatusOK, xmlResponse(true))
	case "api.inventory.getPartList":
		a.partList(writer, user, values)
	case "api.inventory.getPartOfferList":
		a.partOffers(writer)
	case "api.inventory.vendorParts":
		a.vendorParts(writer, request, user, values.Get("transactions"))
	case "api.inventory.updatePartStatus":
		partIDs, partIDErr := parseUint64CSV(values.Get("part_id"))
		statusCodes, statusErr := parseUint64CSV(values.Get("status"))
		isDetailStatus := len(statusCodes) != 0
		for _, currentStatus := range statusCodes {
			if currentStatus != uint64(sporenet.PartStatusDetail) {
				isDetailStatus = false
				break
			}
		}
		isSuccess := user != nil && values.Get("operator") == "" &&
			partIDErr == nil && statusErr == nil &&
			len(partIDs) == len(statusCodes) && isDetailStatus
		var statusErrApply error
		if isSuccess {
			_, statusErrApply = a.userManager.ConvertPartsToDetail(
				request.Context(), user, partIDs,
			)
			isSuccess = statusErrApply == nil
		}
		if a.logger != nil {
			a.logger.Printf(
				"inventory_part_status account=%q part_id=%q status=%q operator=%q success=%t error=%v",
				userLoginName(user), values.Get("part_id"), values.Get("status"),
				values.Get("operator"), isSuccess, statusErrApply,
			)
		}
		writeXML(writer, http.StatusOK, xmlResponse(isSuccess))
	case "api.creature.getCreature":
		a.creatureResponse(writer, user, values)
	case "api.creature.getTemplate":
		a.templateResponse(writer, values.Get("id"))
	case "api.creature.unlockCreature":
		templateID := values.Get("template_id")
		if templateID == "" {
			templateID = values.Get("noun_id")
		}
		noun, err := strconv.ParseUint(templateID, 10, 32)
		if err != nil || user == nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		id, err := a.userManager.UnlockCreature(request.Context(), user, uint32(noun))
		if err != nil {
			if a.logger != nil {
				a.logger.Printf(
					"creature_unlock account=%q template=%d result=%q error=%v",
					userLoginName(user), uint32(noun), "rejected", err,
				)
			}
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		if a.logger != nil {
			a.logger.Printf(
				"creature_unlock account=%q template=%d creature=%d result=%q",
				userLoginName(user), uint32(noun), id, "accepted",
			)
		}
		writeXML(writer, http.StatusOK, xmlResponse(id != 0, xmlText("creature_id", strconv.FormatUint(uint64(id), 10))))
	case "api.deck.updateDecks":
		command := sporenet.DeckUpdate{
			PVEActiveSlot: parseUint32(values.Get("pve_active_slot")),
			PVECreatures:  parseUint32List(values.Get("pve_creatures")),
			PVPActiveSlot: parseUint32(values.Get("pvp_active_slot")),
			PVPCreatures:  parseUint32List(values.Get("pvp_creatures")),
		}
		if user == nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		err := a.userManager.UpdateDecks(request.Context(), user, command)
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		writeXML(writer, http.StatusOK, xmlResponse(true))
	// These methods acknowledge the request but intentionally do not write
	// storage until their method-specific commands and invariants are decoded.
	case "api.account.unlock":
		if user == nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		unlockID, err := strconv.ParseUint(values.Get("unlock_id"), 10, 32)
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		err = a.userManager.UnlockUpgrade(request.Context(), user, uint32(unlockID))
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		view := user.View()
		writeXML(writer, http.StatusOK, xmlResponse(true, a.privateAccountFields(user, view)...))
	case "api.account.setSettings":
		if user == nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		err := a.userManager.SetSettings(request.Context(), user, parseSettings(values.Get("settings")))
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		writeXML(writer, http.StatusOK, xmlResponse(true))
	case "api.account.setNewPlayerStats":
		if user == nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		progress, err := optionalUint32(values.Get("new_player_progress"))
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		inventory, err := optionalUint32(values.Get("new_player_inventory"))
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		err = a.userManager.UpdateOnboarding(request.Context(), user, progress, inventory)
		if progress != nil && a.logger != nil {
			result := "accepted"
			reason := "ship_milestone"
			if err != nil {
				result = "rejected"
				reason = "invalid_transition"
			}
			view := user.View()
			a.logger.Printf(
				"onboarding_progress_submission remote_ip=%q account=%q value=%d result=%q reason=%q",
				requestRemoteIP(request.RemoteAddr), view.LoginName, *progress, result, reason,
			)
		}
		if err != nil {
			writeXML(writer, http.StatusOK, xmlResponse(false))
			return
		}
		a.accountResponse(request.Context(), writer, user, values)
	case "api.creature.updateCreature":
		a.updateCreature(writer, request, user, values)
	case "api.creature.resetCreature":
		if a.logger != nil {
			account := ""
			if user != nil {
				account = user.View().LoginName
			}
			a.logger.Printf(
				"game_api_compat method=%q account=%q fields=%q",
				method, account, compatibilityFieldNames(values),
			)
		}
		writeXML(writer, http.StatusOK, xmlResponse(user != nil))
	case "api.account.searchAccounts":
		terms := strings.ToLower(strings.TrimSpace(values.Get("terms")))
		matches := make([]sporenet.UserView, 0)
		for _, candidate := range a.userManager.Users() {
			view := candidate.View()
			isSelf := user != nil && view.Account.ID == user.Account.ID
			if isSelf {
				continue
			}
			if terms != "" && !strings.Contains(strings.ToLower(view.DisplayName), terms) {
				continue
			}
			matches = append(matches, view)
		}
		sort.Slice(matches, func(left, right int) bool {
			return strings.ToLower(matches[left].DisplayName) < strings.ToLower(matches[right].DisplayName)
		})
		start := 0
		parsedStart, startErr := strconv.Atoi(values.Get("start"))
		if startErr == nil && parsedStart > 0 {
			start = parsedStart
		}
		if start > len(matches) {
			start = len(matches)
		}
		count := 10
		parsedCount, countErr := strconv.Atoi(values.Get("count"))
		if countErr == nil && parsedCount > 0 {
			count = parsedCount
		}
		end := start + count
		if end > len(matches) {
			end = len(matches)
		}
		nodes := make([]string, 0, end-start)
		for _, view := range matches[start:end] {
			accountID := strconv.FormatInt(view.Account.ID, 10)
			nodes = append(nodes, xmlNode("account",
				xmlText("blaze_id", accountID),
				xmlText("account_id", accountID),
				xmlText("name", view.DisplayName),
				xmlText("avatar_id", number(view.Account.AvatarID)),
				xmlText("level", number(view.Account.Level)),
				xmlText("presence_level", "4"),
			))
		}
		writeXML(writer, http.StatusOK, xmlResponse(true,
			xmlText("total", strconv.Itoa(len(matches))),
			xmlNode("accounts", nodes...),
		))
	// Minimal game compatibility response; replay/session semantics are not yet
	// reconstructed from the client.
	case "api.game.getGame", "api.game.getRandomGame", "api.game.exitGame":
		writeXML(writer, http.StatusOK, xmlResponse(true, xmlText("game_id", "1")))
	case "api.leaderboard.getLeaderboard":
		users := []*sporenet.User(nil)
		if a.userManager != nil {
			users = a.userManager.Users()
		}
		writeXML(writer, http.StatusOK, leaderboardResponse(user, users, values))
	default:
		// Unknown and not-yet-decoded methods intentionally receive the legacy
		// success envelope so client flows can continue during reconstruction.
		if a.logger != nil {
			account := ""
			if user != nil {
				account = user.View().LoginName
			}
			a.logger.Printf(
				"game_api_compat method=%q account=%q fields=%q",
				method, account, compatibilityFieldNames(values),
			)
		}
		writeXML(writer, http.StatusOK, xmlResponse(true))
	}
}

func (a *API) claimBroadcast(remoteAddress string) bool {
	if a == nil {
		return false
	}
	recipient := requestRemoteIP(remoteAddress)
	if recipient == "" {
		return false
	}
	a.broadcastMutex.Lock()
	defer a.broadcastMutex.Unlock()
	if _, isFound := a.broadcastRecipients[recipient]; isFound {
		return false
	}
	a.broadcastRecipients[recipient] = struct{}{}
	return true
}

func compatibilityFieldNames(values url.Values) string {
	fields := make([]string, 0, len(values))
	for field := range values {
		switch strings.ToLower(field) {
		case "method", "token", "key", "password", "auth":
			continue
		}
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return strings.Join(fields, ",")
}

func userLoginName(user *sporenet.User) string {
	if user == nil {
		return ""
	}
	return user.View().LoginName
}

func (a *API) updateCreature(writer http.ResponseWriter, request *http.Request, user *sporenet.User, values url.Values) {
	if user == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	creatureID := parseUint32(values.Get("id"))
	version := parseUint32(values.Get("version")) + 1
	partID := parseUint64List(values.Get("parts"))
	if a.logger != nil {
		a.logger.Printf(
			"creature_update account=%q creature_id=%d version=%d gear=%q points=%q parts=%q stats_length=%d ability_stats_length=%d thumb_length=%d large_length=%d",
			userLoginName(user), creatureID, version, values.Get("gear"), values.Get("points"), values.Get("parts"),
			len(values.Get("stats")), len(values.Get("stats_ability_keyvalues")), len(values.Get("thumb")), len(values.Get("large")),
		)
	}
	largeURL, err := a.storeCreatureImage(creatureID, version, "large", values.Get("large"), values.Get("large_crc"))
	if err != nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	thumbURL, err := a.storeCreatureImage(creatureID, version, "thumb", values.Get("thumb"), values.Get("thumb_crc"))
	if err != nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	gearScore, err := strconv.ParseFloat(values.Get("gear"), 32)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("creature_update invalid gear account=%q gear=%q error=%q", userLoginName(user), values.Get("gear"), err)
		}
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	itemPoints, err := strconv.ParseFloat(values.Get("points"), 32)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("creature_update invalid points account=%q points=%q error=%q", userLoginName(user), values.Get("points"), err)
		}
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	creature, err := a.userManager.UpdateCreature(request.Context(), user, sporenet.CreatureUpdate{
		CreatureID: creatureID, GearScore: float32(gearScore), ItemPoints: float32(itemPoints),
		Stats: values.Get("stats"), AbilityStats: values.Get("stats_ability_keyvalues"), EquippedPartID: partID,
		LargeImageURL: largeURL, ThumbImageURL: thumbURL,
	})
	if err != nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	writeXML(writer, http.StatusOK, xmlResponse(true, xmlNode("creature", xmlText("version", number(creature.Version)))))
}

func (a *API) storeCreatureImage(creatureID, version uint32, kind, encoded, rawCRC string) (string, error) {
	_ = rawCRC
	if encoded == "" {
		return "", nil
	}
	contents, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, " ", "+"))
	if err != nil {
		return "", fmt.Errorf("imageDecode: %w", err)
	}
	directory := filepath.Join(a.storage, "creature_png")
	err = os.MkdirAll(directory, 0o755)
	if err != nil {
		return "", fmt.Errorf("imageDirectory: %w", err)
	}
	name := fmt.Sprintf("%d_%d_%s.png", creatureID, version, kind)
	err = os.WriteFile(filepath.Join(directory, name), contents, 0o600)
	if err != nil {
		return "", fmt.Errorf("imageWrite: %w", err)
	}
	return fmt.Sprintf("http://%s:%d/creature_png/%s", a.host, a.httpPort, name), nil
}

func parseUint64List(encoded string) []uint64 {
	items := make([]uint64, 0)
	for _, field := range strings.Split(encoded, ",") {
		itemID, err := strconv.ParseUint(strings.TrimSpace(field), 10, 64)
		if err == nil && itemID != 0 {
			items = append(items, itemID)
		}
	}
	return items
}

func parseUint64CSV(encoded string) ([]uint64, error) {
	fields := strings.Split(encoded, ",")
	if encoded == "" || len(fields) == 0 {
		return nil, errors.New("uint64 CSV empty")
	}
	numbers := make([]uint64, 0, len(fields))
	for index, currentField := range fields {
		currentNumber, err := strconv.ParseUint(strings.TrimSpace(currentField), 10, 64)
		if err != nil || currentNumber == 0 {
			return nil, fmt.Errorf("uint64CSV[%d]: invalid", index)
		}
		numbers = append(numbers, currentNumber)
	}
	return numbers, nil
}

func requestRemoteIP(remoteAddress string) string {
	address, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return address
	}
	return remoteAddress
}

func (a *API) accountResponse(
	ctx context.Context, writer http.ResponseWriter, user *sporenet.User, values interface{ Get(string) string },
) {
	a.accountProfileResponse(ctx, writer, user, values, false)
}

func (a *API) accountProfileResponse(
	ctx context.Context, writer http.ResponseWriter, user *sporenet.User,
	values interface{ Get(string) string }, isPublic bool,
) {
	if user == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	view := user.View()
	a.accountProfileViewResponse(ctx, writer, user, view, values, isPublic)
}

func (a *API) accountProfileViewResponse(
	ctx context.Context, writer http.ResponseWriter, user *sporenet.User, view sporenet.UserView,
	values interface{ Get(string) string }, isPublic bool,
) {
	if !isPublic && a.resumeActivator != nil {
		_, err := a.resumeActivator.ActivateResume(ctx, user)
		if err != nil {
			a.logger.Printf("Account resume activation failed user=%d: %v", user.Account.ID, err)
			writeXML(writer, http.StatusInternalServerError, xmlResponse(false))
			return
		}
		view = user.View()
	}
	if !isPublic && (values.Get("cookie") == "true" || values.Get("cookie") == "1") {
		http.SetCookie(writer, &http.Cookie{Name: "token", Value: view.AuthToken, Path: "/", HttpOnly: true})
	}
	accountNode := a.accountNode(user, view, isPublic)
	nodes := []string{xmlText("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10)), accountNode}
	if !isPublic && queryBool(values, "include_settings", "includeSettings") {
		settings := make([]string, 0)
		for key, setting := range view.Settings {
			if validXMLName(key) {
				settings = append(settings, xmlText(key, setting))
			}
		}
		nodes = append(nodes, xmlNode("settings", settings...))
	}
	if values.Get("include_creatures") == "true" {
		creatures := make([]string, 0, len(view.Creatures))
		for _, creature := range view.Creatures {
			creatures = append(creatures, a.accountCreatureNode(creature))
		}
		nodes = append(nodes, xmlNode("creatures", creatures...))
	}
	if values.Get("include_decks") == "true" {
		creatureByID := make(map[uint32]*sporenet.Creature, len(view.Creatures))
		for _, creature := range view.Creatures {
			if creature != nil {
				creatureByID[creature.ID] = creature
			}
		}
		squads := append([]sporenet.Squad(nil), view.Squads...)
		sort.SliceStable(squads, func(firstIndex, secondIndex int) bool {
			isFirstActive := squads[firstIndex].ID == view.Account.DefaultDeckPVEID
			isSecondActive := squads[secondIndex].ID == view.Account.DefaultDeckPVEID
			return isFirstActive && !isSecondActive
		})
		decks := make([]string, 0, len(squads))
		for _, squad := range squads {
			creatures := make([]string, 0, len(squad.CreatureIDs))
			for _, creatureID := range squad.CreatureIDs {
				creature := creatureByID[creatureID]
				if creature != nil {
					creatures = append(creatures, a.accountCreatureNode(creature))
				}
			}
			decks = append(decks, xmlNode("deck",
				xmlText("name", squad.Name),
				xmlText("category", squad.Category),
				xmlText("id", number(squad.ID)),
				xmlText("slot", number(squad.Slot)),
				xmlText("locked", boolNumber(squad.IsLocked)),
				xmlNode("creatures", creatures...),
			))
		}
		nodes = append(nodes, xmlNode("decks", decks...))
	}
	if queryBool(values, "include_feed", "includeFeed") {
		friendViews := []sporenet.UserView(nil)
		if !isPublic {
			friendViews = a.accountFriendViews(ctx, view)
		}
		nodes = append(nodes, accountFeedNode(view, isPublic, friendViews...))
	}
	if queryBool(values, "include_stats", "includeStats") {
		nodes = append(nodes, accountStatsNode(view.Account, view.Stats))
	}
	writeXML(writer, http.StatusOK, xmlResponse(true, nodes...))
}

func (a *API) accountFriendViews(ctx context.Context, owner sporenet.UserView) []sporenet.UserView {
	members := owner.Associations[sporenet.FriendAssociationList]
	views := make([]sporenet.UserView, 0, len(members))
	seen := make(map[int64]struct{}, len(members))
	for _, member := range members {
		if member.ID <= 0 || member.ID == owner.Account.ID {
			continue
		}
		if _, isFound := seen[member.ID]; isFound {
			continue
		}
		view, err := a.userManager.PublicProfile(ctx, member.ID, "")
		if err != nil {
			continue
		}
		seen[member.ID] = struct{}{}
		views = append(views, view)
	}
	return views
}

func (a *API) accountNode(user *sporenet.User, view sporenet.UserView, isPublic bool) string {
	if isPublic {
		profile := newPublicAccountProfile(view)
		return profile.xmlNode()
	}
	return xmlNode("account", a.privateAccountFields(user, view)...)
}

func (a *API) privateAccountFields(user *sporenet.User, view sporenet.UserView) []string {
	account := view.Account
	return []string{
		xmlText("blaze_id", strconv.FormatInt(account.ID, 10)),
		xmlText("name", view.DisplayName),
		xmlText("tutorial_completed", boolText(account.IsTutorialCompleted())),
		xmlText("chain_progression", number(account.ChainProgression)),
		xmlText("creature_rewards", number(account.CreatureRewards)),
		xmlText("current_game_id", number(user.CurrentGameID())),
		xmlText("current_playgroup_id", number(user.CurrentPlaygroupID())),
		xmlText("default_deck_pve_id", number(account.DefaultDeckPVEID)),
		xmlText("default_deck_pvp_id", number(account.DefaultDeckPVPID)),
		xmlText("level", number(account.Level)),
		xmlText("avatar_id", number(account.AvatarID)),
		xmlText("id", strconv.FormatInt(account.ID, 10)),
		xmlText("token", view.AuthToken),
		xmlText("dna", number(account.DNA)),
		xmlText("xp", number(account.XP)),
		xmlText("new_player_inventory", number(account.NewPlayerInventory)),
		xmlText("new_player_progress", number(account.OnboardingProgress)),
		xmlText("cashout_bonus_time", number(account.CashoutBonusTime)),
		xmlText("star_level", number(account.StarLevel)),
		xmlText("unlock_catalysts", number(account.UnlockCatalysts)),
		xmlText("unlock_diagonal_catalysts", number(account.UnlockDiagonalCatalysts)),
		xmlText("unlock_inventory", number(account.UnlockInventoryIdentify)),
		xmlText("unlock_fuel_tanks", number(account.UnlockFuelTanks)),
		xmlText("unlock_pve_decks", number(account.UnlockPVEDecks)),
		xmlText("unlock_pvp_decks", number(account.UnlockPVPDecks)),
		xmlText("unlock_stats", number(account.UnlockStats)),
		xmlText("unlock_inventory_identify", number(account.UnlockInventory)),
		xmlText("unlock_editor_flair_slots", number(account.UnlockEditorFlairSlots)),
		xmlText("upsell", number(account.Upsell)),
		xmlText("cap_level", number(account.CapLevel)),
		xmlText("cap_progression", number(account.CapProgression)),
		xmlText("grant_all_access", boolNumber(account.IsAllAccessGranted)),
		xmlText("grant_online_access", boolNumber(account.IsOnlineAccessGranted)),
	}
}

func accountStatsNode(account sporenet.Account, stats sporenet.PlayerStats) string {
	return xmlNode("stats",
		xmlText("pve_playTime", profilePlayTime(stats.PVEPlayTimeSecond)),
		xmlText("pve_xp", number(account.XP)),
		xmlText("pve_minionKills", number(stats.PVEMinionKill)),
		xmlText("pve_specialKills", number(stats.PVESpecialKill)),
		xmlText("pve_bossKills", number(stats.PVEBossKill)),
		xmlText("pve_totalKills", number(stats.PVETotalKill)),
		xmlText("pve_deaths", number(stats.PVEDeath)),
		xmlText("pve_killDeathRatio", profileRatio(stats.PVETotalKill, stats.PVEDeath)),
		xmlText("pve_damageDealt", profileWholeStatNumber(stats.PVEDamageDealt)),
		xmlText("pve_damageTaken", profileWholeStatNumber(stats.PVEDamageTaken)),
		xmlText("pve_damageMax", profileWholeStatNumber(stats.PVEDamageMaximum)),
		xmlText("pve_healing", profileWholeStatNumber(stats.PVEHealing)),
		xmlText("pve_healingReceived", profileWholeStatNumber(stats.PVEHealingReceived)),
		xmlText("pve_healingMax", profileWholeStatNumber(stats.PVEHealingMaximum)),
		xmlText("pve_progression", number(account.ChainProgression)),
		xmlText("pvp_playTime", profilePlayTime(stats.PVPPlayTimeSecond)),
		xmlText("pvp_wins", number(stats.PVPWin)),
		xmlText("pvp_losses", number(stats.PVPLoss)),
		xmlText("pvp_winLossRatio", profileRatio(stats.PVPWin, stats.PVPLoss)),
		xmlText("pvp_playerKills", number(stats.PVPPlayerKill)),
		xmlText("pvp_deaths", number(stats.PVPDeath)),
		xmlText("pvp_killDeathRatio", profileRatio(stats.PVPPlayerKill, stats.PVPDeath)),
		xmlText("pvp_damageDealt", profileStatNumber(stats.PVPDamageDealt)),
		xmlText("pvp_damageTaken", profileStatNumber(stats.PVPDamageTaken)),
		xmlText("pvp_damageMax", profileStatNumber(stats.PVPDamageMaximum)),
		xmlText("pvp_healing", profileStatNumber(stats.PVPHealing)),
		xmlText("pvp_healingReceived", profileStatNumber(stats.PVPHealingReceived)),
		xmlText("pvp_healingMax", profileStatNumber(stats.PVPHealingMaximum)),
		xmlText("wins", number(stats.PVPWin)),
	)
}

func profilePlayTime(second uint64) string {
	hour := second / 3600
	minute := second % 3600 / 60
	remainingSecond := second % 60
	if hour != 0 {
		return fmt.Sprintf("%dh %dm %ds", hour, minute, remainingSecond)
	}
	if minute != 0 {
		return fmt.Sprintf("%dm %ds", minute, remainingSecond)
	}
	return fmt.Sprintf("%ds", remainingSecond)
}

func profileRatio(numerator, denominator uint64) string {
	if denominator == 0 {
		return profileStatNumber(float64(numerator))
	}
	return profileStatNumber(float64(numerator) / float64(denominator))
}

func profileStatNumber(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

func profileWholeStatNumber(number float64) string {
	return strconv.FormatFloat(math.Ceil(max(0, number)), 'f', 0, 64)
}

func (a *API) partList(writer http.ResponseWriter, user *sporenet.User, values interface{ Get(string) string }) {
	if user == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	query := parsePartListQuery(values)
	view := user.View()
	nodes := make([]string, 0, min(query.count, len(view.Parts)))
	for index := range view.Parts {
		if len(nodes) >= query.count {
			break
		}
		if !query.matches(view.Parts[index]) {
			continue
		}
		nodes = append(nodes, partNode(&view.Parts[index]))
	}
	if a.logger != nil {
		a.logger.Printf(
			"inventory_part_list account=%q user_id=%d stored=%d returned=%d count=%d owned_only=%t creature_filtered=%t creature_id=%d",
			view.LoginName, view.Account.ID, len(view.Parts), len(nodes), query.count,
			query.isOwnedOnly, query.isCreatureFiltered, query.equippedCreatureID,
		)
	}
	writeXML(writer, http.StatusOK, xmlResponse(true, xmlNode("parts", nodes...)))
}

type partListQuery struct {
	count              int
	isOwnedOnly        bool
	equippedCreatureID uint32
	isCreatureFiltered bool
}

func parsePartListQuery(values interface{ Get(string) string }) partListQuery {
	query := partListQuery{count: int(^uint(0) >> 1)}
	if values == nil {
		return query
	}
	rawCount := strings.TrimSpace(values.Get("count"))
	if rawCount != "" {
		count, err := strconv.Atoi(rawCount)
		if err == nil && count >= 0 {
			query.count = count
		}
	}
	for _, field := range strings.Split(values.Get("filter"), ";") {
		field = strings.TrimSpace(field)
		switch {
		case field == "market_status_full-owned":
			query.isOwnedOnly = true
		case strings.HasPrefix(field, "creature_id-"):
			creatureID, err := strconv.ParseUint(strings.TrimPrefix(field, "creature_id-"), 10, 32)
			if err == nil {
				query.equippedCreatureID = uint32(creatureID)
				query.isCreatureFiltered = true
			}
		}
	}
	return query
}

func (query partListQuery) matches(part sporenet.Part) bool {
	if query.isOwnedOnly && part.MarketStatus != 0 {
		return false
	}
	if query.isCreatureFiltered && part.EquippedToCreatureID != query.equippedCreatureID {
		return false
	}
	return true
}

func (a *API) partOffers(writer http.ResponseWriter) {
	// Build 103 consumes packaged offers locally and has no retail XML schema
	// for this compatibility-only endpoint.
	writeXML(writer, http.StatusOK, xmlResponse(true, xmlNode("parts")))
}

func (a *API) creatureResponse(writer http.ResponseWriter, user *sporenet.User, values interface{ Get(string) string }) {
	id, err := strconv.ParseUint(values.Get("id"), 10, 32)
	if err != nil || user == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	var creature *sporenet.Creature
	view := user.View()
	for _, candidate := range view.Creatures {
		if candidate != nil && candidate.ID == uint32(id) {
			creature = candidate
			break
		}
	}
	if creature == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	equippedPart := make([]sporenet.Part, 0)
	if queryBool(values, "include_parts", "includeParts") {
		for _, part := range view.Parts {
			if part.EquippedToCreatureID == creature.ID {
				equippedPart = append(equippedPart, part)
			}
		}
	}
	if a.logger != nil {
		a.logger.Printf("creature_profile account=%q creature_id=%d include_parts=%t equipped=%d", view.LoginName, creature.ID, queryBool(values, "include_parts", "includeParts"), len(equippedPart))
	}
	imageURL := fmt.Sprintf("http://%s:%d/template_png/%d_thumb.png", a.host, a.httpPort, creature.Noun())
	partImageBaseURL := fmt.Sprintf("http://%s:%d/assets/images/loot/", a.host, a.httpPort)
	profileXML := profileCreatureNode(creature, equippedPart, imageURL, partImageBaseURL, a.partCatalog)
	writeXML(writer, http.StatusOK, xmlResponse(true, profileXML))
}

func (a *API) templateResponse(writer http.ResponseWriter, rawID string) {
	id, err := strconv.ParseUint(rawID, 0, 32)
	if err != nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	template := a.template.ByNoun(uint32(id))
	if template == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	creature := sporenet.NewCreature(template)
	creature.ID = template.Noun
	creature.Stats = append([]sporenet.Stat(nil), template.Stats...)
	imageURL := fmt.Sprintf("http://%s:%d/game/service/png?template_id=%d", a.host, a.httpPort, template.Noun)
	profileXML := profileTemplateNode(creature, imageURL, a.partCatalog)
	if a.logger != nil {
		a.logger.Printf("creature_template_profile noun_id=%d", template.Noun)
	}
	writeXML(writer, http.StatusOK, xmlResponse(true, profileXML))
}

func (a *API) survey(writer http.ResponseWriter, _ *http.Request, _ *recaphttp.URI) {
	writeXML(writer, http.StatusOK, xmlResponse(true, xmlNode("surveys")))
}

func (a *API) qos(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	values := requestValues(request, uri)
	writeXML(writer, http.StatusOK, xmlNode("qos", xmlText("numprobes", "2"), xmlText("probesize", "8"), xmlText("qosport", number(a.qosPort)), xmlText("requestid", values.Get("qtyp")), xmlText("reqsecret", "4919")))
}

func (a *API) firewall(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	values := requestValues(request, uri)
	count, err := strconv.Atoi(values.Get("nint"))
	if err != nil || count < 0 {
		count = 0
	}
	ips := make([]string, count)
	ports := make([]string, count)
	for index := 0; index < count; index++ {
		ips[index] = xmlText("ips", a.host)
		ports[index] = xmlText("ports", number(a.qosPort))
	}
	writeXML(writer, http.StatusOK, xmlNode("firewall", xmlText("numinterfaces", strconv.Itoa(count)), xmlNode("ips", ips...), xmlNode("ports", ports...), xmlText("requestid", "1"), xmlText("reqsecret", "4919")))
}

func (a *API) firetype(writer http.ResponseWriter, _ *http.Request, _ *recaphttp.URI) {
	writeXML(writer, http.StatusOK, xmlNode("firetype", xmlText("firetype", "1")))
}

func (a *API) static(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	a.serveStaticFile(writer, request, uri.Resource())
}

func (a *API) profileImage(writer http.ResponseWriter, request *http.Request, _ *recaphttp.URI) {
	query := request.URL.Query()
	templateID, err := strconv.ParseUint(query.Get("template_id"), 10, 32)
	if err == nil && templateID > 0 {
		a.serveStaticFile(writer, request, fmt.Sprintf("template_png/%d_thumb.png", templateID))
		return
	}
	accountID, err := strconv.ParseInt(query.Get("account_id"), 10, 64)
	if err != nil || accountID <= 0 {
		accountID = 0
	}
	avatar := accountProfileAvatar(request.Context(), a.userManager, accountID)
	writer.Header().Set("Cache-Control", "private, no-cache")
	a.serveStaticFile(writer, request, avatar.Target)
}

func accountProfileAvatar(
	ctx context.Context, userManager UserManager, accountID int64,
) contentcache.ProfileAvatar {
	fallback, isFallbackFound := contentcache.ProfileAvatarByID(0)
	if !isFallbackFound {
		return contentcache.ProfileAvatar{}
	}
	if userManager == nil || accountID <= 0 {
		return fallback
	}
	view, err := userManager.PublicProfile(ctx, accountID, "")
	if err != nil {
		return fallback
	}
	avatar, isFound := contentcache.ProfileAvatarByID(view.Account.AvatarID)
	if !isFound {
		return fallback
	}
	return avatar
}

func (a *API) serveStaticFile(writer http.ResponseWriter, request *http.Request, resource string) {
	if a.staticFileSystem == nil {
		http.NotFound(writer, request)
		return
	}
	name := path.Clean(strings.TrimPrefix(resource, "/"))
	if name == "." || !fs.ValidPath(name) {
		http.NotFound(writer, request)
		return
	}
	info, err := fs.Stat(a.staticFileSystem, name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	if info.IsDir() {
		name = path.Join(name, "index.html")
		info, err = fs.Stat(a.staticFileSystem, name)
		if err != nil {
			http.NotFound(writer, request)
			return
		}
	}
	contents, err := fs.ReadFile(a.staticFileSystem, name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	http.ServeContent(writer, request, name, info.ModTime(), bytes.NewReader(contents))
}

func (a *API) creatureImage(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
	a.serveStorageFile(writer, request, a.storage, uri.Resource())
}

func (a *API) serveStorageFile(writer http.ResponseWriter, request *http.Request, root, resource string) {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(resource, "/")))
	path := filepath.Join(rootPath, clean)
	if !strings.HasPrefix(path, rootPath+string(os.PathSeparator)) && path != rootPath {
		http.NotFound(writer, request)
		return
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		path = filepath.Join(path, "index.html")
	}
	http.ServeFile(writer, request, path)
}

func (a *API) userFromRequest(request *http.Request, values interface{ Get(string) string }) *sporenet.User {
	token := authTokenFromRequest(request, values)
	return a.userManager.UserByAuthToken(token)
}

func authTokenFromRequest(request *http.Request, values interface{ Get(string) string }) string {
	token := values.Get("token")
	key := values.Get("key")
	if token == "" && key != "" {
		token = strings.SplitN(key, "::", 2)[0]
	}
	if token == "" || token == "cookie" {
		cookie, err := request.Cookie("token")
		if err == nil {
			token = cookie.Value
		}
	}
	return token
}

func requestValues(request *http.Request, uri *recaphttp.URI) url.Values {
	values := request.URL.Query()
	for key, value := range uri.Parameters() {
		if values.Get(key) == "" {
			values.Set(key, value)
		}
	}
	err := request.ParseMultipartForm(16 << 20)
	if err == nil {
		for key, entries := range request.Form {
			if len(entries) > 0 {
				values.Set(key, entries[len(entries)-1])
			}
		}
	}
	return values
}

func xmlResponse(isSuccess bool, nodes ...string) string {
	stat, code, result := "ok", "200", "1"
	if !isSuccess {
		stat, code, result = "error", "500", "0"
	}
	nodes = append(nodes, xmlText("stat", stat), xmlText("code", code), xmlText("result", result))
	return xmlNode("response", nodes...)
}

func xmlNode(name string, nodes ...string) string {
	return "<" + name + ">" + strings.Join(nodes, "") + "</" + name + ">"
}

func xmlText(name, value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return xmlNode(name, builder.String())
}

func writeXML(writer http.ResponseWriter, status int, body string) {
	writer.Header().Set("Content-Type", "text/xml")
	writer.Header().Set("Content-Language", "en-us")
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(xml.Header + body))
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func partNode(part *sporenet.Part) string {
	if part == nil {
		return ""
	}
	return xmlNode("part",
		xmlText("is_flair", boolNumber(part.IsFlair)),
		xmlText("cost", number(part.Cost)),
		xmlText("creature_id", number(part.EquippedToCreatureID)),
		xmlText("id", number(part.ID)), xmlText("reference_id", number(part.ReferenceID)),
		xmlText("level", number(part.Level)),
		xmlText("market_status", number(uint16(part.MarketStatus)+1)),
		xmlText("rigblock_asset_id", number(part.RigblockAssetHash)),
		xmlText("prefix_asset_id", number(part.PrefixAssetHash)),
		xmlText("prefix_secondary_asset_id", number(part.PrefixSecondaryAssetHash)),
		xmlText("rarity", number(uint16(part.Rarity)+1)),
		xmlText("suffix_asset_id", number(part.SuffixAssetHash)),
		xmlText("status", number(part.Status)), xmlText("usage", number(part.Usage)),
		xmlText("creation_date", number(part.CreationDate)),
	)
}

func creatureNode(creature *sporenet.Creature) string {
	if creature == nil {
		return ""
	}
	return creatureNodeWithImages(creature, creature.LargeImageURL, creature.ThumbImageURL)
}

func (a *API) accountCreatureNode(creature *sporenet.Creature) string {
	if creature == nil {
		return ""
	}
	fallbackImageURL := fmt.Sprintf("http://%s:%d/game/service/png?template_id=%d", a.host, a.httpPort, creature.Noun())
	largeImageURL := defaultText(creature.LargeImageURL, fallbackImageURL+"&size=large")
	thumbImageURL := defaultText(creature.ThumbImageURL, fallbackImageURL+"&size=thumb")
	return creatureNodeWithImages(creature, largeImageURL, thumbImageURL)
}

func creatureNodeWithImages(creature *sporenet.Creature, largeImageURL, thumbImageURL string) string {
	if creature == nil {
		return ""
	}
	nameLocaleID := "0"
	typeName := "all"
	className := "all"
	if creature.Template != nil {
		nameLocaleID = defaultText(creature.Template.NameLocaleID, "0")
		if creature.Template.LocalizationTableID != 0 {
			nameLocaleID = fmt.Sprintf("0x%08x!%s", creature.Template.LocalizationTableID, nameLocaleID)
		}
		typeName = strings.ToLower(defaultText(creature.Template.ElementType, "all"))
		className = strings.ToLower(defaultText(creature.Template.ClassType, "all"))
	}
	return xmlNode("creature",
		xmlText("id", number(creature.ID)), xmlText("name", creature.Name()),
		xmlText("name_locale_id", nameLocaleID), xmlText("noun_id", number(creature.Noun())),
		xmlText("version", number(creature.Version)), xmlText("type_a", typeName), xmlText("class", className),
		xmlText("gear_score", fmt.Sprint(creature.GearScore)), xmlText("item_points", fmt.Sprint(creature.ItemPoints)),
		xmlText("png_large_url", largeImageURL), xmlText("png_thumb_url", thumbImageURL),
	)
}

func profileCreatureNode(creature *sporenet.Creature, parts []sporenet.Part, fallbackImageURL, partImageBaseURL string, partCatalog *PartCatalog) string {
	return profileNode("creature", creature, parts, fallbackImageURL, partImageBaseURL, partCatalog)
}

func profileTemplateNode(creature *sporenet.Creature, fallbackImageURL string, partCatalog *PartCatalog) string {
	return profileNode("template", creature, nil, fallbackImageURL, "", partCatalog)
}

func profileNode(nodeName string, creature *sporenet.Creature, parts []sporenet.Part, fallbackImageURL, partImageBaseURL string, partCatalog *PartCatalog) string {
	if creature == nil || creature.Template == nil {
		return ""
	}
	template := creature.Template
	profile := creatureProfileFieldsFor(template)
	abilityStats := encodeAbilityStats(creature.AbilityStats)
	if abilityStats == "" && template.Noun != 1667741389 && len(creature.Stats) > 0 {
		attributes := profilePartAttributes(parts, partCatalog)
		abilityStats = profileAbilityStatsWithAttributes(template, profile.abilityID, creature.Stats, attributes)
	}
	creatorID := creature.CreatorID
	largeImageURL := defaultText(creature.LargeImageURL, fallbackImageURL)
	thumbImageURL := defaultText(creature.ThumbImageURL, fallbackImageURL)
	partNodes := make([]string, 0, len(parts))
	for index := range parts {
		partNodes = append(partNodes, profilePartNode(&parts[index], partImageBaseURL, partCatalog))
	}
	abilityNodes := make([]string, 0, 5)
	for _, abilityID := range profile.abilityID {
		ability := profile.abilities[abilityID]
		abilityNodes = append(abilityNodes, xmlNode("ability", xmlText("id", number(abilityID)), xmlText("locale_name", ability.nameLocaleID), xmlText("locale_description", ability.descriptionLocaleID)))
	}
	nodes := []string{
		xmlText("id", number(creature.ID)), xmlText("noun_id", number(creature.Noun())),
		xmlText("version", number(creature.Version)), xmlText("name_locale_id", profile.nameLocaleID),
		xmlText("text_locale_id", profile.descriptionLocaleID), xmlText("name", creature.Name()),
		xmlText("type_a", strings.ToLower(template.ElementType)), xmlText("creator_id", strconv.FormatInt(creatorID, 10)),
		xmlText("weapon_min_damage", fmt.Sprint(template.WeaponMinDamage)), xmlText("weapon_max_damage", fmt.Sprint(template.WeaponMaxDamage)),
		xmlText("gear_score", fmt.Sprint(creature.GearScore)), xmlText("item_points", fmt.Sprint(creature.ItemPoints)),
		xmlText("class", strings.ToLower(template.ClassType)),
		xmlText("stats", profileStatsText(creature.Stats, profile.stats)),
		xmlText("stats_template", profileStatsText(template.Stats, profile.stats)),
		xmlText("stats_template_ability", profile.abilityBase),
		xmlText("stats_template_ability_keyvalues", profile.abilityStats),
		xmlText("stats_ability_keyvalues", defaultText(abilityStats, profile.abilityStats)),
		xmlNode("parts", partNodes...), xmlText("creature_parts", creaturePartsText(template.EquipableParts)),
		xmlText("ability_passive", number(profile.abilityID[0])), xmlText("ability_basic", number(profile.abilityID[1])),
		xmlText("ability_random", number(profile.abilityID[2])), xmlText("ability_special_1", number(profile.abilityID[3])),
		xmlText("ability_special_2", number(profile.abilityID[4])),
	}
	nodes = append(nodes, abilityNodes...)
	nodes = append(nodes, xmlText("png_large_url", largeImageURL), xmlText("png_thumb_url", thumbImageURL))
	return xmlNode(nodeName, nodes...)
}

const emptyProfileStats = "STR,0,0;DEX,0,0;MIND,0,0;HLTH,0,0;MANA,0,0;PDEF,0,0;EDEF,0,0;CRTR,0,0;CRTD,0,0;PROS,0,0;COOL,0,0;AOEDMG,0,0;AOERES,0,0;MOV,0,0;AOEDUR,0,0;LFSTL,0,0;"

type creatureProfileAbility struct {
	nameLocaleID        string
	descriptionLocaleID string
}

type creatureProfileFields struct {
	nameLocaleID        string
	descriptionLocaleID string
	stats               string
	abilityBase         string
	abilityStats        string
	abilityID           [5]uint32
	abilities           map[uint32]creatureProfileAbility
}

func creatureProfileFieldsFor(template *sporenet.TemplateCreature) creatureProfileFields {
	localeGroup := ""
	if template.LocalizationTableID != 0 {
		localeGroup = fmt.Sprintf("0x%08x!", template.LocalizationTableID)
	}
	profile := creatureProfileFields{
		nameLocaleID:        localeGroup + defaultText(template.NameLocaleID, "0"),
		descriptionLocaleID: localeGroup + defaultText(template.DescriptionLocaleID, "0"),
		stats:               profileStatsText(template.Stats, emptyProfileStats),
		abilityBase:         "0!0!0;",
		abilityStats:        "0!0,0;",
		abilityID:           [5]uint32{template.AbilityPassive, template.AbilityBasic, template.AbilityRandom, template.AbilitySpecial1, template.AbilitySpecial2},
		abilities:           make(map[uint32]creatureProfileAbility, 5),
	}
	for _, abilityID := range profile.abilityID {
		abilityLocale := template.AbilityLocale[abilityID]
		abilityLocaleGroup := localeGroup
		if abilityLocale.LocalizationTableID != 0 {
			abilityLocaleGroup = fmt.Sprintf(
				"0x%08x!", abilityLocale.LocalizationTableID,
			)
		}
		profile.abilities[abilityID] = creatureProfileAbility{
			nameLocaleID: abilityLocaleGroup +
				defaultText(abilityLocale.NameLocaleID, "0"),
			descriptionLocaleID: abilityLocaleGroup +
				defaultText(abilityLocale.DescriptionLocaleID, "0"),
		}
	}
	if profile.abilityID != [5]uint32{} {
		profile.abilityBase = strings.Join([]string{
			fmt.Sprintf("%d!DMG!minDamage,maxDamage", profile.abilityID[1]),
			fmt.Sprintf("%d!DMG!minDamage,maxDamage", profile.abilityID[3]),
			fmt.Sprintf("%d!DMG!minDamage,maxDamage", profile.abilityID[4]),
			fmt.Sprintf("%d!DMG!minDamage,maxDamage", profile.abilityID[2]),
			fmt.Sprintf("%d!CRTD!percent", profile.abilityID[0]),
		}, ";") + ";"
		profile.abilityStats = profileAbilityStats(template, profile.abilityID)
	}
	if template.Noun != 1667741389 {
		return profile
	}
	if localeGroup == "" {
		localeGroup = "0xdf75b8ce!"
	}
	profile.nameLocaleID = localeGroup + "0x0ababafd"
	profile.descriptionLocaleID = localeGroup + "0x0acaf252"
	profile.abilityID = [5]uint32{4022963036, 868969257, 3492557026, 2779439490, 1137096183}
	profile.abilityBase = strings.Join([]string{
		"868969257!DMG!minDamage,maxDamage",
		"2779439490!DMG!minDamage,maxDamage",
		"1137096183!DMG!minDamage,maxDamage",
		"3492557026!DMG!minDamage,maxDamage",
		"4022963036!CRTD!percent",
	}, ";") + ";"
	profile.abilityStats = strings.Join([]string{
		"868969257!minDamage,4", "868969257!maxDamage,12",
		"1137096183!minDamage,21", "1137096183!maxDamage,32", "1137096183!stunDuration,3",
		"3492557026!minSecondaryDamage,7", "3492557026!maxSecondaryDamage,18",
		"3492557026!minDamage,21", "3492557026!maxDamage,35", "3492557026!radius,4",
		"2779439490!numOrbs,6", "2779439490!minDamage,14", "2779439490!maxDamage,35",
		"2779439490!deflectionIncrease,100", "4022963036!percent,50",
	}, ";") + ";"
	profile.abilities = map[uint32]creatureProfileAbility{
		868969257:  {nameLocaleID: localeGroup + "0x08a64f51", descriptionLocaleID: localeGroup + "0x08a64f52"},
		2779439490: {nameLocaleID: localeGroup + "0x08916351", descriptionLocaleID: localeGroup + "0x08916352"},
		1137096183: {nameLocaleID: localeGroup + "0x08916353", descriptionLocaleID: localeGroup + "0x08916354"},
		3492557026: {nameLocaleID: localeGroup + "0x08916355", descriptionLocaleID: localeGroup + "0x08916356"},
		4022963036: {nameLocaleID: localeGroup + "0x09506544", descriptionLocaleID: localeGroup + "0x09506545"},
	}
	return profile
}

func profileAbilityStats(template *sporenet.TemplateCreature, abilityID [5]uint32) string {
	return profileAbilityStatsWithAttributes(template, abilityID, template.Stats, [partAttributeCount]float32{})
}

func profileAbilityStatsWithStats(template *sporenet.TemplateCreature, abilityID [5]uint32, stats []sporenet.Stat) string {
	return profileAbilityStatsWithAttributes(template, abilityID, stats, [partAttributeCount]float32{})
}

type profileAbilityTokenBuilder struct {
	fields     []string
	seenFields map[string]bool
}

func newProfileAbilityTokenBuilder() *profileAbilityTokenBuilder {
	return &profileAbilityTokenBuilder{
		fields: make([]string, 0, 24), seenFields: make(map[string]bool),
	}
}

func (e *profileAbilityTokenBuilder) append(
	id uint32, token string, number float64,
) {
	if id == 0 || token == "" {
		return
	}
	identity := fmt.Sprintf("%d/%s", id, token)
	if e.seenFields[identity] {
		return
	}
	e.seenFields[identity] = true
	e.fields = append(
		e.fields,
		fmt.Sprintf("%d!%s,%s", id, token, profileAbilityNumber(number)),
	)
}

func (e *profileAbilityTokenBuilder) text() string {
	return strings.Join(e.fields, ";") + ";"
}

func profileAbilityStatsWithAttributes(
	template *sporenet.TemplateCreature, abilityID [5]uint32,
	stats []sporenet.Stat, attributes [partAttributeCount]float32,
) string {
	builder := newProfileAbilityTokenBuilder()
	builder.append(abilityID[1], "minDamage", template.WeaponMinDamage)
	builder.append(abilityID[1], "maxDamage", template.WeaponMaxDamage)
	for _, id := range []uint32{abilityID[1], abilityID[3], abilityID[4], abilityID[2], abilityID[0]} {
		properties := template.AbilityProperty[id]
		for _, property := range properties {
			name := property.Name
			if name == "" {
				continue
			}
			sourceName := defaultText(property.SourceName, name)
			if strings.EqualFold(name, "damage") {
				builder.append(id, "minDamage", profileAbilityProjectedNumber(template, stats, properties, "minDamage", property.SourceTableName, sourceName, property.Coefficient, property.Minimum, attributes))
				builder.append(id, "maxDamage", profileAbilityProjectedNumber(template, stats, properties, "maxDamage", property.SourceTableName, sourceName, property.Coefficient, property.Maximum, attributes))
				continue
			}
			builder.append(id, name, profileAbilityProjectedNumber(template, stats, properties, name, property.SourceTableName, sourceName, property.Coefficient, property.Minimum, attributes))
			if property.Minimum == property.Maximum {
				continue
			}
			suffix := strings.ToUpper(name[:1]) + name[1:]
			builder.append(id, "min"+suffix, profileAbilityProjectedNumber(template, stats, properties, "min"+suffix, property.SourceTableName, sourceName, property.Coefficient, property.Minimum, attributes))
			builder.append(id, "max"+suffix, profileAbilityProjectedNumber(template, stats, properties, "max"+suffix, property.SourceTableName, sourceName, property.Coefficient, property.Maximum, attributes))
		}
	}
	for _, fallback := range []struct {
		id    uint32
		token string
	}{
		{id: abilityID[3], token: "minDamage"}, {id: abilityID[3], token: "maxDamage"},
		{id: abilityID[4], token: "minDamage"}, {id: abilityID[4], token: "maxDamage"},
		{id: abilityID[2], token: "minDamage"}, {id: abilityID[2], token: "maxDamage"},
		{id: abilityID[0], token: "percent"},
	} {
		builder.append(fallback.id, fallback.token, 0)
	}
	return builder.text()
}

func profileAbilityProjectedNumber(template *sporenet.TemplateCreature, stats []sporenet.Stat, properties []sporenet.AbilityProperty, token, sourceTableName, sourceName string, coefficient, number float64, attributes [partAttributeCount]float32) float64 {
	lowerToken := strings.ToLower(token)
	isMinimumDamage := lowerToken == "mindamage" || lowerToken == "minsecondarydamage" || lowerToken == "petmindamage"
	isMaximumDamage := lowerToken == "maxdamage" || lowerToken == "maxsecondarydamage" || lowerToken == "petmaxdamage" || lowerToken == "damage"
	isHealing := lowerToken == "minhealing" || lowerToken == "maxhealing" || lowerToken == "healing"
	if !isMinimumDamage && !isMaximumDamage && !isHealing {
		return number
	}
	coefficientName := []string{sourceName + "Coefficient", "damageCoefficient"}
	if isHealing {
		coefficientName = []string{"healingCoefficient"}
	}
	if coefficient == 0 {
		coefficient = profileAbilityCoefficient(properties, sourceTableName, coefficientName)
	}
	primary, isFound := profilePrimaryAttribute(template, stats)
	if isHealing {
		projected := number
		if isFound && coefficient != 0 {
			// Game 5.3.0.103 sub_43A170 routes damage tokens through
			// sub_9E5B10/sub_9E4E60. The latter applies the authored coefficient to
			// the class-selected primary attribute relative to the build's -1 base.
			projected *= 1 + (primary+1)*coefficient
		}
		return math.Floor(projected)
	}
	ability := profileAbilityDamage(properties, sourceTableName, float32(number), float32(coefficient))
	projected, err := ResolveAbilityDamageRange(
		ability, damageProfile(float32(primary), isFound, attributes),
	)
	if err != nil {
		return 0
	}
	if isMinimumDamage {
		return float64(projected.Minimum)
	}
	return float64(projected.Maximum)
}

func profileAbilityDamage(
	properties []sporenet.AbilityProperty, sourceTableName string, baseDamage float32, coefficient float32,
) AbilityDamage {
	ability := AbilityDamage{Minimum: baseDamage, Maximum: baseDamage, Coefficient: coefficient}
	descriptorNumber, isDescriptorFound := profileAbilitySourceNumber(properties, sourceTableName, "descriptors")
	if isDescriptorFound && descriptorNumber >= 0 && descriptorNumber <= math.MaxUint32 && descriptorNumber == math.Trunc(descriptorNumber) {
		ability.Descriptor = uint32(descriptorNumber)
		ability.IsDescriptorFound = true
	}
	damageType, isDamageTypeFound := profileAbilitySourceNumber(properties, sourceTableName, "damageType")
	if isDamageTypeFound && damageType >= 0 && damageType <= 4 && damageType == math.Trunc(damageType) {
		ability.DamageType = uint8(damageType)
		ability.IsDamageTypeFound = true
	}
	damageSource, isDamageSourceFound := profileAbilitySourceNumber(properties, sourceTableName, "damageSource")
	if isDamageSourceFound && damageSource >= 0 && damageSource <= math.MaxUint8 && damageSource == math.Trunc(damageSource) {
		ability.DamageSource = uint8(damageSource)
		ability.IsDamageSourceFound = true
	}
	return ability
}

func profileAbilitySourceNumber(properties []sporenet.AbilityProperty, sourceTableName, propertyName string) (float64, bool) {
	for _, property := range properties {
		if property.SourceTableName == sourceTableName && strings.EqualFold(property.Name, propertyName) {
			return property.Minimum, true
		}
	}
	return 0, false
}

func profilePartAttributes(parts []sporenet.Part, partCatalog *PartCatalog) [partAttributeCount]float32 {
	var result [partAttributeCount]float32
	for index := range parts {
		attributes, isFound := partCatalog.runtimeAttributes(&parts[index])
		if !isFound {
			continue
		}
		for attributeIndex, amount := range attributes {
			result[attributeIndex] += amount
		}
	}
	return result
}

func profileAbilityCoefficient(properties []sporenet.AbilityProperty, sourceTableName string, coefficientName []string) float64 {
	for _, expectedName := range coefficientName {
		for _, property := range properties {
			if property.SourceTableName == sourceTableName && strings.EqualFold(property.Name, expectedName) {
				return property.Minimum
			}
		}
	}
	return 0
}

func profilePrimaryAttribute(template *sporenet.TemplateCreature, stats []sporenet.Stat) (float64, bool) {
	statName := map[sporenet.CreatureClass]string{
		sporenet.CreatureRavager:  "STR",
		sporenet.CreatureSentinel: "DEX",
		sporenet.CreatureTempest:  "MIND",
	}[template.Class]
	if statName == "" {
		return 0, false
	}
	for _, stat := range stats {
		if stat.Name == statName {
			return float64(stat.Maximum), true
		}
	}
	return 0, false
}

func profileAbilityNumber(number float64) string {
	return strconv.FormatFloat(float64(float32(number)), 'f', -1, 32)
}

func profilePartNode(part *sporenet.Part, partImageBaseURL string, partCatalog *PartCatalog) string {
	if part == nil {
		return ""
	}
	definition, isDefined := partCatalog.ByRigblock(part.RigblockAssetID)
	partType := "utility"
	classType := "all"
	scienceType := "all"
	if isDefined {
		partType = definition.SlotType
		classType = definition.ClassType
		scienceType = definition.ScienceType
	}
	rarityName := []string{"basic", "uncommon", "rare", "epic", "unique", "rareunique", "epicunique"}
	rarity := int(part.Rarity)
	if rarity < 0 || rarity >= len(rarityName) {
		rarity = 0
	}
	return xmlNode("part",
		xmlText("type_full", partType), xmlText("is_flair", boolNumber(part.IsFlair)), xmlText("stats", partCatalog.Stats(part)),
		xmlText("cost", number(part.Cost)), xmlText("level", number(part.Level)), xmlText("class_types_full", classType),
		xmlText("science_types_full", scienceType), xmlText("rarity_full", rarityName[rarity]),
		xmlText("rigblock_asset_id", number(part.RigblockAssetHash)), xmlText("png_key", profilePartIcon(part, partType, partImageBaseURL, isDefined)),
		xmlText("suffix_asset_id", number(part.SuffixAssetHash)), xmlText("prefix_asset_id", number(part.PrefixAssetHash)),
		xmlText("prefix_secondary_asset_id", number(part.PrefixSecondaryAssetHash)),
		xmlText("rarity", number(uint16(part.Rarity)+1)),
		xmlText("weapon_damage_modifier", profilePartNumber(partCatalog.WeaponDamageModifier(part))),
	)
}

func profilePartNumber(number float32) string {
	return strconv.FormatFloat(float64(number), 'f', -1, 32)
}

func profilePartIcon(part *sporenet.Part, partType, partImageBaseURL string, isDefined bool) string {
	if part != nil && part.RigblockAssetID != 0 && isDefined {
		return partImageBaseURL + strconv.FormatUint(uint64(part.RigblockAssetID), 10) + ".png"
	}
	return map[string]string{
		"defense": "images/icon_editor_defense_20x20.png",
		"offense": "images/icon_editor_offense_20x20.png",
		"utility": "images/icon_editor_utility_20x20.png",
		"grasper": "images/icon_editor_hand_20x20.png",
		"foot":    "images/icon_editor_feet_20x20.png",
		"weapon":  "images/inventory_icon_weapon_20x20.png",
		"detail":  "images/icon_editor_flair_20x20.png",
	}[partType]
}

func defaultText(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

func creaturePartsText(parts sporenet.CreatureParts) string {
	switch parts {
	case sporenet.CreatureNoFeet:
		return "no_feet"
	case sporenet.CreatureNoHands:
		return "no_hands"
	default:
		return "all"
	}
}

func encodeStats(stats []sporenet.Stat) string {
	fields := make([]string, 0, len(stats))
	for _, stat := range stats {
		fields = append(fields, fmt.Sprintf("%s,%d,%d", stat.Name, stat.Maximum, stat.Current))
	}
	return strings.Join(fields, ";")
}

func profileStatsText(stats []sporenet.Stat, fallback string) string {
	if len(stats) == 0 {
		return fallback
	}
	fields := make([]string, 0, len(stats)+8)
	isPresent := make(map[string]bool, len(stats))
	for _, stat := range stats {
		fields = append(fields, fmt.Sprintf("%s,%d,%d", stat.Name, stat.Maximum, stat.Current))
		isPresent[stat.Name] = true
	}
	for _, field := range strings.Split(fallback, ";") {
		parts := strings.SplitN(field, ",", 2)
		if len(parts) != 2 || isPresent[parts[0]] {
			continue
		}
		fields = append(fields, field)
		isPresent[parts[0]] = true
	}
	return strings.Join(fields, ";") + ";"
}

func encodeAbilityStats(stats []sporenet.AbilityStat) string {
	fields := make([]string, 0, len(stats))
	for _, stat := range stats {
		fields = append(fields, stat.Token+"!"+stat.Value)
	}
	return strings.Join(fields, ";")
}

func number(value any) string { return fmt.Sprint(value) }

func parseUint32(value string) uint32 {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(parsed)
}

func parseUint32List(value string) []uint32 {
	fields := strings.Split(value, ",")
	result := make([]uint32, 0, len(fields))
	for _, field := range fields {
		result = append(result, parseUint32(field))
	}
	return result
}

func optionalUint32(value string) (*uint32, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("optionalUint: %w", err)
	}
	result := uint32(parsed)
	return &result, nil
}

func parseSettings(value string) map[string]string {
	settings := make(map[string]string)
	for _, entry := range strings.Split(value, ";") {
		parts := strings.SplitN(entry, ",", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if validXMLName(key) {
			settings[key] = strings.TrimSpace(parts[1])
		}
	}
	return settings
}

func validXMLName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' || index > 0 && (character == '-' || character == '.') {
			continue
		}
		return false
	}
	return true
}
func boolText(isTrue bool) string {
	if isTrue {
		return "Y"
	}
	return "N"
}
func boolNumber(isTrue bool) string {
	if isTrue {
		return "1"
	}
	return "0"
}

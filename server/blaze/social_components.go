package blaze

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/party"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
)

var nextSystemMessageID atomic.Uint64

const roomViewDisplayShownSessionKey = "rooms.viewDisplayShown"

const (
	roomsErrorNotFound           uint16 = 0x000b
	roomsErrorFull               uint16 = 0x000c
	roomsErrorUnknownCategory    uint16 = 0x0019
	messagingErrorUnknown        uint16 = 0x0001
	messagingErrorTargetNotFound uint16 = 0x0004
	messagingErrorTargetType     uint16 = 0x0005
	userSessionObjectType        uint64 = 1
	gameManagerObjectType        uint64 = 1
	messagingPartyInviteType     uint64 = 1
	messagingChatType            uint64 = 2
	messagingBodyAttribute       uint64 = 0xff02
	playgroupNetworkTopology     uint64 = 0
)

type associationManager interface {
	UpdateUserAssociations(
		context.Context, *sporenet.User, uint32, []sporenet.AssociationMember, bool,
	) error
}

type socialUserManager interface {
	associationManager
	UserByID(int64) *sporenet.User
	UserByDisplayName(string) *sporenet.User
	UserByLoginName(string) *sporenet.User
}

type playgroupRelay interface {
	Endpoint() (netip.AddrPort, bool)
	RegisterMember(uint32, int64, []netip.AddrPort)
	RemoveMember(uint32, int64)
	RemoveParty(uint32)
}

type matchmakingDirectory interface {
	IsQueued(int64) bool
}

// RegisterSocialComponents adds association, messaging, playgroup, and room RPCs.
func RegisterSocialComponents(
	registry *Registry, roomManager *sporenet.RoomManager, messenger *chat.Service,
	userManager socialUserManager, partyService *party.Service,
	relay playgroupRelay, matchmaking matchmakingDirectory,
	presenceRegistries ...*PresenceRegistry,
) error {
	if roomManager == nil {
		return errors.New("register social components: nil room manager")
	}
	if messenger == nil {
		return errors.New("register social components: nil chat service")
	}
	if userManager == nil {
		return errors.New("register social components: nil association manager")
	}
	if partyService == nil {
		return errors.New("register social components: nil party service")
	}
	presenceRegistry := NewPresenceRegistry()
	if len(presenceRegistries) != 0 && presenceRegistries[0] != nil {
		presenceRegistry = presenceRegistries[0]
	}
	components := []Component{
		associationComponent(userManager), messagingComponent(messenger, partyService),
		playgroupsComponent(
			partyService, userManager, presenceRegistry, relay, matchmaking,
		),
		roomsComponent(roomManager, presenceRegistry),
	}
	for _, component := range components {
		err := registry.Register(component)
		if err != nil {
			return fmt.Errorf("socialRegister[%q]: %w", component.Name, err)
		}
	}
	return nil
}

func associationComponent(userManager socialUserManager) Component {
	empty := func(context.Context, *Request) (*Response, error) { return &Response{}, nil }
	return Component{ID: AssociationComponentID, Name: "AssociationLists", Commands: map[uint16]Handler{
		0x01: associationUpdateHandler(userManager, true),
		0x02: associationUpdateHandler(userManager, false),
		0x03: empty,
		0x04: empty,
		0x05: empty,
		0x06: associationGetListsHandler,
		0x07: empty,
		0x08: empty,
	}}
}

func associationUpdateHandler(userManager socialUserManager, isAddition bool) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		listType := associationListType(request.Fields)
		members, isResolved := associationMembers(userManager, request.Fields)
		if !isResolved {
			return &Response{ErrorCode: 0x0001}, nil
		}
		if isAddition && associationContainsUser(members, user.Account.ID) {
			return &Response{ErrorCode: 0x0001}, nil
		}
		if isAddition && associationContainsMember(user.AssociationSnapshot(listType), members) {
			return &Response{ErrorCode: 0x0005}, nil
		}
		err := userManager.UpdateUserAssociations(ctx, user, listType, members, isAddition)
		if err != nil {
			return nil, fmt.Errorf("associationUpdate: %w", err)
		}
		for index, member := range members {
			target := userManager.UserByID(member.ID)
			if target == nil {
				continue
			}
			err = request.QueueNotification(
				UserSessionComponentID, userSessionUserUpdated,
				userUpdatedNotificationFields(target),
			)
			if err != nil {
				return nil, fmt.Errorf("associationPresence[%d]: %w", index, err)
			}
		}
		if !isAddition {
			removedMembers := make([]tdf.Value, 0, len(members))
			for _, member := range members {
				removedMembers = append(
					removedMembers, tdf.StructValue(associationMemberIDFields(member)...),
				)
			}
			return &Response{Fields: []tdf.Field{
				tdf.FieldNamed("REM", tdf.ListValue(tdf.Struct, removedMembers...)),
			}}, nil
		}
		addedMembers := make([]tdf.Value, 0, len(members))
		for _, member := range members {
			addedMembers = append(
				addedMembers, tdf.StructValue(associationMemberFields(member)...),
			)
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("LMID", tdf.ListValue(tdf.Struct, addedMembers...)),
		}}, nil
	}
}

func associationContainsMember(
	currentMembers []sporenet.AssociationMember, requestedMembers []sporenet.AssociationMember,
) bool {
	for _, requestedMember := range requestedMembers {
		for _, currentMember := range currentMembers {
			if currentMember.ID == requestedMember.ID {
				return true
			}
		}
	}
	return false
}

func associationContainsUser(members []sporenet.AssociationMember, userID int64) bool {
	for _, member := range members {
		if member.ID == userID {
			return true
		}
	}
	return false
}

func associationGetListsHandler(_ context.Context, request *Request) (*Response, error) {
	user := requestUser(request)
	if user == nil {
		return &Response{ErrorCode: authInvalidUser}, nil
	}
	requested, isFound := tdf.Find(request.Fields, "ALST")
	lists := make([]tdf.Value, 0)
	if isFound && requested.Type == tdf.List {
		for _, item := range requested.Items {
			listType := associationListType(item.Fields)
			members := user.AssociationSnapshot(listType)
			memberValues := make([]tdf.Value, 0, len(members))
			for _, member := range members {
				memberValues = append(memberValues, tdf.StructValue(associationMemberFields(member)...))
			}
			listInfoFields := append([]tdf.Field(nil), item.Fields...)
			fields := []tdf.Field{
				tdf.FieldNamed("INFO", tdf.StructValue(listInfoFields...)),
				tdf.FieldNamed("MEML", tdf.ListValue(tdf.Struct, memberValues...)),
				tdf.FieldNamed("OFRC", tdf.IntegerValue(fieldInteger(request.Fields, "OFRC"))),
				tdf.FieldNamed("TOCT", tdf.IntegerValue(uint64(len(members)))),
			}
			lists = append(lists, tdf.StructValue(fields...))
		}
	}
	return &Response{Fields: []tdf.Field{tdf.FieldNamed("LMAP", tdf.ListValue(tdf.Struct, lists...))}}, nil
}

func associationMembers(userManager socialUserManager, fields []tdf.Field) ([]sporenet.AssociationMember, bool) {
	value, isFound := tdf.Find(fields, "BIDL")
	if !isFound || value.Type != tdf.List {
		return nil, false
	}
	members := make([]sporenet.AssociationMember, 0, len(value.Items))
	for _, item := range value.Items {
		memberFields := item.Fields
		info, hasInfo := tdf.Find(memberFields, "INFO")
		if hasInfo && info.Type == tdf.Struct {
			memberFields = info.Fields
		}
		memberIDValue, hasID := tdf.Find(memberFields, "LMID")
		if hasID && memberIDValue.Type == tdf.Struct {
			memberFields = memberIDValue.Fields
		}
		memberID := int64(fieldInteger(memberFields, "BLID"))
		memberName := fieldString(memberFields, "PNAM")
		target := userManager.UserByID(memberID)
		if target == nil && memberName != "" {
			target = userManager.UserByDisplayName(memberName)
			if target == nil {
				target = userManager.UserByLoginName(memberName)
			}
		}
		if target == nil {
			return nil, false
		}
		members = append(members, sporenet.AssociationMember{
			ID:   target.Account.ID,
			Name: target.DisplayName,
			Time: uint64(time.Now().Unix()),
		})
	}
	return members, len(members) != 0
}

func associationMemberFields(member sporenet.AssociationMember) []tdf.Field {
	id := tdf.StructValue(associationMemberIDFields(member)...)
	return []tdf.Field{
		tdf.FieldNamed("LMID", id),
		tdf.FieldNamed("TIME", tdf.IntegerValue(member.Time)),
	}
}

func associationMemberIDFields(member sporenet.AssociationMember) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("BLID", tdf.IntegerValue(uint64(member.ID))),
		tdf.FieldNamed("PNAM", tdf.StringValue(member.Name)),
		tdf.FieldNamed("XREF", tdf.IntegerValue(0)),
		tdf.FieldNamed("XTYP", tdf.IntegerValue(0)),
	}
}

func associationListType(fields []tdf.Field) uint32 {
	value, isFound := tdf.Find(fields, "LID")
	if isFound && value.Type == tdf.Struct {
		return uint32(fieldInteger(value.Fields, "TYPE"))
	}
	return uint32(fieldInteger(fields, "TYPE"))
}

func associationListID(listType uint32) tdf.Value {
	return tdf.StructValue(
		tdf.FieldNamed("LNM", tdf.StringValue("")),
		tdf.FieldNamed("TYPE", tdf.IntegerValue(uint64(listType))),
	)
}

func messagingComponent(messenger *chat.Service, partyService *party.Service) Component {
	emptyCount := func(context.Context, *Request) (*Response, error) {
		return &Response{Fields: []tdf.Field{tdf.FieldNamed("MCNT", tdf.IntegerValue(0))}}, nil
	}
	empty := func(context.Context, *Request) (*Response, error) { return &Response{}, nil }
	return Component{ID: MessagingComponentID, Name: "Messaging", Commands: map[uint16]Handler{
		0x01: messagingSendHandler(messenger, partyService), 0x02: emptyCount, 0x03: emptyCount, 0x04: empty, 0x05: empty,
	}}
}

func messagingSendHandler(messenger *chat.Service, partyService *party.Service) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		startedAt := time.Now()
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		messageType := fieldInteger(request.Fields, "TYPE")
		target, err := messagingTarget(request.Fields)
		if err != nil {
			return &Response{ErrorCode: messagingErrorTargetType}, nil
		}
		partyID, isPartyInvite := messagingPartyInvite(request.Fields)
		if isPartyInvite {
			if messageType != messagingPartyInviteType {
				return &Response{ErrorCode: messagingErrorTargetType}, nil
			}
			return forwardPartyInvite(ctx, request, user, target, partyID, partyService)
		}
		if messageType != messagingChatType {
			return &Response{ErrorCode: messagingErrorTargetType}, nil
		}
		body, isFound := messagingBody(request.Fields)
		if !isFound {
			return &Response{ErrorCode: messagingErrorTargetType}, nil
		}
		commandName, isCommand := messagingCommandName(body)
		if isCommand {
			responseBody := fmt.Sprintf("Unknown command %s. Available: /help, /taunt, /ss, /bug, /b, /ping, /hint, /loc, /stat, /follow, /ai, /effect, /summon, /level, /warp, /spawn, /drop, /dna, /damage, /heal, /power, /mana, /event, /goto, /kill, /reset, /recap, /victory, /defeat, /exit", commandName)
			if commandName == "/help" {
				responseBody = darkspinChatHelp
			}
			if commandName == "/taunt" {
				responseBody = "Unfortunately, /taunt was added in a newer Darkspore build than Darkspin supports, so taunt animations are unavailable."
			}
			if commandName == "/bug" || commandName == "/b" {
				responseBody = bugReportSyntax
				description := commandRemainder(body)
				if description != "" {
					reportName, reportErr := messenger.ReportBug(ctx, chat.BugCommand{
						Sender:      chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID:      user.CurrentGameID(),
						Description: description,
					})
					if reportErr == nil {
						responseBody = "Bug report queued as " + reportName
					} else if !errors.Is(reportErr, chat.ErrBugReportUnavailable) {
						return nil, fmt.Errorf("commandBug: %w", reportErr)
					}
				}
			}
			if commandName == "/ss" {
				field := strings.Fields(body)
				snapshotCommand := chat.SnapshotCommand{
					Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
					GameID: user.CurrentGameID(),
				}
				if len(field) >= 2 {
					snapshotCommand.Action = field[1]
					if len(field) == 3 {
						snapshotCommand.Argument = field[2]
					}
					if len(field) > 3 {
						snapshotCommand.Action = "invalid"
					}
				}
				result, snapshotErr := messenger.Snapshot(ctx, snapshotCommand)
				if snapshotErr == nil {
					responseBody = result.Message
				} else if errors.Is(snapshotErr, chat.ErrSnapshotUnavailable) {
					responseBody = "Sync Snapshot is unavailable on this server"
				} else {
					return nil, fmt.Errorf("commandSnapshot: %w", snapshotErr)
				}
			}
			if commandName == "/ping" {
				ping, pingErr := messenger.Ping(ctx, chat.PingCommand{
					Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
					GameID: user.CurrentGameID(),
				})
				if pingErr != nil {
					return nil, fmt.Errorf("commandPing: %w", pingErr)
				}
				responseBody = fmt.Sprintf(
					"Pong | server_processing=%dus | blaze_session=%d | game=%d | players=%d",
					time.Since(startedAt).Microseconds(), request.Session.ID,
					ping.GameID, ping.PlayerCount,
				)
			}
			if commandName == "/hint" {
				result, hintErr := messenger.Hint(ctx, chat.HintRequest{
					Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
					GameID: user.CurrentGameID(),
				})
				if hintErr == nil && result.Name == "" {
					responseBody = "No living hostile mobs remain in this deployment"
				} else if hintErr == nil {
					responseBody = fmt.Sprintf("Hint: %s is %s", result.Name, result.Direction)
				} else if errors.Is(hintErr, chat.ErrHintUnavailable) {
					responseBody = "Hint only works during an active deployment"
				} else {
					return nil, fmt.Errorf("commandHint: %w", hintErr)
				}
			}
			if commandName == "/loc" {
				responseBody = locSyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					location, locationErr := messenger.Location(ctx, chat.LocationRequest{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(),
					})
					if locationErr == nil {
						responseBody = fmt.Sprintf(
							"Position: X=%.4f Y=%.4f Z=%.4f",
							location.X, location.Y, location.Z,
						)
					} else if errors.Is(locationErr, chat.ErrLocationUnavailable) {
						responseBody = "Location only works during an active deployment"
					} else {
						return nil, fmt.Errorf("commandLocation: %w", locationErr)
					}
				}
			}
			if commandName == "/stat" {
				responseBody = "Stats only work during an active deployment"
				field := strings.Fields(body)
				req := chat.ResourceStatusRequest{
					Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
					GameID: user.CurrentGameID(),
				}
				if len(field) == 5 {
					objectID, isObjectValid := parseCommandUint(field[1], 32)
					hitPointBits, isHitPointValid := parseCommandUint(field[2], 32)
					powerPointBits, isPowerPointValid := parseCommandUint(field[3], 32)
					mask, isMaskValid := parseCommandUint(field[4], 8)
					if isObjectValid && isHitPointValid && isPowerPointValid && isMaskValid {
						req.ClientObjectID = uint32(objectID)
						req.ClientHitPoint = math.Float32frombits(uint32(hitPointBits))
						req.ClientPowerPoint = math.Float32frombits(uint32(powerPointBits))
						req.IsClientHitPointSet = mask&1 != 0 &&
							!math.IsNaN(float64(req.ClientHitPoint)) &&
							!math.IsInf(float64(req.ClientHitPoint), 0) &&
							req.ClientHitPoint >= 0
						req.IsClientPowerPointSet = mask&2 != 0 &&
							!math.IsNaN(float64(req.ClientPowerPoint)) &&
							!math.IsInf(float64(req.ClientPowerPoint), 0) &&
							req.ClientPowerPoint >= 0
					}
				}
				if len(field) == 1 || len(field) == 5 {
					status, statusErr := messenger.ResourceStatus(ctx, req)
					if statusErr == nil {
						responseBody = formatResourceStatus(status, req)
					} else if !errors.Is(statusErr, chat.ErrResourceStatusUnavailable) {
						return nil, fmt.Errorf("commandResourceStatus: %w", statusErr)
					}
				}
			}
			if commandName == "/ai" {
				responseBody = "Syntax: /ai (toggle assistance; movement cancels)"
				if len(strings.Fields(body)) == 1 {
					err := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "ai",
					})
					if err == nil {
						responseBody = "AI toggle requested: assist allies with basic attacks and occasional abilities; move to take control"
					} else if errors.Is(err, chat.ErrEventUnavailable) {
						responseBody = "AI only works in multiplayer co-op or PvP"
					} else {
						return nil, fmt.Errorf("commandAI: %w", err)
					}
				}
			}
			if commandName == "/follow" {
				result, followErr := messenger.Follow(ctx, chat.FollowCommand{
					Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
					GameID: user.CurrentGameID(), TargetName: commandRemainder(body),
				})
				if followErr == nil && result.IsQueued {
					responseBody = fmt.Sprintf("Following %s; issue a movement command to stop", result.Target.Name)
				} else if followErr == nil {
					names := make([]string, 0, len(result.Candidates))
					for _, candidate := range result.Candidates {
						names = append(names, candidate.Name)
					}
					responseBody = fmt.Sprintf("Syntax: /follow <name> | allies: %s", strings.Join(names, ", "))
				} else if errors.Is(followErr, chat.ErrFollowUnavailable) {
					responseBody = "Follow only works in an active multiplayer game"
				} else {
					return nil, fmt.Errorf("commandFollow: %w", followErr)
				}
			}
			if commandName == "/effect" {
				responseBody = effectPreviewSyntax
				field := strings.Fields(body)
				isWorldMode := len(field) == 2 ||
					(len(field) == 3 && strings.EqualFold(field[2], "world"))
				if len(field) == 3 && strings.EqualFold(field[2], "attached") {
					responseBody = "Attached effect previews are unsupported: the build-103 removal and admission contract is not proven"
				} else if isWorldMode {
					definition, isValid := effectPreviewAsset(field[1])
					if isValid {
						previewErr := messenger.PreviewEffect(ctx, chat.EffectPreviewCommand{
							Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID: user.CurrentGameID(), Asset: definition.asset,
						})
						if previewErr == nil {
							responseBody = fmt.Sprintf(
								"Previewing Fang world effect %s (0x%08x, %s)",
								definition.name, definition.asset, definition.context,
							)
						} else if !errors.Is(previewErr, chat.ErrEffectPreviewUnavailable) {
							return nil, fmt.Errorf("commandEffect: %w", previewErr)
						}
					} else {
						responseBody = fmt.Sprintf(
							"Effect %q is unknown or has no supported presentation context", field[1],
						)
					}
				}
			}
			if commandName == "/summon" {
				responseBody = itemSummonSyntax
				field := strings.Fields(body)
				if len(field) == 5 {
					rigblockID, isRigblockValid := parseCommandUint(field[1], 16)
					primaryPrefix, isPrimaryValid := parseCommandUint(field[2], 16)
					secondaryPrefix, isSecondaryValid := parseCommandUint(field[3], 16)
					suffix, isSuffixValid := parseCommandUint(field[4], 16)
					if isRigblockValid && rigblockID != 0 &&
						isPrimaryValid && isSecondaryValid && isSuffixValid {
						summonErr := messenger.SummonItem(ctx, chat.ItemSummonCommand{
							Sender:        chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID:        user.CurrentGameID(),
							RigblockID:    uint16(rigblockID),
							PrimaryPrefix: uint16(primaryPrefix), SecondaryPrefix: uint16(secondaryPrefix),
							Suffix: uint16(suffix),
						})
						if summonErr == nil {
							responseBody = fmt.Sprintf("Summoned rigblock %d to inventory", rigblockID)
						} else if !errors.Is(summonErr, chat.ErrItemSummonUnavailable) {
							return nil, fmt.Errorf("commandSummon: %w", summonErr)
						}
					}
				}
			}
			if commandName == "/level" {
				responseBody = levelSyntax
				field := strings.Fields(body)
				if len(field) == 2 {
					level, isLevelValid := parseCommandUint(field[1], 32)
					if isLevelValid && level > 0 && level <= 100 {
						levelErr := messenger.SetLevel(ctx, chat.LevelCommand{
							Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID: user.CurrentGameID(), Level: uint32(level),
						})
						if levelErr == nil {
							responseBody = fmt.Sprintf("Account level set to %d", level)
						} else if !errors.Is(levelErr, chat.ErrLevelUnavailable) {
							return nil, fmt.Errorf("commandLevel: %w", levelErr)
						}
					}
				}
			}
			if commandName == "/warp" {
				responseBody = warpAreaCatalog
				field := strings.Fields(body)
				if len(field) == 2 {
					level := field[1]
					warpErr := messenger.RequestWarp(ctx, chat.WarpCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						Level:  level,
					})
					if warpErr == nil {
						responseBody = fmt.Sprintf(
							"Next campaign mission will warp to %s", level,
						)
					} else if !errors.Is(warpErr, chat.ErrWarpUnavailable) {
						return nil, fmt.Errorf("commandWarp: %w", warpErr)
					}
				}
			}
			if commandName == "/spawn" {
				responseBody = npcSpawnSyntax
				field := strings.Fields(body)
				if len(field) == 2 {
					nounName := field[1]
					spawnErr := messenger.SpawnNPC(ctx, chat.NPCSpawnCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), NounName: nounName,
					})
					if spawnErr == nil {
						responseBody = fmt.Sprintf("Spawn queued: %s", nounName)
					} else if errors.Is(spawnErr, chat.ErrNPCSpawnUnavailable) {
						responseBody = "Spawn only works inside a campaign mission entered with /warp"
					} else {
						return nil, fmt.Errorf("commandNPCSpawn: %w", spawnErr)
					}
				}
			}
			if commandName == "/drop" {
				responseBody = dropCommandHelp
				field := strings.Fields(body)
				category := ""
				isCreate := len(field) >= 2 && strings.EqualFold(field[1], "create")
				if len(field) == 3 {
					category = dropCategory(field[2])
				}
				isCategoryValid := len(field) == 2 || len(field) == 3 && category != ""
				if isCreate && isCategoryValid {
					dropErr := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "drop-create", Category: category,
					})
					if dropErr == nil {
						responseBody = "Campaign equipment drop debug queued"
						if category != "" {
							responseBody += " for " + dropCategoryDisplay(category)
						}
					} else if errors.Is(dropErr, chat.ErrEventUnavailable) {
						responseBody = "Drop creation only works during an active campaign mission"
					} else {
						return nil, fmt.Errorf("commandDropCreate: %w", dropErr)
					}
				}
			}
			if commandName == "/dna" {
				responseBody = dnaSyntax
				field := strings.Fields(body)
				if len(field) == 2 {
					amount, isAmountValid := parseCommandUint(field[1], 32)
					if isAmountValid && amount > 0 {
						dna, dnaErr := messenger.GrantDNA(ctx, chat.DNACommand{
							Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID: user.CurrentGameID(), Amount: uint32(amount),
						})
						if dnaErr == nil {
							responseBody = fmt.Sprintf("Granted %d DNA | total=%d", amount, dna)
						} else if errors.Is(dnaErr, chat.ErrDNAOverflow) {
							responseBody = "DNA grant rejected: account total would overflow"
						} else if !errors.Is(dnaErr, chat.ErrDNAUnavailable) {
							return nil, fmt.Errorf("commandDNA: %w", dnaErr)
						}
					}
				}
			}
			if commandName == "/damage" {
				responseBody = damageSyntax
				field := strings.Fields(body)
				if len(field) == 2 {
					damage, isDamageValid := parsePositiveCommandFloat(field[1])
					if isDamageValid {
						resourceErr := messenger.MutateResource(ctx, chat.ResourceCommand{
							Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID: user.CurrentGameID(), Damage: damage,
						})
						if resourceErr == nil {
							responseBody = fmt.Sprintf("Damaging current hero by %g", damage)
						} else if !errors.Is(resourceErr, chat.ErrResourceUnavailable) {
							return nil, fmt.Errorf("commandDamage: %w", resourceErr)
						}
					}
				}
			}
			if commandName == "/heal" {
				responseBody = healSyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					resourceErr := messenger.MutateResource(ctx, chat.ResourceCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), IsHeal: true,
					})
					if resourceErr == nil {
						responseBody = "Healing all living squad members to full health"
					} else if !errors.Is(resourceErr, chat.ErrResourceUnavailable) {
						return nil, fmt.Errorf("commandHeal: %w", resourceErr)
					}
				}
			}
			if commandName == "/power" || commandName == "/mana" {
				responseBody = powerSyntax
				field := strings.Fields(body)
				resourceCommand := chat.ResourceCommand{
					Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
					GameID: user.CurrentGameID(), IsPowerFill: true,
				}
				isPowerValid := len(field) == 1
				if len(field) == 2 {
					powerReduction, isReductionValid := parseNegativeCommandFloat(field[1])
					if isReductionValid {
						resourceCommand.IsPowerFill = false
						resourceCommand.PowerReduction = powerReduction
						isPowerValid = true
					}
				}
				if isPowerValid {
					resourceErr := messenger.MutateResource(ctx, resourceCommand)
					if resourceErr == nil && resourceCommand.IsPowerFill {
						responseBody = "Restoring current hero to full power"
					} else if resourceErr == nil {
						responseBody = fmt.Sprintf(
							"Reducing current hero power by %g", resourceCommand.PowerReduction,
						)
					} else if !errors.Is(resourceErr, chat.ErrResourceUnavailable) {
						return nil, fmt.Errorf("commandPower: %w", resourceErr)
					}
				}
			}
			if commandName == "/event" {
				responseBody = eventSyntax
				field := strings.Fields(body)
				if len(field) == 2 {
					eventName, isEventFound := developerEventName(field[1])
					if isEventFound {
						eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
							Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID: user.CurrentGameID(), Name: eventName,
						})
						if eventErr == nil {
							responseBody = fmt.Sprintf("Triggering event %s", eventName)
						} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
							return nil, fmt.Errorf("commandEvent: %w", eventErr)
						}
					}
				}
			}
			if commandName == "/goto" {
				responseBody = gotoSyntax
				field := strings.Fields(body)
				if len(field) == 4 {
					x, isXValid := parseCommandFloat(field[1])
					y, isYValid := parseCommandFloat(field[2])
					z, isZValid := parseCommandFloat(field[3])
					if isXValid && isYValid && isZValid {
						eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
							Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
							GameID: user.CurrentGameID(), Name: "goto", X: x, Y: y, Z: z,
						})
						if eventErr == nil {
							responseBody = fmt.Sprintf("Moving current hero to %g %g %g", x, y, z)
						} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
							return nil, fmt.Errorf("commandGoto: %w", eventErr)
						}
					}
				}
			}
			if commandName == "/kill" {
				responseBody = killSyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "kill",
					})
					if eventErr == nil {
						responseBody = "Defeating all live enemies"
					} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
						return nil, fmt.Errorf("commandKill: %w", eventErr)
					}
				}
			}
			if commandName == "/reset" {
				responseBody = resetSyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "reset",
					})
					if eventErr == nil {
						responseBody = "Resetting transient gameplay state"
					} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
						return nil, fmt.Errorf("commandReset: %w", eventErr)
					}
				}
			}
			if commandName == "/recap" {
				responseBody = recapSyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "recap",
					})
					if eventErr == nil {
						responseBody = "Resurrecting fallen heroes across the party"
					} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
						return nil, fmt.Errorf("commandRecap: %w", eventErr)
					}
				}
			}
			if commandName == "/victory" {
				responseBody = victorySyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "victory",
					})
					if eventErr == nil {
						responseBody = "Completing the active level with victory"
					} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
						return nil, fmt.Errorf("commandVictory: %w", eventErr)
					}
				}
			}
			if commandName == "/defeat" {
				responseBody = defeatSyntax
				field := strings.Fields(body)
				if len(field) == 1 {
					eventErr := messenger.TriggerEvent(ctx, chat.EventCommand{
						Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
						GameID: user.CurrentGameID(), Name: "defeat",
					})
					if eventErr == nil {
						responseBody = "Completing the campaign with defeat"
					} else if !errors.Is(eventErr, chat.ErrEventUnavailable) {
						return nil, fmt.Errorf("commandDefeat: %w", eventErr)
					}
				}
			}
			if commandName == "/exit" {
				responseBody = "Closing the game..."
			}
			err = queueMessagingSystemResponse(request, user, responseBody)
			if err != nil {
				return nil, fmt.Errorf("commandReply: %w", err)
			}
			messageGroup, messageIDs := messagingAttributeIDs(request.Fields)
			return &Response{Fields: []tdf.Field{
				tdf.FieldNamed("MGID", tdf.IntegerValue(messageGroup)),
				tdf.FieldNamed("MIDS", tdf.ListValue(tdf.Integer, messageIDs...)),
			}}, nil
		}
		message, err := messenger.Send(ctx, chat.SendCommand{
			Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
			Target: target,
			Body:   body,
		})
		if errors.Is(err, chat.ErrTargetNotFound) {
			return &Response{ErrorCode: messagingErrorTargetNotFound}, nil
		}
		if errors.Is(err, chat.ErrBodyMissing) {
			return &Response{ErrorCode: messagingErrorUnknown}, nil
		}
		if errors.Is(err, chat.ErrTargetType) || errors.Is(err, chat.ErrSenderNotMember) {
			return &Response{ErrorCode: messagingErrorTargetType}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("chatSend: %w", err)
		}

		notification := []tdf.Field{
			tdf.FieldNamed("FLAG", tdf.IntegerValue(0)),
			tdf.FieldNamed("MGID", tdf.IntegerValue(message.ID)),
			tdf.FieldNamed("NAME", tdf.StringValue(message.Sender.Name)),
			tdf.FieldNamed("PYLD", tdf.StructValue(cloneFields(request.Fields)...)),
			tdf.FieldNamed("SRCE", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
				uint64(UserSessionComponentID), userSessionObjectType, uint64(message.Sender.ID),
			}}),
			tdf.FieldNamed("TIME", tdf.IntegerValue(uint64(message.SentAt.Unix()))),
		}
		for index, recipientID := range message.RecipientIDs {
			err = request.QueueUserNotification(recipientID, MessagingComponentID, 0x01, notification)
			if err != nil {
				return nil, fmt.Errorf("chatNotify[%d]: %w", index, err)
			}
		}

		messageGroup, messageIDs := messagingAttributeIDs(request.Fields)
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("MGID", tdf.IntegerValue(messageGroup)),
			tdf.FieldNamed("MIDS", tdf.ListValue(tdf.Integer, messageIDs...)),
		}}, nil
	}
}

func forwardPartyInvite(
	ctx context.Context, request *Request, inviter *sporenet.User,
	target chat.Target, partyID uint32, partyService *party.Service,
) (*Response, error) {
	if target.Scope != chat.ScopeDirect || target.ID > math.MaxInt64 || partyService == nil {
		return &Response{ErrorCode: messagingErrorTargetType}, nil
	}
	inviteeID := int64(target.ID)
	if request.Session == nil || request.Session.server == nil ||
		len(request.Session.server.sessionsForUser(inviteeID)) == 0 {
		return &Response{ErrorCode: messagingErrorTargetNotFound}, nil
	}
	_, err := partyService.Invite(ctx, party.InviteRequest{
		PartyID: partyID,
		Inviter: party.Member{ID: inviter.Account.ID, Name: inviter.DisplayName, AvatarID: inviter.Account.AvatarID},
		Invitee: party.Member{ID: inviteeID},
	})
	if errors.Is(err, party.ErrAlreadyMember) {
		existing, isFound := partyService.PartyForMember(inviteeID)
		if isFound && existing.ID == partyID {
			messageGroup, messageIDs := messagingAttributeIDs(request.Fields)
			return &Response{Fields: []tdf.Field{
				tdf.FieldNamed("MGID", tdf.IntegerValue(messageGroup)),
				tdf.FieldNamed("MIDS", tdf.ListValue(tdf.Integer, messageIDs...)),
			}}, nil
		}
		return &Response{ErrorCode: messagingErrorTargetNotFound}, nil
	}
	if errors.Is(err, party.ErrPartyMissing) || errors.Is(err, party.ErrFull) ||
		errors.Is(err, party.ErrClosed) {
		return &Response{ErrorCode: messagingErrorTargetNotFound}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("partyInvite: %w", err)
	}
	messageID := nextSystemMessageID.Add(1)
	notification := []tdf.Field{
		tdf.FieldNamed("FLAG", tdf.IntegerValue(0)),
		tdf.FieldNamed("MGID", tdf.IntegerValue(messageID)),
		tdf.FieldNamed("NAME", tdf.StringValue(inviter.DisplayName)),
		tdf.FieldNamed("PYLD", tdf.StructValue(cloneFields(request.Fields)...)),
		tdf.FieldNamed("SRCE", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
			uint64(UserSessionComponentID), userSessionObjectType, uint64(inviter.Account.ID),
		}}),
		tdf.FieldNamed("TIME", tdf.IntegerValue(uint64(time.Now().Unix()))),
	}
	err = request.QueueUserNotification(inviteeID, MessagingComponentID, 0x01, notification)
	if err != nil {
		return nil, fmt.Errorf("partyInviteNotify: %w", err)
	}
	messageGroup, messageIDs := messagingAttributeIDs(request.Fields)
	return &Response{Fields: []tdf.Field{
		tdf.FieldNamed("MGID", tdf.IntegerValue(messageGroup)),
		tdf.FieldNamed("MIDS", tdf.ListValue(tdf.Integer, messageIDs...)),
	}}, nil
}

const effectPreviewSyntax = "Syntax: /effect <exact-authored-effect-name> [world]"

const itemSummonSyntax = "Syntax: /summon <rigid> <primary> <secondary> <suffix>"

const levelSyntax = "Syntax: /level <1-100>"

const warpAreaCatalog = "Warp areas: " +
	"1=Game_Tutorial_cryos_1, 2=CreatureEditor_EL, 3=Creature_Vid_Capture, " +
	"4=cryos_1_SM, 5=cryos_2_PVP, 6=front_end_ship, 7=infinity_1_PVP, " +
	"8=infinity_4_SM, 9=Juggernaut_Mode_Testing, 10=nocturna_2_PVP, " +
	"11=nocturna_3_SM, 12=scaldron_1_PVP, 13=scaldron_4_SM, 14=Spectra_1, " +
	"15=Spectra_2, 16=Spectra_3, 17=Spectra_4, 18=test_AI_arena, 19=test_AI_zoo, " +
	"20=test_AI_zoo_bio, 21=test_AI_zoo_chrono, 22=test_AI_zoo_cyber, " +
	"23=test_AI_zoo_elites, 24=test_AI_zoo_plasma, 25=test_AI_zoo_supernatural, " +
	"26=test_AI_zoo_ugc, 27=test_Creature_Vid_Capture, 28=test_holodeck, " +
	"29=test_survivor_arena, 30=test_VFX_arena, 31=TNX_173, 32=tnx173_2, " +
	"33=tnx173_3, 34=verdanth_3_PVP, 35=verdanth_4_SM, 36=zelems_1_SM, " +
	"37=zelems_2_PVP | Use /warp <number|name>"

const npcSpawnSyntax = "Syntax: /spawn <Fang combat noun or unique partial match>"

const dropCommandHelp = "Drop commands: /drop create [weapon|hand|foot|offense|defense|utility]"

const locSyntax = "Syntax: /loc"

func formatResourceStatus(
	status chat.ResourceStatusResult, req chat.ResourceStatusRequest,
) string {
	server := fmt.Sprintf(
		"Stats object=%d | server HP=%.3f/%.3f Power=%.3f/%.3f",
		status.ObjectID, status.HitPoint, status.MaximumHitPoint,
		status.PowerPoint, status.MaximumPowerPoint,
	)
	if req.ClientObjectID == 0 ||
		(!req.IsClientHitPointSet && !req.IsClientPowerPointSet) {
		return server + " | client-received=unavailable"
	}
	if req.ClientObjectID != status.ObjectID {
		return fmt.Sprintf(
			"%s | client-received=stale object=%d", server, req.ClientObjectID,
		)
	}
	client := " | client-received"
	if req.IsClientHitPointSet {
		client += fmt.Sprintf(
			" HP=%.3f delta=%+.3f", req.ClientHitPoint,
			req.ClientHitPoint-status.HitPoint,
		)
	} else {
		client += " HP=unknown"
	}
	if req.IsClientPowerPointSet {
		client += fmt.Sprintf(
			" Power=%.3f delta=%+.3f", req.ClientPowerPoint,
			req.ClientPowerPoint-status.PowerPoint,
		)
	} else {
		client += " Power=unknown"
	}
	return server + client
}

const dnaSyntax = "Syntax: /dna <positive amount>"

const bugReportSyntax = "Syntax: /bug <what happened> | alias: /b"

const snapshotSyntax = "Sync Snapshot: /ss mode [manual|auto|off] | /ss dump | /ss delay <seconds> | /ss ignore <snapshot-id>"

const damageSyntax = "Syntax: /damage <positive amount>"

const healSyntax = "Syntax: /heal"

const powerSyntax = "Syntax: /power [negative amount] | alias: /mana"

const eventSyntax = "Events: 1 security-next @ route starts (-141.3413 83.7317 0.0340), " +
	"(556.1375 -38.2726 0.0175), (601.7911 42.2988 33.0519), " +
	"(-543.5760 546.1642 0.1581), (-625.0099 655.0704 0.1358) | " +
	"2 boss-start @ (948.3736 674.0089 0.0880) | 3 boss-complete @ same | " +
	"Syntax: /event <number-or-keyword>"

const gotoSyntax = "Syntax: /goto <x> <y> <z>"

const killSyntax = "Syntax: /kill"

const resetSyntax = "Syntax: /reset"

const recapSyntax = "Syntax: /recap"

const victorySyntax = "Syntax: /victory"

const defeatSyntax = "Syntax: /defeat"

const darkspinChatHelp = "Darkspin: /help | /taunt (availability info) | " + snapshotSyntax + " | /bug <what happened> | /b <what happened> | /ping | /hint | /loc | /stat | /follow [ally name] | /ai | /effect <exact-authored-name> [world] | " +
	"/summon <rigid> <primary> <secondary> <suffix> | /level <1-100> | /warp [area] | /spawn <noun> | /drop create [weapon|hand|foot|offense|defense|utility] | /dna <amount> | " +
	"/damage <amount> | /heal | /power [negative amount] | /mana [negative amount] | /event [1|security-next|2|boss-start|3|boss-complete] | /goto <x> <y> <z> | /kill | /reset | /recap | /victory | /defeat | /exit | " +
	"Built-in: /tell <player> <message> | /party <message> | /game <message> | /lobby <message> | " +
	"/invite <player> | /leave | /friend <player> | /unfriend <player> | /block <player> | " +
	"/unblock <player> | /reply <message> | /setprofanityfilter 0|1 | /dance"

func messagingCommandName(body string) (string, bool) {
	field := strings.Fields(body)
	if len(field) == 0 || len(field[0]) < 2 {
		return "", false
	}
	prefix := field[0][0]
	if prefix != '\x1f' && prefix != '!' && prefix != '/' {
		return "", false
	}
	commandName := field[0][1:]
	if commandName == "" {
		return "", false
	}
	return "/" + strings.ToLower(commandName), true
}

func dropCategory(category string) string {
	switch strings.ToLower(category) {
	case "weapon", "foot", "offense", "defense", "utility":
		return strings.ToLower(category)
	case "hand":
		return "grasper"
	default:
		return ""
	}
}

func dropCategoryDisplay(category string) string {
	if category == "grasper" {
		return "hand"
	}
	return category
}

func commandRemainder(body string) string {
	body = strings.TrimSpace(body)
	separator := strings.IndexAny(body, " \t\r\n")
	if separator < 0 {
		return ""
	}
	return strings.TrimSpace(body[separator:])
}

func developerEventName(alias string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(alias)) {
	case "1", "security-next":
		return "security-next", true
	case "2", "boss-start":
		return "boss-start", true
	case "3", "boss-complete":
		return "boss-complete", true
	default:
		return "", false
	}
}

type effectPreviewDefinition struct {
	name    string
	context string
	asset   uint32
}

var effectPreviewDefinitionByName = func() map[string]effectPreviewDefinition {
	definition := []effectPreviewDefinition{
		{name: "character_beam_in_plasma_electric", context: "beam-in"},
		{name: "character_beam_out_plasma_electric", context: "beam-out"},
		{name: "character_beam_in_bio", context: "beam-in"},
		{name: "character_beam_out_bio", context: "beam-out"},
		{name: "character_teleport_beam_out", context: "beam-out"},
	}
	byName := make(map[string]effectPreviewDefinition, len(definition))
	for _, candidate := range definition {
		candidate.asset = util.HashID(candidate.name + ".ServerEventDef")
		byName[strings.ToLower(candidate.name)] = candidate
	}
	return byName
}()

func effectPreviewAsset(argument string) (effectPreviewDefinition, bool) {
	name := strings.ToLower(strings.TrimSpace(argument))
	definition, isFound := effectPreviewDefinitionByName[name]
	return definition, isFound
}

func parseCommandUint(argument string, bitSize int) (uint64, bool) {
	number, err := strconv.ParseUint(argument, 0, bitSize)
	if err != nil {
		return 0, false
	}
	return number, true
}

func parsePositiveCommandFloat(argument string) (float32, bool) {
	number, err := strconv.ParseFloat(argument, 32)
	if err != nil || number <= 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return float32(number), true
}

func parseCommandFloat(argument string) (float32, bool) {
	number, err := strconv.ParseFloat(argument, 32)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return float32(number), true
}

func parseNegativeCommandFloat(argument string) (float32, bool) {
	number, err := strconv.ParseFloat(argument, 32)
	if err != nil || number >= 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return float32(-number), true
}

func queueMessagingSystemResponse(request *Request, user *sporenet.User, body string) error {
	payload := messagingPayloadWithBody(request.Fields, body)
	notification := []tdf.Field{
		tdf.FieldNamed("FLAG", tdf.IntegerValue(0)),
		tdf.FieldNamed("MGID", tdf.IntegerValue(nextSystemMessageID.Add(1))),
		tdf.FieldNamed("NAME", tdf.StringValue("Darkspin")),
		tdf.FieldNamed("PYLD", tdf.StructValue(payload...)),
		tdf.FieldNamed("SRCE", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
			uint64(UserSessionComponentID), userSessionObjectType, uint64(user.Account.ID),
		}}),
		tdf.FieldNamed("TIME", tdf.IntegerValue(uint64(time.Now().Unix()))),
	}
	return request.QueueUserNotification(user.Account.ID, MessagingComponentID, 0x01, notification)
}

// NotifySystemChat sends one unsolicited Darkspin chat line to every active
// Blaze session owned by a user.
func (s *Server) NotifySystemChat(
	userID int64, gameID uint32, body string,
) (int, error) {
	if s == nil || userID == 0 || strings.TrimSpace(body) == "" {
		return 0, errors.New("system chat notification invalid")
	}
	targetComponentID := uint64(UserSessionComponentID)
	targetObjectType := userSessionObjectType
	targetObjectID := uint64(userID)
	if gameID != 0 {
		targetComponentID = uint64(GameManagerComponentID)
		targetObjectType = gameManagerObjectType
		targetObjectID = uint64(gameID)
	}
	payload := []tdf.Field{
		tdf.FieldNamed("ATTR", tdf.MapValue(
			tdf.Integer, tdf.String,
			tdf.MapEntry{
				Key: tdf.IntegerValue(messagingBodyAttribute), Value: tdf.StringValue(body),
			},
		)),
		tdf.FieldNamed("TARG", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
			targetComponentID, targetObjectType, targetObjectID,
		}}),
		tdf.FieldNamed("TYPE", tdf.IntegerValue(messagingChatType)),
	}
	notification := []tdf.Field{
		tdf.FieldNamed("FLAG", tdf.IntegerValue(0)),
		tdf.FieldNamed("MGID", tdf.IntegerValue(nextSystemMessageID.Add(1))),
		tdf.FieldNamed("NAME", tdf.StringValue("Darkspin")),
		tdf.FieldNamed("PYLD", tdf.StructValue(payload...)),
		tdf.FieldNamed("SRCE", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
			uint64(UserSessionComponentID), userSessionObjectType, uint64(userID),
		}}),
		tdf.FieldNamed("TIME", tdf.IntegerValue(uint64(time.Now().Unix()))),
	}
	sessions := s.sessionsForUser(userID)
	var notifyErrors []error
	deliveryCount := 0
	for index, session := range sessions {
		err := session.Notify(MessagingComponentID, 0x01, notification)
		if err != nil {
			notifyErrors = append(notifyErrors, fmt.Errorf("systemChat[%d]: %w", index, err))
			continue
		}
		deliveryCount++
	}
	return deliveryCount, errors.Join(notifyErrors...)
}

func messagingPayloadWithBody(fields []tdf.Field, body string) []tdf.Field {
	payload := cloneFields(fields)
	for fieldIndex := range payload {
		if payload[fieldIndex].Label != "ATTR" || payload[fieldIndex].Value.Type != tdf.Map {
			continue
		}
		attributes := payload[fieldIndex].Value
		attributes.Entries = append([]tdf.MapEntry(nil), attributes.Entries...)
		for entryIndex := range attributes.Entries {
			entry := &attributes.Entries[entryIndex]
			if entry.Key.Type == tdf.Integer && entry.Key.Integer == messagingBodyAttribute {
				entry.Value = tdf.StringValue(body)
				payload[fieldIndex].Value = attributes
				return payload
			}
		}
		attributes.Entries = append(attributes.Entries, tdf.MapEntry{
			Key: tdf.IntegerValue(messagingBodyAttribute), Value: tdf.StringValue(body),
		})
		payload[fieldIndex].Value = attributes
		return payload
	}
	return payload
}

func messagingAttributeIDs(fields []tdf.Field) (uint64, []tdf.Value) {
	attributes, isFound := tdf.Find(fields, "ATTR")
	messageIDs := make([]tdf.Value, 0)
	var messageGroup uint64
	if isFound && attributes.Type == tdf.Map {
		for index, entry := range attributes.Entries {
			if index == 0 {
				messageGroup = entry.Key.Integer
			}
			messageIDs = append(messageIDs, tdf.IntegerValue(entry.Key.Integer))
		}
	}
	return messageGroup, messageIDs
}

func messagingBody(fields []tdf.Field) (string, bool) {
	attributes, isFound := tdf.Find(fields, "ATTR")
	if !isFound || attributes.Type != tdf.Map {
		return "", false
	}
	for _, entry := range attributes.Entries {
		if entry.Key.Type == tdf.Integer && entry.Key.Integer == messagingBodyAttribute && entry.Value.Type == tdf.String {
			return entry.Value.String, true
		}
	}
	return "", false
}

func messagingPartyInvite(fields []tdf.Field) (uint32, bool) {
	attributes, isFound := tdf.Find(fields, "ATTR")
	if !isFound || attributes.Type != tdf.Map {
		return 0, false
	}
	for _, entry := range attributes.Entries {
		if entry.Key.Type != tdf.Integer || entry.Key.Integer != 720897 || entry.Value.Type != tdf.String {
			continue
		}
		partyID, err := strconv.ParseUint(entry.Value.String, 10, 32)
		if err != nil || partyID == 0 {
			return 0, false
		}
		return uint32(partyID), true
	}
	return 0, false
}

func messagingTarget(fields []tdf.Field) (chat.Target, error) {
	value, isFound := tdf.Find(fields, "TARG")
	if !isFound || value.Type != tdf.ObjectID || value.ObjectID[2] == 0 {
		return chat.Target{}, chat.ErrTargetType
	}
	scope := chat.Scope(0)
	switch uint16(value.ObjectID[0]) {
	case UserSessionComponentID:
		scope = chat.ScopeDirect
	case PlaygroupsComponentID:
		scope = chat.ScopeParty
	case GameManagerComponentID:
		// Build 103 labels this selected channel General but supplies the active
		// GameManager object. Until shared lobby joining is available, expose it
		// as the requested server-wide development channel.
		scope = chat.ScopeGlobal
	case RoomsComponentID:
		scope = chat.ScopeLobby
	default:
		return chat.Target{}, chat.ErrTargetType
	}
	return chat.Target{Scope: scope, ID: value.ObjectID[2]}, nil
}

func cloneFields(fields []tdf.Field) []tdf.Field {
	return append([]tdf.Field(nil), fields...)
}

func playgroupsComponent(
	partyService *party.Service, userManager socialUserManager,
	presenceRegistry *PresenceRegistry, relay playgroupRelay,
	matchmaking matchmakingDirectory,
) Component {
	networkRegistry := newPlaygroupNetworkRegistry(relay)
	commands := make(map[uint16]Handler)
	for command := uint16(1); command <= 0x0b; command++ {
		commands[command] = func(context.Context, *Request) (*Response, error) { return &Response{}, nil }
	}
	commands[0x01] = playgroupCreateHandler(partyService, userManager, networkRegistry)
	commands[0x02] = playgroupDestroyHandler(partyService, networkRegistry)
	commands[0x03] = playgroupJoinHandler(
		partyService, userManager, presenceRegistry, networkRegistry,
	)
	commands[0x04] = playgroupLeaveHandler(
		partyService, networkRegistry, matchmaking,
	)
	commands[0x0a] = playgroupLookupHandler(partyService, networkRegistry)
	return Component{ID: PlaygroupsComponentID, Name: "Playgroups", Commands: commands}
}

type playgroupNetworkRegistry struct {
	mu                sync.RWMutex
	relay             playgroupRelay
	hostsByParty      map[uint32]tdf.Value
	bindingsByPartyID map[uint32]map[int64]tdf.Value
	networksByPartyID map[uint32]map[int64]tdf.Value
}

func newPlaygroupNetworkRegistry(relay playgroupRelay) *playgroupNetworkRegistry {
	return &playgroupNetworkRegistry{
		relay: relay, hostsByParty: make(map[uint32]tdf.Value),
		bindingsByPartyID: make(map[uint32]map[int64]tdf.Value),
		networksByPartyID: make(map[uint32]map[int64]tdf.Value),
	}
}

func (e *playgroupNetworkRegistry) recordHost(
	partyID uint32, userID int64, playgroup, publishedNetwork tdf.Value,
) {
	host, isFound := tdf.Find(playgroup.Fields, "HNET")
	if !isFound || host.Type != tdf.Union || host.ActiveMember == 0x7f {
		if publishedNetwork.Type != tdf.Union || publishedNetwork.ActiveMember == 0x7f {
			return
		}
		host = publishedNetwork
	}
	binding := host
	if e.relay != nil {
		e.relay.RegisterMember(partyID, userID, playgroupNetworkEndpoints(host))
		host = e.relayNetwork()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hostsByParty[partyID] = host
	e.bindingsByPartyID[partyID] = map[int64]tdf.Value{userID: binding}
	e.networksByPartyID[partyID] = map[int64]tdf.Value{userID: host}
}

func (e *playgroupNetworkRegistry) recordMember(
	partyID uint32, userID int64, fields []tdf.Field, publishedNetwork tdf.Value,
) {
	network, isFound := tdf.Find(fields, "PNET")
	if !isFound || network.Type != tdf.Union || network.ActiveMember == 0x7f {
		if publishedNetwork.Type != tdf.Union || publishedNetwork.ActiveMember == 0x7f {
			return
		}
		network = publishedNetwork
	}
	binding := network
	if e.relay != nil {
		e.relay.RegisterMember(partyID, userID, playgroupNetworkEndpoints(network))
		network = e.relayNetwork()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	networksByUser := e.networksByPartyID[partyID]
	if networksByUser == nil {
		networksByUser = make(map[int64]tdf.Value)
		e.networksByPartyID[partyID] = networksByUser
	}
	bindingsByUser := e.bindingsByPartyID[partyID]
	if bindingsByUser == nil {
		bindingsByUser = make(map[int64]tdf.Value)
		e.bindingsByPartyID[partyID] = bindingsByUser
	}
	bindingsByUser[userID] = binding
	networksByUser[userID] = network
}

func (e *playgroupNetworkRegistry) removeMember(partyID uint32, userID int64) {
	if e.relay != nil {
		e.relay.RemoveMember(partyID, userID)
	}
	e.mu.Lock()
	delete(e.networksByPartyID[partyID], userID)
	delete(e.bindingsByPartyID[partyID], userID)
	e.mu.Unlock()
}

func (e *playgroupNetworkRegistry) removeParty(partyID uint32) {
	if e.relay != nil {
		e.relay.RemoveParty(partyID)
	}
	e.mu.Lock()
	delete(e.hostsByParty, partyID)
	delete(e.bindingsByPartyID, partyID)
	delete(e.networksByPartyID, partyID)
	e.mu.Unlock()
}

func (e *playgroupNetworkRegistry) relayNetwork() tdf.Value {
	endpoint, isFound := e.relay.Endpoint()
	if !isFound || !endpoint.Addr().Is4() {
		return tdf.UnionValue(0x7f)
	}
	ipBytes := endpoint.Addr().As4()
	ip := uint32(ipBytes[0])<<24 | uint32(ipBytes[1])<<16 |
		uint32(ipBytes[2])<<8 | uint32(ipBytes[3])
	address := tdf.StructValue(
		tdf.FieldNamed("IP", tdf.IntegerValue(uint64(ip))),
		tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(endpoint.Port()))),
	)
	return tdf.UnionValue(2, tdf.FieldNamed("VALU", tdf.StructValue(
		tdf.FieldNamed("EXIP", address),
		tdf.FieldNamed("INIP", address),
	)))
}

func playgroupNetworkEndpoints(network tdf.Value) []netip.AddrPort {
	endpoints := make([]netip.AddrPort, 0, 2)
	collectPlaygroupNetworkEndpoints(network, &endpoints)
	return endpoints
}

func collectPlaygroupNetworkEndpoints(network tdf.Value, endpoints *[]netip.AddrPort) {
	if network.Type == tdf.Struct || network.Type == tdf.Union {
		ip := uint32(fieldInteger(network.Fields, "IP"))
		port := uint16(fieldInteger(network.Fields, "PORT"))
		if ip != 0 && port != 0 {
			address := netip.AddrFrom4([4]byte{
				byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip),
			})
			*endpoints = append(*endpoints, netip.AddrPortFrom(address, port))
		}
		for _, field := range network.Fields {
			collectPlaygroupNetworkEndpoints(field.Value, endpoints)
		}
	}
	if network.Type == tdf.List {
		for _, item := range network.Items {
			collectPlaygroupNetworkEndpoints(item, endpoints)
		}
	}
}

func (e *playgroupNetworkRegistry) host(partyID uint32) (tdf.Value, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	host, isFound := e.hostsByParty[partyID]
	return host, isFound
}

func (e *playgroupNetworkRegistry) member(partyID uint32, userID int64) (tdf.Value, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	network, isFound := e.networksByPartyID[partyID][userID]
	return network, isFound
}

func (e *playgroupNetworkRegistry) memberForRecipient(
	partyID uint32, userID, recipientID int64,
) (tdf.Value, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if userID == recipientID {
		binding, isFound := e.bindingsByPartyID[partyID][userID]
		return binding, isFound
	}
	network, isFound := e.networksByPartyID[partyID][userID]
	return network, isFound
}

func (e *playgroupNetworkRegistry) hostForRecipient(
	partyID uint32, hostID, recipientID int64,
) (tdf.Value, bool) {
	if hostID == recipientID {
		return e.memberForRecipient(partyID, hostID, recipientID)
	}
	return e.host(partyID)
}

func playgroupCreateHandler(
	partyService *party.Service, userManager socialUserManager, networkRegistry *playgroupNetworkRegistry,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		playgroup := playgroupCreateInfo(request.Fields)
		before, isExisting := partyService.PartyForMember(user.Account.ID)
		partySnapshot, err := partyService.Create(ctx, party.CreateRequest{
			Actor: party.Member{ID: user.Account.ID, Name: user.DisplayName, AvatarID: user.Account.AvatarID},
			Name:  fieldString(playgroup.Fields, "NAME"), JoinControl: uint8(fieldInteger(request.Fields, "JOIN")),
		})
		if err != nil {
			return nil, fmt.Errorf("playgroupCreate: %w", err)
		}
		attributes := playgroupCreateAttributes(playgroup, userManager)
		isExisting = isExisting && before.ID == partySnapshot.ID
		if !isExisting {
			publishedNetwork, isPublishedNetworkFound := requestNetwork(request)
			if !isPublishedNetworkFound {
				publishedNetwork = tdf.Value{}
			}
			networkRegistry.recordHost(
				partySnapshot.ID, user.Account.ID, playgroup, publishedNetwork,
			)
		}
		isGameHandoff := isExisting && fieldString(playgroup.Fields, "UKEY") != ""
		rosterFields := playgroupJoinFields(
			partySnapshot, user.Account.ID, networkRegistry, attributes,
		)
		if !isExisting {
			err = request.QueueNotificationBeforeReply(
				PlaygroupsComponentID, 0x33, rosterFields,
			)
			if err != nil {
				return nil, fmt.Errorf("playgroupCreateRoster: %w", err)
			}
		} else if isGameHandoff {
			err = request.QueueUserNotification(
				user.Account.ID, PlaygroupsComponentID, 0x33,
				rosterFields,
			)
			if err != nil {
				return nil, fmt.Errorf("playgroupCreateRoster: %w", err)
			}
		}
		logPlaygroupCreate(
			request, playgroup, attributes, partySnapshot.ID, networkRegistry,
		)
		if !isExisting && fieldString(playgroup.Fields, "UKEY") == "" && len(attributes.Entries) == 0 {
			err = recoverSinglePeerPartyInvite(
				ctx, request, user, partySnapshot, userManager, partyService,
			)
			if err != nil {
				return nil, fmt.Errorf("playgroupInviteRecovery: %w", err)
			}
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("INFO", playgroupInfoValueWithNetwork(
				partySnapshot, networkRegistry, user.Account.ID, attributes,
			)),
		}}, nil
	}
}

func recoverSinglePeerPartyInvite(
	ctx context.Context, request *Request, inviter *sporenet.User, partySnapshot party.Snapshot,
	userManager socialUserManager, partyService *party.Service,
) error {
	if request == nil || request.Session == nil || request.Session.server == nil {
		return nil
	}
	inviteeID, isUnique := request.Session.server.uniqueOtherUserID(inviter.Account.ID)
	if !isUnique {
		return nil
	}
	invitee := userManager.UserByID(inviteeID)
	if invitee == nil {
		return nil
	}
	_, err := partyService.Invite(ctx, party.InviteRequest{
		PartyID: partySnapshot.ID,
		Inviter: party.Member{
			ID: inviter.Account.ID, Name: inviter.DisplayName, AvatarID: inviter.Account.AvatarID,
		},
		Invitee: party.Member{
			ID: invitee.Account.ID, Name: invitee.DisplayName, AvatarID: invitee.Account.AvatarID,
		},
	})
	if errors.Is(err, party.ErrAlreadyMember) || errors.Is(err, party.ErrFull) ||
		errors.Is(err, party.ErrClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inviteRecord: %w", err)
	}
	payload := []tdf.Field{
		tdf.FieldNamed("ATTR", tdf.MapValue(tdf.Integer, tdf.String,
			tdf.MapEntry{Key: tdf.IntegerValue(65282), Value: tdf.StringValue(invitee.DisplayName)},
			tdf.MapEntry{
				Key:   tdf.IntegerValue(720897),
				Value: tdf.StringValue(strconv.FormatUint(uint64(partySnapshot.ID), 10)),
			},
		)),
		tdf.FieldNamed("FLAG", tdf.IntegerValue(4)),
		tdf.FieldNamed("TARG", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
			uint64(UserSessionComponentID), userSessionObjectType, uint64(invitee.Account.ID),
		}}),
		tdf.FieldNamed("TYPE", tdf.IntegerValue(messagingPartyInviteType)),
	}
	notification := []tdf.Field{
		tdf.FieldNamed("FLAG", tdf.IntegerValue(0)),
		tdf.FieldNamed("MGID", tdf.IntegerValue(nextSystemMessageID.Add(1))),
		tdf.FieldNamed("NAME", tdf.StringValue(inviter.DisplayName)),
		tdf.FieldNamed("PYLD", tdf.StructValue(payload...)),
		tdf.FieldNamed("SRCE", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{
			uint64(UserSessionComponentID), userSessionObjectType, uint64(inviter.Account.ID),
		}}),
		tdf.FieldNamed("TIME", tdf.IntegerValue(uint64(time.Now().Unix()))),
	}
	err = request.QueueUserNotification(invitee.Account.ID, MessagingComponentID, 0x01, notification)
	if err != nil {
		return fmt.Errorf("inviteNotify: %w", err)
	}
	if request.Session.server.logger != nil {
		request.Session.server.logger.Printf(
			"playgroup_invite_recovered inviter=%q invitee=%q party_id=%d reason=%q",
			inviter.DisplayName, invitee.DisplayName, partySnapshot.ID, "single_online_peer",
		)
	}
	return nil
}

func playgroupCreateInfo(fields []tdf.Field) tdf.Value {
	playgroup, isFound := tdf.Find(fields, "PGRP")
	if !isFound || playgroup.Type != tdf.Struct {
		return tdf.StructValue()
	}
	return playgroup
}

func playgroupCreateAttributes(playgroup tdf.Value, userManager socialUserManager) tdf.Value {
	attributes, isFound := tdf.Find(playgroup.Fields, "ATTR")
	if !isFound || attributes.Type != tdf.Map || attributes.KeyType != tdf.String ||
		attributes.ValueType != tdf.String {
		attributes, isFound = playgroupStringMap(playgroup)
		if !isFound {
			attributes = tdf.MapValue(tdf.String, tdf.String)
		}
	}
	entries := append([]tdf.MapEntry(nil), attributes.Entries...)
	userKey := fieldString(playgroup.Fields, "UKEY")
	if len(entries) == 0 && userKey != "" {
		entries = append(entries, tdf.MapEntry{
			Key: tdf.StringValue("PlaygroupKey"), Value: tdf.StringValue(userKey),
		})
	}
	for index := range entries {
		entry := &entries[index]
		if entry.Key.Type != tdf.String || entry.Value.Type != tdf.String ||
			!strings.EqualFold(entry.Key.String, "PlaygroupKey") {
			continue
		}
		target := userManager.UserByDisplayName(entry.Value.String)
		if target == nil {
			target = userManager.UserByLoginName(entry.Value.String)
		}
		if target != nil {
			entry.Value = tdf.StringValue(target.DisplayName)
		}
	}
	return tdf.MapValue(tdf.String, tdf.String, entries...)
}

func playgroupStringMap(playgroup tdf.Value) (tdf.Value, bool) {
	for _, field := range playgroup.Fields {
		if field.Value.Type == tdf.Map && field.Value.KeyType == tdf.String &&
			field.Value.ValueType == tdf.String && len(field.Value.Entries) != 0 {
			return field.Value, true
		}
	}
	return tdf.Value{}, false
}

func logPlaygroupCreate(
	request *Request, playgroup, attributes tdf.Value, partyID uint32,
	networkRegistry *playgroupNetworkRegistry,
) {
	if request == nil || request.Session == nil || request.Session.server == nil ||
		request.Session.server.logger == nil {
		return
	}
	attributeNames := make([]string, 0, len(attributes.Entries))
	for _, entry := range attributes.Entries {
		if entry.Key.Type == tdf.String {
			attributeNames = append(attributeNames, entry.Key.String)
		}
	}
	hostNetworkMember := uint8(0x7f)
	hostNetwork, isFound := tdf.Find(playgroup.Fields, "HNET")
	if isFound && hostNetwork.Type == tdf.Union {
		hostNetworkMember = hostNetwork.ActiveMember
	}
	projectedHostNetworkMember := uint8(0x7f)
	projectedHostNetwork, isFound := networkRegistry.host(partyID)
	if isFound && projectedHostNetwork.Type == tdf.Union {
		projectedHostNetworkMember = projectedHostNetwork.ActiveMember
	}
	request.Session.server.logger.Printf(
		"playgroup_create_request fields=%q attributes=%q user_key_empty=%t request_topology=%d projected_topology=%d request_host_network_member=%d projected_host_network_member=%d",
		traceFieldShapes(playgroup.Fields), attributeNames, fieldString(playgroup.Fields, "UKEY") == "",
		fieldInteger(playgroup.Fields, "NTOP"), playgroupNetworkTopology,
		hostNetworkMember, projectedHostNetworkMember,
	)
}

func playgroupJoinHandler(
	partyService *party.Service, userManager socialUserManager,
	presenceRegistry *PresenceRegistry, networkRegistry *playgroupNetworkRegistry,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		partyID := uint32(fieldInteger(request.Fields, "PGID"))
		before, isBeforeFound := partyService.Lookup(partyID)
		if !isBeforeFound {
			return &Response{ErrorCode: 0x0006}, nil
		}
		partySnapshot, err := partyService.Join(ctx, party.JoinRequest{
			PartyID: partyID,
			Actor: party.Member{
				ID: user.Account.ID, Name: user.DisplayName, AvatarID: user.Account.AvatarID,
			},
		})
		if errors.Is(err, party.ErrPartyMissing) || errors.Is(err, party.ErrInviteMissing) {
			return &Response{ErrorCode: 0x0006}, nil
		}
		if errors.Is(err, party.ErrFull) || errors.Is(err, party.ErrClosed) || errors.Is(err, party.ErrAlreadyMember) {
			return &Response{ErrorCode: 0x0006}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("playgroupJoin: %w", err)
		}
		member := partySnapshot.Members[len(partySnapshot.Members)-1]
		publishedNetwork, isPublishedNetworkFound := requestNetwork(request)
		if !isPublishedNetworkFound {
			publishedNetwork = tdf.Value{}
		}
		networkRegistry.recordMember(partyID, member.ID, request.Fields, publishedNetwork)
		memberNetwork, isMemberNetworkFound := networkRegistry.member(partyID, member.ID)
		if !isMemberNetworkFound {
			memberNetwork = tdf.Value{}
		}
		memberValue, networkMember, externalID := playgroupJoiningMemberValue(
			member, request.Fields, memberNetwork,
		)
		if request.Session != nil && request.Session.server != nil && request.Session.server.logger != nil {
			request.Session.server.logger.Printf(
				"playgroup_join_projection party_id=%d member_id=%d slot_id=%d network_member=%d external_id=%d recipients=%d",
				partyID, member.ID, playgroupMemberSlotID(member), networkMember, externalID, len(before.Members),
			)
		}
		for index, existing := range before.Members {
			existingUser := userManager.UserByID(existing.ID)
			if existingUser != nil {
				err = queueUserVisibility(
					request, existing.ID, user, presenceRegistry,
					userExternalID(user.Account.AvatarID),
				)
				if err != nil {
					return nil, fmt.Errorf("playgroupJoinVisibility[%d]: %w", index, err)
				}
				err = queueUserVisibility(
					request, user.Account.ID, existingUser, presenceRegistry,
					userExternalID(existingUser.Account.AvatarID),
				)
				if err != nil {
					return nil, fmt.Errorf("playgroupExistingVisibility[%d]: %w", index, err)
				}
			}
			err = request.QueueUserNotification(
				existing.ID, PlaygroupsComponentID, 0x33,
				playgroupJoinFields(partySnapshot, existing.ID, networkRegistry),
			)
			if err != nil {
				return nil, fmt.Errorf("playgroupRosterNotify[%d]: %w", existing.JoinOrdinal, err)
			}
			err = request.QueueUserNotification(existing.ID, PlaygroupsComponentID, 0x34, []tdf.Field{
				tdf.FieldNamed("MEMB", memberValue),
				tdf.FieldNamed("PGID", tdf.IntegerValue(uint64(partyID))),
			})
			if err != nil {
				return nil, fmt.Errorf("playgroupMemberNotify[%d]: %w", existing.JoinOrdinal, err)
			}
		}
		err = request.QueueUserNotification(
			user.Account.ID, PlaygroupsComponentID, 0x33,
			playgroupJoinFields(partySnapshot, user.Account.ID, networkRegistry),
		)
		if err != nil {
			return nil, fmt.Errorf("playgroupJoinNotify: %w", err)
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("INFO", playgroupInfoValueWithNetwork(
				partySnapshot, networkRegistry, user.Account.ID,
			)),
		}}, nil
	}
}

func playgroupLeaveHandler(
	partyService *party.Service, networkRegistry *playgroupNetworkRegistry,
	matchmaking matchmakingDirectory,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		partyID := uint32(fieldInteger(request.Fields, "PGID"))
		before, isBeforeFound := partyService.Lookup(partyID)
		if !isBeforeFound {
			return &Response{ErrorCode: 0x0006}, nil
		}
		isGameHandoff := user.CurrentGameID() != 0 &&
			user.CurrentPlaygroupID() == partyID
		isTeamMatchmakingHandoff := user.CurrentPlaygroupID() == partyID &&
			isPlaygroupMatchmaking(before, user.Account.ID, matchmaking)
		if isGameHandoff || isTeamMatchmakingHandoff {
			if request.Session != nil {
				request.Session.pendingPlaygroupLeave = &playgroupDisconnectLeave{
					partyService: partyService, networkRegistry: networkRegistry,
					partyID: partyID, userID: user.Account.ID,
				}
			}
			reason := "indirect_game_handoff"
			if isTeamMatchmakingHandoff && !isGameHandoff {
				reason = "team_matchmaking_handoff"
			}
			if request.Session != nil && request.Session.server != nil &&
				request.Session.server.logger != nil {
				request.Session.server.logger.Printf(
					"playgroup_leave_preserved account=%q party_id=%d game_id=%d reason=%q",
					user.LoginName, partyID, user.CurrentGameID(), reason,
				)
			}
			return &Response{}, nil
		}
		return completePlaygroupLeave(
			ctx, request, partyService, networkRegistry, before, user.Account.ID,
		)
	}
}

func completePlaygroupLeave(
	ctx context.Context, request *Request, partyService *party.Service,
	networkRegistry *playgroupNetworkRegistry, before party.Snapshot, userID int64,
) (*Response, error) {
	partyID := before.ID
	partySnapshot, err := partyService.Leave(ctx, party.MemberRequest{PartyID: partyID, ActorID: userID})
	if errors.Is(err, party.ErrPartyMissing) || errors.Is(err, party.ErrMemberMissing) {
		return &Response{ErrorCode: 0x0006}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("playgroupLeave: %w", err)
	}
	removedSlotID := playgroupMemberSlotIDForUser(before, userID)
	networkRegistry.removeMember(partyID, userID)
	if len(partySnapshot.Members) == 0 {
		networkRegistry.removeParty(partyID)
	}
	removedFields := []tdf.Field{
		tdf.FieldNamed("MLST", tdf.ListValue(tdf.Integer, tdf.IntegerValue(removedSlotID))),
		tdf.FieldNamed("PGID", tdf.IntegerValue(uint64(partyID))),
		tdf.FieldNamed("REAS", tdf.IntegerValue(0)),
	}
	if request.Session != nil && request.Session.server != nil &&
		request.Session.server.logger != nil {
		request.Session.server.logger.Printf(
			"playgroup_leave_projection party_id=%d member_id=%d slot_id=%d recipients=%d",
			partyID, userID, removedSlotID, len(before.Members),
		)
	}
	for _, member := range before.Members {
		err = request.QueueUserNotification(member.ID, PlaygroupsComponentID, 0x35, removedFields)
		if err != nil {
			return nil, fmt.Errorf("playgroupLeaveNotify[%d]: %w", member.JoinOrdinal, err)
		}
	}
	if partySnapshot.LeaderID != 0 && partySnapshot.LeaderID != before.LeaderID {
		leaderFields := []tdf.Field{
			tdf.FieldNamed("HSID", tdf.IntegerValue(playgroupLeaderSlotID(partySnapshot))),
			tdf.FieldNamed("LID", tdf.IntegerValue(uint64(partySnapshot.LeaderID))),
			tdf.FieldNamed("PGID", tdf.IntegerValue(uint64(partyID))),
		}
		for _, member := range partySnapshot.Members {
			err = request.QueueUserNotification(member.ID, PlaygroupsComponentID, 0x4f, leaderFields)
			if err != nil {
				return nil, fmt.Errorf("playgroupLeaderNotify[%d]: %w", member.JoinOrdinal, err)
			}
		}
	}
	return &Response{}, nil
}

func isPlaygroupMatchmaking(
	partySnapshot party.Snapshot, leavingUserID int64,
	matchmaking matchmakingDirectory,
) bool {
	if matchmaking == nil || partySnapshot.ID == 0 {
		return false
	}
	for _, member := range partySnapshot.Members {
		if member.ID == leavingUserID {
			continue
		}
		if matchmaking.IsQueued(member.ID) {
			return true
		}
	}
	return false
}

func playgroupDestroyHandler(
	partyService *party.Service, networkRegistry *playgroupNetworkRegistry,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		partyID := uint32(fieldInteger(request.Fields, "PGID"))
		before, isBeforeFound := partyService.Lookup(partyID)
		if !isBeforeFound {
			return &Response{ErrorCode: 0x0006}, nil
		}
		_, err := partyService.Destroy(ctx, party.MemberRequest{PartyID: partyID, ActorID: user.Account.ID})
		if errors.Is(err, party.ErrPartyMissing) || errors.Is(err, party.ErrNotAuthorized) {
			return &Response{ErrorCode: 0x0006}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("playgroupDestroy: %w", err)
		}
		networkRegistry.removeParty(partyID)
		fields := []tdf.Field{
			tdf.FieldNamed("PGID", tdf.IntegerValue(uint64(partyID))),
			tdf.FieldNamed("REAS", tdf.IntegerValue(fieldInteger(request.Fields, "REAS"))),
		}
		for _, member := range before.Members {
			err = request.QueueUserNotification(member.ID, PlaygroupsComponentID, 0x32, fields)
			if err != nil {
				return nil, fmt.Errorf("playgroupDestroyNotify[%d]: %w", member.JoinOrdinal, err)
			}
		}
		return &Response{}, nil
	}
}

func playgroupLookupHandler(partyService *party.Service, networkRegistry *playgroupNetworkRegistry) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		partyID := uint32(fieldInteger(request.Fields, "PGID"))
		partySnapshot, isFound := partyService.Lookup(partyID)
		if !isFound {
			return &Response{ErrorCode: 0x0006}, nil
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("INFO", playgroupInfoValueWithNetwork(
				partySnapshot, networkRegistry, user.Account.ID,
			)),
		}}, nil
	}
}

func playgroupJoinFields(
	partySnapshot party.Snapshot, userID int64, networkRegistry *playgroupNetworkRegistry,
	attributes ...tdf.Value,
) []tdf.Field {
	members := make([]tdf.Value, 0, len(partySnapshot.Members))
	for _, member := range partySnapshot.Members {
		memberValue := playgroupMemberValue(member)
		if network, isFound := networkRegistry.memberForRecipient(
			partySnapshot.ID, member.ID, userID,
		); isFound {
			replaceField(memberValue.Fields, "PNET", network)
		}
		members = append(members, memberValue)
	}
	return []tdf.Field{
		tdf.FieldNamed("INFO", playgroupInfoValueWithNetwork(
			partySnapshot, networkRegistry, userID, attributes...,
		)),
		tdf.FieldNamed("MLST", tdf.ListValue(tdf.Struct, members...)),
		tdf.FieldNamed("USER", tdf.IntegerValue(uint64(userID))),
	}
}

func playgroupInfoValueWithNetwork(
	partySnapshot party.Snapshot, networkRegistry *playgroupNetworkRegistry,
	recipientID int64, attributes ...tdf.Value,
) tdf.Value {
	playgroup := playgroupInfoValue(partySnapshot, attributes...)
	if host, isFound := networkRegistry.hostForRecipient(
		partySnapshot.ID, partySnapshot.LeaderID, recipientID,
	); isFound {
		replaceField(playgroup.Fields, "HNET", host)
	}
	return playgroup
}

func playgroupInfoValue(partySnapshot party.Snapshot, attributes ...tdf.Value) tdf.Value {
	attribute := tdf.MapValue(tdf.String, tdf.String)
	userKey := ""
	if len(attributes) != 0 && attributes[0].Type == tdf.Map {
		attribute = attributes[0]
		for _, entry := range attribute.Entries {
			if entry.Key.Type == tdf.String && entry.Value.Type == tdf.String &&
				strings.EqualFold(entry.Key.String, "PlaygroupKey") {
				userKey = entry.Value.String
				break
			}
		}
	}
	return tdf.StructValue(
		tdf.FieldNamed("ATTR", attribute),
		tdf.FieldNamed("ENBV", tdf.IntegerValue(0)),
		tdf.FieldNamed("HNET", tdf.UnionValue(0x7f)),
		tdf.FieldNamed("HSID", tdf.IntegerValue(playgroupLeaderSlotID(partySnapshot))),
		tdf.FieldNamed("JOIN", tdf.IntegerValue(uint64(partySnapshot.JoinControl))),
		tdf.FieldNamed("MLIM", tdf.IntegerValue(party.MaximumMembers)),
		tdf.FieldNamed("NAME", tdf.StringValue(partySnapshot.Name)),
		tdf.FieldNamed("NTOP", tdf.IntegerValue(playgroupNetworkTopology)),
		tdf.FieldNamed("OWNR", tdf.IntegerValue(uint64(partySnapshot.LeaderID))),
		tdf.FieldNamed("PGID", tdf.IntegerValue(uint64(partySnapshot.ID))),
		tdf.FieldNamed("PRES", tdf.IntegerValue(1)),
		tdf.FieldNamed("UKEY", tdf.StringValue(userKey)),
		tdf.FieldNamed("UPRS", tdf.IntegerValue(1)),
		tdf.FieldNamed("UUID", tdf.StringValue("")),
		tdf.FieldNamed("VOIP", tdf.IntegerValue(0)),
		tdf.FieldNamed("XNNC", tdf.BinaryValue(nil)),
		tdf.FieldNamed("XSES", tdf.BinaryValue(nil)),
	)
}

func playgroupMemberValue(member party.Member) tdf.Value {
	return tdf.StructValue(
		tdf.FieldNamed("ATTR", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("JTIM", tdf.IntegerValue(uint64(member.JoinedAt.Unix()))),
		tdf.FieldNamed("PERM", tdf.IntegerValue(0)),
		tdf.FieldNamed("PNET", tdf.UnionValue(0x7f)),
		tdf.FieldNamed("SID", tdf.IntegerValue(playgroupMemberSlotID(member))),
		tdf.FieldNamed("USER", tdf.StructValue(
			tdf.FieldNamed("AID", tdf.IntegerValue(uint64(member.ID))),
			tdf.FieldNamed("ALOC", tdf.IntegerValue(0)),
			tdf.FieldNamed("EXBB", tdf.BinaryValue(nil)),
			tdf.FieldNamed("EXID", tdf.IntegerValue(userExternalID(member.AvatarID))),
			tdf.FieldNamed("ID", tdf.IntegerValue(uint64(member.ID))),
			tdf.FieldNamed("NAME", tdf.StringValue(member.Name)),
		)),
	)
}

func playgroupJoiningMemberValue(
	member party.Member, requestFields []tdf.Field, memberNetwork tdf.Value,
) (tdf.Value, uint8, uint64) {
	memberValue := playgroupMemberValue(member)
	networkMember := uint8(0x7f)
	if memberNetwork.Type == tdf.Union && memberNetwork.ActiveMember != 0x7f {
		replaceField(memberValue.Fields, "PNET", memberNetwork)
		networkMember = memberNetwork.ActiveMember
	}
	identity, isFound := tdf.Find(requestFields, "USER")
	if !isFound || identity.Type != tdf.Struct {
		return memberValue, networkMember, userExternalID(member.AvatarID)
	}
	identityFields := cloneFields(identity.Fields)
	externalID := userExternalID(member.AvatarID)
	replaceField(identityFields, "AID", tdf.IntegerValue(uint64(member.ID)))
	replaceField(identityFields, "EXID", tdf.IntegerValue(externalID))
	replaceField(identityFields, "ID", tdf.IntegerValue(uint64(member.ID)))
	replaceField(identityFields, "NAME", tdf.StringValue(member.Name))
	replaceField(memberValue.Fields, "USER", tdf.StructValue(identityFields...))
	return memberValue, networkMember, externalID
}

func playgroupLeaderSlotID(partySnapshot party.Snapshot) uint64 {
	for _, member := range partySnapshot.Members {
		if member.ID == partySnapshot.LeaderID {
			return playgroupMemberSlotID(member)
		}
	}
	return 0
}

func playgroupMemberSlotID(member party.Member) uint64 {
	if member.JoinOrdinal == 0 {
		return 0
	}
	return uint64(member.JoinOrdinal - 1)
}

func playgroupMemberSlotIDForUser(partySnapshot party.Snapshot, userID int64) uint64 {
	for _, member := range partySnapshot.Members {
		if member.ID == userID {
			return playgroupMemberSlotID(member)
		}
	}
	return 0
}

func roomsComponent(roomManager *sporenet.RoomManager, presenceRegistry *PresenceRegistry) Component {
	return Component{ID: RoomsComponentID, Name: "Rooms", Commands: map[uint16]Handler{
		0x0a: roomsSelectViewHandler(roomManager),
		0x0b: roomsSelectCategoryHandler(roomManager),
		0x14: roomsJoinHandler(roomManager, presenceRegistry),
		0xa0: func(context.Context, *Request) (*Response, error) { return &Response{}, nil },
	}}
}

func roomsSelectViewHandler(roomManager *sporenet.RoomManager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		viewID := uint32(0)
		if fieldInteger(request.Fields, "UPDT") != 0 {
			viewID = 1
			view := roomManager.View(viewID)
			if view != nil {
				display := ""
				_, isDisplayShown := request.Session.Get(roomViewDisplayShownSessionKey)
				if !isDisplayShown {
					display = "hello"
					request.Session.Set(roomViewDisplayShownSessionKey, true)
				}
				viewFields := view.FieldsWithDisplay(display)
				err := request.QueueNotification(RoomsComponentID, 0x0b, viewFields)
				if err != nil {
					return nil, fmt.Errorf("viewAddNotify: %w", err)
				}
				err = request.QueueNotification(RoomsComponentID, 0x0a, viewFields)
				if err != nil {
					return nil, fmt.Errorf("viewUpdateNotify: %w", err)
				}
			}
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("SEID", tdf.IntegerValue(1)),
			tdf.FieldNamed("UPRE", tdf.IntegerValue(1)),
			tdf.FieldNamed("USID", tdf.IntegerValue(uint64(user.Account.ID))),
			tdf.FieldNamed("VWID", tdf.IntegerValue(uint64(viewID))),
		}}, nil
	}
}

func roomsSelectCategoryHandler(roomManager *sporenet.RoomManager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		viewID := uint32(fieldInteger(request.Fields, "VWID"))
		if viewID != 0 {
			for categoryID := uint32(1); categoryID <= 4; categoryID++ {
				category := roomManager.Category(categoryID)
				if category == nil {
					continue
				}
				err := request.QueueNotification(RoomsComponentID, 0x15, category.Fields())
				if err != nil {
					return nil, fmt.Errorf("categoryAddNotify: %w", err)
				}
				err = request.QueueNotification(RoomsComponentID, 0x14, category.Fields())
				if err != nil {
					return nil, fmt.Errorf("categoryUpdateNotify: %w", err)
				}
			}
		}
		return &Response{Fields: []tdf.Field{tdf.FieldNamed("VWID", tdf.IntegerValue(uint64(viewID)))}}, nil
	}
}

func roomsJoinHandler(roomManager *sporenet.RoomManager, presenceRegistry *PresenceRegistry) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		roomID := uint32(fieldInteger(request.Fields, "RMID"))
		categoryID := uint32(fieldInteger(request.Fields, "CTID"))
		isRoomDiscovery := roomID == 0
		if categoryID == 0 {
			categoryID = 1
		}
		room := roomManager.Room(roomID)
		if isRoomDiscovery {
			// Build 103 uses a zero room ID for its automatic lobby admission,
			// not as a request for a private user-created room. Each static
			// category owns the matching shared room.
			room = roomManager.Room(categoryID)
			if room != nil {
				roomID = room.ID
			}
		}
		if room == nil {
			return &Response{ErrorCode: roomsErrorNotFound}, nil
		}
		category := roomManager.Category(categoryID)
		if category == nil {
			return &Response{ErrorCode: roomsErrorUnknownCategory}, nil
		}
		if category.ViewID == 0 {
			view := roomManager.CreateView()
			category.ViewID = view.ID
		}
		view := roomManager.View(category.ViewID)
		room.SetCategory(category)
		if !room.AddUser(user) {
			return &Response{ErrorCode: roomsErrorFull}, nil
		}
		if isRoomDiscovery {
			// Build 103's join callback immediately resolves the returned room ID
			// from the Rooms SDK cache. Populate that cache before the reply, in
			// the same order as the recovered server.
			err := request.QueueNotificationBeforeReply(RoomsComponentID, 0x1f, room.Fields(category))
			if err != nil {
				return nil, fmt.Errorf("roomCreateNotify: %w", err)
			}
			err = request.QueueNotificationBeforeReply(RoomsComponentID, 0x1e, room.Fields(category))
			if err != nil {
				return nil, fmt.Errorf("roomUpdateNotify: %w", err)
			}
		}
		joinedFields := []tdf.Field{
			tdf.FieldNamed("BZID", tdf.IntegerValue(uint64(user.Account.ID))),
			tdf.FieldNamed("RMID", tdf.IntegerValue(uint64(room.ID))),
		}
		members := room.Users()
		for index, recipient := range members {
			err := queueRoomUserVisibility(request, recipient, members, presenceRegistry)
			if err != nil {
				return nil, fmt.Errorf("roomUserVisibility[%d]: %w", index, err)
			}
			err = request.QueueUserNotification(recipient.Account.ID, RoomsComponentID, 0x1e, room.Fields(category))
			if err != nil {
				return nil, fmt.Errorf("roomUpdateNotify[%d]: %w", index, err)
			}
			err = request.QueueUserNotification(recipient.Account.ID, RoomsComponentID, 0x32, joinedFields)
			if err != nil {
				return nil, fmt.Errorf("roomJoinNotify[%d]: %w", index, err)
			}
			if recipient.Account.ID == user.Account.ID {
				err = queueExistingRoomMembers(request, recipient, user, room.ID, members)
				if err != nil {
					return nil, fmt.Errorf("roomExistingNotify[%d]: %w", index, err)
				}
			}
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("CDAT", tdf.StructValue(category.Fields()...)),
			tdf.FieldNamed("CRIT", tdf.StringValue("")),
			tdf.FieldNamed("MDAT", tdf.StructValue(
				tdf.FieldNamed("BZID", tdf.IntegerValue(uint64(user.Account.ID))),
				tdf.FieldNamed("RMID", tdf.IntegerValue(uint64(room.ID))),
			)),
			tdf.FieldNamed("RDAT", tdf.StructValue(room.Fields(category)...)),
			tdf.FieldNamed("VDAT", tdf.StructValue(view.Fields()...)),
			tdf.FieldNamed("VERS", tdf.IntegerValue(1)),
		}}, nil
	}
}

func queueExistingRoomMembers(
	request *Request, recipient *sporenet.User, joining *sporenet.User,
	roomID uint32, members []*sporenet.User,
) error {
	for index, member := range members {
		if member.Account.ID == joining.Account.ID {
			continue
		}
		fields := []tdf.Field{
			tdf.FieldNamed("BZID", tdf.IntegerValue(uint64(member.Account.ID))),
			tdf.FieldNamed("RMID", tdf.IntegerValue(uint64(roomID))),
		}
		err := request.QueueUserNotification(recipient.Account.ID, RoomsComponentID, 0x32, fields)
		if err != nil {
			return fmt.Errorf("memberNotify[%d]: %w", index, err)
		}
	}
	return nil
}

func queueRoomUserVisibility(
	request *Request, recipient *sporenet.User, members []*sporenet.User,
	presenceRegistry *PresenceRegistry,
) error {
	for index, member := range members {
		if member.Account.ID == recipient.Account.ID {
			continue
		}
		err := queueUserVisibility(
			request, recipient.Account.ID, member, presenceRegistry, 0,
		)
		if err != nil {
			return fmt.Errorf("userVisibility[%d]: %w", index, err)
		}
	}
	return nil
}

func queueUserVisibility(
	request *Request, recipientID int64, subject *sporenet.User,
	presenceRegistry *PresenceRegistry, externalID uint64,
) error {
	variables := []tdf.Field(nil)
	if presenceRegistry != nil {
		record, isFound := presenceRegistry.lookup(subject.Account.ID)
		if isFound {
			variables = record.variables
		}
	}
	err := request.QueueUserNotification(
		recipientID, UserSessionComponentID, userSessionUserAdded,
		userAddedNotificationFieldsWithIdentity(subject, variables, externalID),
	)
	if err != nil {
		return fmt.Errorf("userAdded: %w", err)
	}
	err = request.QueueUserNotification(
		recipientID, UserSessionComponentID, userSessionUserUpdated,
		userUpdatedNotificationFields(subject),
	)
	if err != nil {
		return fmt.Errorf("userUpdated: %w", err)
	}
	return nil
}

func fieldInteger(fields []tdf.Field, label string) uint64 {
	value, isFound := tdf.Find(fields, label)
	if !isFound || value.Type != tdf.Integer {
		return 0
	}
	return value.Integer
}

func boolInteger(isTrue bool) uint64 {
	if isTrue {
		return 1
	}
	return 0
}

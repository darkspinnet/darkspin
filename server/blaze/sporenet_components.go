package blaze

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

const (
	authGetAuthToken         uint16 = 0x24
	authLogin                uint16 = 0x28
	authAcceptTOS            uint16 = 0x29
	authGetTOSInfo           uint16 = 0x2a
	authGetTerms             uint16 = 0x2e
	authGetPrivacy           uint16 = 0x2f
	authSilentLogin          uint16 = 0x32
	authExpressLogin         uint16 = 0x3c
	authLogout               uint16 = 0x46
	authLoginPersona         uint16 = 0x6e
	userSessionLookupUser    uint16 = 0x0c
	userSessionUpdateNetwork uint16 = 0x14
	userSessionUpdateClient  uint16 = 0x19
	userSessionExtendedData  uint16 = 0x01
	userSessionUserAdded     uint16 = 0x02
	userSessionUserUpdated   uint16 = 0x05
	authInvalidUser          uint16 = 0x000b
)

const (
	sessionUserKey              = "sporenet.user"
	sessionNetworkKey           = "sporenet.network"
	sessionPresenceRevisionsKey = "sporenet.presence.revisions"
)

// TokenLoginUsername is the reserved MAIL value used by the injected 5.3.0.103
// client when PASS carries an externally issued launch JWT.
const TokenLoginUsername = "token@local.invalid"

// LoginTokenVerifier validates an external launch credential and returns the
// local account login name named by it.
type LoginTokenVerifier interface {
	Verify(string) (string, error)
}

// UserManager is the Blaze adapter's application boundary. Blaze owns
// TDF and session translation; profile rules and persistence stay behind it.
type UserManager interface {
	Login(context.Context, string, string) sporenet.LoginResult
	LoginTrusted(context.Context, string) sporenet.LoginResult
	Logout(context.Context, *sporenet.User) error
	UserByAuthToken(string) *sporenet.User
	UserByDisplayName(string) *sporenet.User
	UserByLoginName(string) *sporenet.User
	Users() []*sporenet.User
}

// LoginMembershipReconciler restores transient feature projection after login.
type LoginMembershipReconciler interface {
	ReconcileMember(int64) bool
}

// RegisterSporeNetComponents adds authentication and user-session RPCs.
func RegisterSporeNetComponents(
	registry *Registry, userManager UserManager,
	tokenVerifiers ...LoginTokenVerifier,
) error {
	var tokenVerifier LoginTokenVerifier
	if len(tokenVerifiers) != 0 {
		tokenVerifier = tokenVerifiers[0]
	}
	return registerSporeNetComponents(
		registry, userManager, tokenVerifier, nil,
	)
}

// RegisterSporeNetComponentsWithMembership additionally restores transient
// party projection when a returning transport authenticates a durable user.
func RegisterSporeNetComponentsWithMembership(
	registry *Registry, userManager UserManager,
	tokenVerifier LoginTokenVerifier,
	membershipReconciler LoginMembershipReconciler,
	presenceRegistries ...*PresenceRegistry,
) error {
	presenceRegistry := NewPresenceRegistry()
	if len(presenceRegistries) != 0 && presenceRegistries[0] != nil {
		presenceRegistry = presenceRegistries[0]
	}
	return registerSporeNetComponents(
		registry, userManager, tokenVerifier, membershipReconciler, presenceRegistry,
	)
}

func registerSporeNetComponents(
	registry *Registry, userManager UserManager,
	tokenVerifier LoginTokenVerifier,
	membershipReconciler LoginMembershipReconciler,
	presenceRegistries ...*PresenceRegistry,
) error {
	if userManager == nil {
		return errors.New("register SporeNet components: nil user manager")
	}
	err := registry.Register(authenticationComponent(
		userManager, tokenVerifier, membershipReconciler,
	))
	if err != nil {
		return fmt.Errorf("authRegister: %w", err)
	}
	presenceRegistry := NewPresenceRegistry()
	if len(presenceRegistries) != 0 && presenceRegistries[0] != nil {
		presenceRegistry = presenceRegistries[0]
	}
	err = registry.Register(userSessionComponent(userManager, presenceRegistry))
	if err != nil {
		return fmt.Errorf("sessionRegister: %w", err)
	}
	return nil
}

func authenticationComponent(
	userManager UserManager, tokenVerifier LoginTokenVerifier,
	membershipReconciler LoginMembershipReconciler,
) Component {
	empty := func(context.Context, *Request) (*Response, error) { return &Response{}, nil }
	return Component{
		ID:   AuthComponentID,
		Name: "Authentication",
		Commands: map[uint16]Handler{
			authGetAuthToken: func(_ context.Context, request *Request) (*Response, error) {
				user := requestUser(request)
				if user == nil {
					return &Response{ErrorCode: authInvalidUser}, nil
				}
				return &Response{Fields: []tdf.Field{tdf.FieldNamed("AUTH", tdf.StringValue(user.AuthToken))}}, nil
			},
			authLogin:        loginHandler(userManager, tokenVerifier, membershipReconciler),
			authAcceptTOS:    empty,
			authGetTOSInfo:   tosInfoHandler,
			authGetTerms:     termsHandler("Something", "Hello this is something"),
			authGetPrivacy:   termsHandler("Something2", "Hello this is stuff about privacy"),
			authSilentLogin:  silentLoginHandler(userManager, membershipReconciler),
			authExpressLogin: expressLoginHandler(userManager, membershipReconciler),
			authLogout: func(ctx context.Context, request *Request) (*Response, error) {
				user := requestUser(request)
				if user == nil {
					return &Response{}, nil
				}
				err := userManager.Logout(ctx, user)
				if err != nil {
					return nil, fmt.Errorf("blazeLogout: %w", err)
				}
				request.Session.Delete(sessionUserKey)
				return &Response{}, nil
			},
			authLoginPersona: loginPersonaHandler(userManager),
		},
	}
}

func loginHandler(
	userManager UserManager, tokenVerifier LoginTokenVerifier,
	membershipReconciler LoginMembershipReconciler,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		clearSessionUser(request)
		loginName := fieldString(request.Fields, "MAIL")
		password := fieldString(request.Fields, "PASS")
		account := loginName
		method := "password"
		result := sporenet.LoginResult{}
		if loginName == TokenLoginUsername && tokenVerifier != nil {
			account = ""
			method = "jwt"
			verifiedLoginName, err := tokenVerifier.Verify(password)
			if err == nil {
				account = verifiedLoginName
				result = userManager.LoginTrusted(ctx, verifiedLoginName)
			} else {
				logJWTLoginRejection(request, err)
			}
		} else {
			result = userManager.Login(ctx, loginName, password)
		}
		isAccepted := result.IsSuccess || result.IsAlreadyLoggedIn
		logServerLoginAttempt(request, method, account, result.User, isAccepted)
		if !isAccepted {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		reconcileLoginMembership(result.User, membershipReconciler)
		request.Session.Set(sessionUserKey, result.User)
		return &Response{Fields: loginFields(result.User)}, nil
	}
}

func logJWTLoginRejection(request *Request, verificationErr error) {
	if verificationErr == nil || request == nil || request.Session == nil ||
		request.Session.server == nil || request.Session.server.logger == nil {
		return
	}
	request.Session.server.logger.Printf(
		"jwt_login_rejected remote_ip=%q reason=%q",
		serverLoginRemoteIP(request), verificationErr.Error(),
	)
}

func silentLoginHandler(
	userManager UserManager, membershipReconciler LoginMembershipReconciler,
) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		clearSessionUser(request)
		user := userManager.UserByAuthToken(fieldString(request.Fields, "AUTH"))
		account := ""
		if user != nil {
			account = user.LoginName
		}
		logServerLoginAttempt(request, "session", account, user, user != nil)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		reconcileLoginMembership(user, membershipReconciler)
		request.Session.Set(sessionUserKey, user)
		return &Response{Fields: fullLoginFields(user)}, nil
	}
}

func expressLoginHandler(
	userManager UserManager, membershipReconciler LoginMembershipReconciler,
) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		clearSessionUser(request)
		account := fieldString(request.Fields, "MAIL")
		user := userManager.UserByLoginName(account)
		logServerLoginAttempt(request, "express", account, user, user != nil)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		reconcileLoginMembership(user, membershipReconciler)
		request.Session.Set(sessionUserKey, user)
		return &Response{Fields: fullLoginFields(user)}, nil
	}
}

func reconcileLoginMembership(
	user *sporenet.User, membershipReconciler LoginMembershipReconciler,
) {
	if user == nil || membershipReconciler == nil {
		return
	}
	membershipReconciler.ReconcileMember(user.Account.ID)
}

func logServerLoginAttempt(request *Request, method, account string, user *sporenet.User, isAccepted bool) {
	if request == nil || request.Session == nil || request.Session.server == nil || request.Session.server.logger == nil {
		return
	}
	displayName := ""
	if user != nil {
		displayName = user.DisplayName
	}
	result := "rejected"
	if isAccepted {
		result = "accepted"
	}
	request.Session.server.logger.Printf(
		"server_login_attempt remote_ip=%q account=%q display_name=%q method=%q result=%q",
		serverLoginRemoteIP(request), account, displayName, method, result,
	)
}

func serverLoginRemoteIP(request *Request) string {
	if request == nil {
		return ""
	}
	return sessionRemoteIP(request.Session)
}

func loginPersonaHandler(userManager UserManager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		err := queueLoginNotifications(request, user)
		if err != nil {
			return nil, fmt.Errorf("loginNotify: %w", err)
		}
		err = queuePeerLoginVisibility(request, user, userManager.Users())
		if err != nil {
			return nil, fmt.Errorf("loginPeerVisibility: %w", err)
		}
		return &Response{Fields: sessionFields(user)}, nil
	}
}

func queueLoginNotifications(request *Request, user *sporenet.User) error {
	err := request.QueueNotification(
		UserSessionComponentID, userSessionUserAdded, userAddedNotificationFields(user),
	)
	if err != nil {
		return fmt.Errorf("userAdded: %w", err)
	}
	err = request.QueueNotification(UserSessionComponentID, userSessionUserUpdated, []tdf.Field{
		tdf.FieldNamed("FLGS", tdf.IntegerValue(3)),
		tdf.FieldNamed("ID", tdf.IntegerValue(uint64(user.Account.ID))),
	})
	if err != nil {
		return fmt.Errorf("userUpdated: %w", err)
	}
	return nil
}

func queuePeerLoginVisibility(request *Request, user *sporenet.User, peers []*sporenet.User) error {
	for index, peer := range peers {
		if peer == nil || peer.Account.ID == user.Account.ID {
			continue
		}
		err := request.QueueNotification(
			UserSessionComponentID, userSessionUserAdded, userAddedNotificationFields(peer),
		)
		if err != nil {
			return fmt.Errorf("peerAddedLocal[%d]: %w", index, err)
		}
		err = request.QueueNotification(UserSessionComponentID, userSessionUserUpdated, userUpdatedNotificationFields(peer))
		if err != nil {
			return fmt.Errorf("peerUpdatedLocal[%d]: %w", index, err)
		}
		err = request.QueueUserNotification(
			peer.Account.ID, UserSessionComponentID, userSessionUserAdded, userAddedNotificationFields(user),
		)
		if err != nil {
			return fmt.Errorf("peerAddedRemote[%d]: %w", index, err)
		}
		err = request.QueueUserNotification(
			peer.Account.ID, UserSessionComponentID, userSessionUserUpdated, userUpdatedNotificationFields(user),
		)
		if err != nil {
			return fmt.Errorf("peerUpdatedRemote[%d]: %w", index, err)
		}
	}
	return nil
}

func userUpdatedNotificationFields(user *sporenet.User) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("FLGS", tdf.IntegerValue(3)),
		tdf.FieldNamed("ID", tdf.IntegerValue(uint64(user.Account.ID))),
	}
}

func userOfflineNotificationFields(userID int64) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("FLGS", tdf.IntegerValue(1)),
		tdf.FieldNamed("ID", tdf.IntegerValue(uint64(userID))),
	}
}

func tosInfoHandler(context.Context, *Request) (*Response, error) {
	return &Response{Fields: []tdf.Field{
		tdf.FieldNamed("EAMC", tdf.IntegerValue(0)),
		tdf.FieldNamed("PMC", tdf.IntegerValue(0)),
		tdf.FieldNamed("PRIV", tdf.StringValue("")),
		tdf.FieldNamed("THST", tdf.StringValue("")),
		tdf.FieldNamed("TURI", tdf.StringValue("")),
	}}, nil
}

func termsHandler(document, content string) Handler {
	return func(context.Context, *Request) (*Response, error) {
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("LDVC", tdf.StringValue(document)),
			tdf.FieldNamed("TCOL", tdf.IntegerValue(uint64(len(content)))),
			tdf.FieldNamed("TCOT", tdf.StringValue(content)),
		}}, nil
	}
}

func loginFields(user *sporenet.User) []tdf.Field {
	persona := personaFields(user)
	return []tdf.Field{
		tdf.FieldNamed("NTOS", tdf.IntegerValue(0)),
		tdf.FieldNamed("PCTK", tdf.StringValue("unknown_data")),
		tdf.FieldNamed("PLST", tdf.ListValue(tdf.Struct, tdf.StructValue(persona...))),
		tdf.FieldNamed("PRIV", tdf.StringValue("")),
		tdf.FieldNamed("SKEY", tdf.StringValue("telemetry_key")),
		tdf.FieldNamed("SPAM", tdf.IntegerValue(0)),
		tdf.FieldNamed("THST", tdf.StringValue("")),
		tdf.FieldNamed("TURI", tdf.StringValue("")),
		tdf.FieldNamed("UID", tdf.IntegerValue(uint64(user.Account.ID))),
	}
}

func fullLoginFields(user *sporenet.User) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("AGUP", tdf.IntegerValue(0)),
		tdf.FieldNamed("NTOS", tdf.IntegerValue(0)),
		tdf.FieldNamed("PCTK", tdf.StringValue("")),
		tdf.FieldNamed("PRIV", tdf.StringValue("")),
		tdf.FieldNamed("SESS", tdf.StructValue(sessionFields(user)...)),
		tdf.FieldNamed("SPAM", tdf.IntegerValue(0)),
		tdf.FieldNamed("THST", tdf.StringValue("")),
		tdf.FieldNamed("TURI", tdf.StringValue("")),
	}
}

func sessionFields(user *sporenet.User) []tdf.Field {
	now := uint64(time.Now().Unix())
	return []tdf.Field{
		tdf.FieldNamed("BUID", tdf.IntegerValue(uint64(user.Account.ID))),
		tdf.FieldNamed("FRST", tdf.IntegerValue(0)),
		tdf.FieldNamed("KEY", tdf.StringValue("telemetry_key")),
		tdf.FieldNamed("LLOG", tdf.IntegerValue(now)),
		tdf.FieldNamed("MAIL", tdf.StringValue(user.LoginName)),
		tdf.FieldNamed("PDTL", tdf.StructValue(personaFields(user)...)),
		tdf.FieldNamed("UID", tdf.IntegerValue(uint64(user.Account.ID))),
	}
}

func personaFields(user *sporenet.User) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("DSNM", tdf.StringValue(user.DisplayName)),
		tdf.FieldNamed("LAST", tdf.IntegerValue(uint64(time.Now().Unix()))),
		tdf.FieldNamed("PID", tdf.IntegerValue(uint64(user.Account.ID))),
		tdf.FieldNamed("STAS", tdf.IntegerValue(2)),
		tdf.FieldNamed("XREF", tdf.IntegerValue(0)),
		tdf.FieldNamed("XTYP", tdf.IntegerValue(0)),
	}
}

func userSessionComponent(userManager UserManager, presenceRegistry *PresenceRegistry) Component {
	return Component{
		ID: UserSessionComponentID, Name: "UserSessions",
		Commands: map[uint16]Handler{
			userSessionLookupUser:    lookupUserHandler(userManager, presenceRegistry),
			userSessionUpdateNetwork: updateNetworkInfoHandler,
			userSessionUpdateClient:  updateUserSessionClientDataHandler(userManager, presenceRegistry),
		},
	}
}

func updateUserSessionClientDataHandler(
	userManager UserManager, presenceRegistry *PresenceRegistry,
) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		variables := presenceVariableFields(request.Fields)
		if len(variables) == 0 {
			return &Response{}, nil
		}
		users := userManager.Users()
		publishedVariables := variables
		variables = authoritativePresenceVariableFields(user, users, variables)
		record, isChanged := presenceRegistry.update(user.Account.ID, variables)
		if isChanged && request.Session != nil && request.Session.server != nil &&
			request.Session.server.logger != nil {
			typeID := variables[0].Value.VariableTypeID
			publishedFields := presenceVariableDataFields(publishedVariables)
			presenceFields := variables[0].Value.VariableField.Value.Fields
			request.Session.server.logger.Printf(
				"presence_projection user_id=%d type_id=%d incoming_group=%d incoming_status=%d group=%d level=%d status=%d extra=%d",
				user.Account.ID, typeID, fieldInteger(publishedFields, "GRP"),
				fieldInteger(publishedFields, "STAT"),
				fieldInteger(presenceFields, "GRP"), fieldInteger(presenceFields, "LVL"), fieldInteger(presenceFields, "STAT"),
				fieldInteger(presenceFields, "XTRA"),
			)
		}
		usersByID := make(map[int64]*sporenet.User, len(users))
		for _, subject := range users {
			if subject != nil {
				usersByID[subject.Account.ID] = subject
			}
		}
		if isChanged {
			fields := presenceNotificationFields(user, record.variables)
			for recipientIndex, recipient := range users {
				if recipient == nil || recipient.Account.ID == user.Account.ID {
					continue
				}
				err := request.QueueUserNotification(
					recipient.Account.ID, UserSessionComponentID, userSessionUserUpdated,
					userUpdatedNotificationFields(user),
				)
				if err != nil {
					return nil, fmt.Errorf("presenceOwnerUpdate[%d]: %w", recipientIndex, err)
				}
				err = request.QueueUserNotification(
					recipient.Account.ID, UserSessionComponentID, userSessionExtendedData, fields,
				)
				if err != nil {
					return nil, fmt.Errorf("presenceBroadcast[%d]: %w", recipientIndex, err)
				}
			}
		}
		revisions := make(map[int64]uint64)
		storedRevisions, isFound := request.Session.Get(sessionPresenceRevisionsKey)
		if isFound {
			storedMap, isMap := storedRevisions.(map[int64]uint64)
			if isMap {
				revisions = storedMap
			}
		}
		for subjectID, subjectRecord := range presenceRegistry.recordsSnapshot() {
			if subjectID == user.Account.ID {
				revisions[subjectID] = subjectRecord.revision
				continue
			}
			if revisions[subjectID] == subjectRecord.revision {
				continue
			}
			subject := usersByID[subjectID]
			if subject == nil {
				continue
			}
			err := request.QueueNotification(
				UserSessionComponentID, userSessionExtendedData,
				presenceNotificationFields(subject, subjectRecord.variables),
			)
			if err != nil {
				return nil, fmt.Errorf("presenceReplay[%d]: %w", subjectID, err)
			}
			revisions[subjectID] = subjectRecord.revision
		}
		request.Session.Set(sessionPresenceRevisionsKey, revisions)
		return &Response{}, nil
	}
}

func updateNetworkInfoHandler(_ context.Context, request *Request) (*Response, error) {
	user := requestUser(request)
	if user == nil {
		return &Response{ErrorCode: authInvalidUser}, nil
	}
	data := extendedDataFields(user)
	if address, isFound := tdf.Find(request.Fields, "ADDR"); isFound && address.Type == tdf.Union {
		replaceField(data, "ADDR", address)
		request.Session.Set(sessionNetworkKey, address)
	}
	replaceField(data, "UATT", tdf.IntegerValue(0x4000000000000000))
	err := request.QueueNotification(UserSessionComponentID, userSessionExtendedData, []tdf.Field{
		tdf.FieldNamed("DATA", tdf.StructValue(data...)),
		tdf.FieldNamed("USID", tdf.IntegerValue(uint64(user.Account.ID))),
	})
	if err != nil {
		return nil, fmt.Errorf("networkNotify: %w", err)
	}
	return &Response{}, nil
}

func replaceField(fields []tdf.Field, label string, value tdf.Value) {
	for index := range fields {
		if fields[index].Label == label {
			fields[index].Value = value
			return
		}
	}
}

func lookupUserHandler(userManager UserManager, presenceRegistry *PresenceRegistry) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		name := fieldString(request.Fields, "NAME")
		var user *sporenet.User
		if name != "" {
			user = userManager.UserByLoginName(name)
			if user == nil {
				user = userManager.UserByDisplayName(name)
			}
		}
		if user == nil {
			user = requestUser(request)
		}
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		variables := []tdf.Field(nil)
		if presenceRegistry != nil {
			record, isFound := presenceRegistry.lookup(user.Account.ID)
			if isFound {
				variables = record.variables
			}
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("EDAT", tdf.StructValue(extendedDataFieldsWithPresence(user, variables)...)),
			tdf.FieldNamed("FLGS", tdf.IntegerValue(3)),
			tdf.FieldNamed("USER", tdf.StructValue(userLookupIdentificationFields(user, request.Fields)...)),
		}}, nil
	}
}

func userLookupIdentificationFields(user *sporenet.User, requestFields []tdf.Field) []tdf.Field {
	fields := userIdentificationFields(user)
	externalID := fieldInteger(requestFields, "EXID")
	if externalID == 0 {
		return fields
	}
	replaceField(fields, "EXID", tdf.IntegerValue(userExternalID(user.Account.AvatarID)))
	return fields
}

func userAddedNotificationFields(user *sporenet.User) []tdf.Field {
	return userAddedNotificationFieldsWithPresence(user, nil)
}

func userAddedNotificationFieldsWithPresence(user *sporenet.User, variables []tdf.Field) []tdf.Field {
	return userAddedNotificationFieldsWithIdentity(user, variables, 0)
}

func userAddedNotificationFieldsWithIdentity(
	user *sporenet.User, variables []tdf.Field, externalID uint64,
) []tdf.Field {
	identityFields := userIdentificationFields(user)
	if externalID != 0 {
		replaceField(identityFields, "EXID", tdf.IntegerValue(externalID))
	}
	return []tdf.Field{
		tdf.FieldNamed("DATA", tdf.StructValue(extendedDataFieldsWithPresence(user, variables)...)),
		tdf.FieldNamed("USER", tdf.StructValue(identityFields...)),
	}
}

func userIdentificationFields(user *sporenet.User) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("AID", tdf.IntegerValue(uint64(user.Account.ID))),
		tdf.FieldNamed("ALOC", tdf.IntegerValue(0)),
		tdf.FieldNamed("EXBB", tdf.BinaryValue(nil)),
		tdf.FieldNamed("EXID", tdf.IntegerValue(0)),
		tdf.FieldNamed("ID", tdf.IntegerValue(uint64(user.Account.ID))),
		tdf.FieldNamed("NAME", tdf.StringValue(user.DisplayName)),
	}
}

func userExternalID(avatarID uint32) uint64 {
	return uint64(1) | uint64(avatarID)<<32
}

func extendedDataFields(user *sporenet.User) []tdf.Field {
	_ = user
	latencies := make([]tdf.Value, 5)
	for index := range latencies {
		latencies[index] = tdf.IntegerValue(1161889797)
	}
	return []tdf.Field{
		tdf.FieldNamed("ADDR", tdf.UnionValue(0x7f)),
		tdf.FieldNamed("BPS", tdf.StringValue("gva")),
		tdf.FieldNamed("CMAP", tdf.MapValue(tdf.Integer, tdf.Integer)),
		tdf.FieldNamed("CTY", tdf.StringValue("US")),
		tdf.FieldNamed("CVAR", tdf.Value{Type: tdf.Variable}),
		tdf.FieldNamed("DMAP", tdf.MapValue(tdf.Integer, tdf.Integer)),
		tdf.FieldNamed("HWFG", tdf.IntegerValue(1)),
		tdf.FieldNamed("PSLM", tdf.ListValue(tdf.Integer, latencies...)),
		tdf.FieldNamed("QDAT", tdf.StructValue(
			tdf.FieldNamed("DBPS", tdf.IntegerValue(128000)),
			tdf.FieldNamed("NATT", tdf.IntegerValue(0)),
			tdf.FieldNamed("UBPS", tdf.IntegerValue(2)),
		)),
		tdf.FieldNamed("UATT", tdf.IntegerValue(3)),
		tdf.FieldNamed("ULST", tdf.ListValue(tdf.ObjectID,
			tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{4, 1, 0}},
			tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{5, 1, 0}},
		)),
	}
}

func extendedDataFieldsWithPresence(user *sporenet.User, variables []tdf.Field) []tdf.Field {
	fields := extendedDataFields(user)
	if len(variables) != 1 || variables[0].Label != "CVAR" || variables[0].Value.Type != tdf.Variable {
		return fields
	}
	replaceField(fields, "CVAR", variables[0].Value)
	return fields
}

func requestUser(request *Request) *sporenet.User {
	if request == nil || request.Session == nil {
		return nil
	}
	value, isFound := request.Session.Get(sessionUserKey)
	if !isFound {
		return nil
	}
	user, isUser := value.(*sporenet.User)
	if !isUser {
		return nil
	}
	return user
}

func requestNetwork(request *Request) (tdf.Value, bool) {
	if request == nil || request.Session == nil {
		return tdf.Value{}, false
	}
	network, isFound := request.Session.Get(sessionNetworkKey)
	if !isFound {
		return tdf.Value{}, false
	}
	address, isNetwork := network.(tdf.Value)
	return address, isNetwork && address.Type == tdf.Union && address.ActiveMember != 0x7f
}

func clearSessionUser(request *Request) {
	if request == nil || request.Session == nil {
		return
	}
	request.Session.Delete(sessionUserKey)
	request.Session.Delete(sessionNetworkKey)
}

func fieldString(fields []tdf.Field, label string) string {
	value, isFound := tdf.Find(fields, label)
	if !isFound || value.Type != tdf.String {
		return ""
	}
	return value.String
}

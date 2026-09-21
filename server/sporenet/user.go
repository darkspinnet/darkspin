package sporenet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User is one persisted SporeNet account.
type User struct {
	mu       sync.RWMutex
	mutation sync.Mutex

	DisplayName                 string
	LoginName                   string
	Password                    string
	CreateDT                    time.Time
	LastConnectionDT            time.Time
	AuthToken                   string
	IsTutorialCompletionPending bool
	Account                     Account
	Stats                       PlayerStats
	Squads                      []Squad
	Creatures                   []*Creature
	Parts                       []Part
	Events                      []UserEvent
	CampaignExperiences         []CampaignExperience
	State                       uint32
	RoomID                      uint32
	GameID                      uint32
	PlaygroupID                 uint32
	Associations                map[uint32][]AssociationMember
	Settings                    map[string]string
	playStartedAt               time.Time

	extensions []OpaqueField
}

// UserEvent is one durable, append-only profile activity entry. Key is a
// server-owned idempotency identity and is never exposed to the client.
type UserEvent struct {
	Key        string
	MessageID  uint32
	Metadata   string
	OccurredAt int64
	IsPublic   bool
}

func (u *User) startPlayTime(now time.Time) {
	if u == nil || now.IsZero() {
		return
	}
	u.mu.Lock()
	u.playStartedAt = now
	u.mu.Unlock()
}

func (u *User) accruePlayTime(now time.Time) {
	if u == nil || now.IsZero() {
		return
	}
	u.mu.Lock()
	if u.playStartedAt.IsZero() || !now.After(u.playStartedAt) {
		u.mu.Unlock()
		return
	}
	seconds := uint64(now.Sub(u.playStartedAt) / time.Second)
	if seconds == 0 {
		u.mu.Unlock()
		return
	}
	u.Stats.PVEPlayTimeSecond += seconds
	u.playStartedAt = u.playStartedAt.Add(time.Duration(seconds) * time.Second)
	u.mu.Unlock()
}

// AssociationMember is one friend/ignore-list entry.
type AssociationMember struct {
	ID   int64
	Name string
	Time uint64
}

// PresenceSnapshot is the transient, transport-neutral profile projection used
// by social surfaces such as the lobby player browser.
type PresenceSnapshot struct {
	Experience       uint32
	ChainProgression uint32
	PlaygroupID      uint32
}

// PresenceSnapshot returns one consistent social-presence projection.
func (u *User) PresenceSnapshot() PresenceSnapshot {
	if u == nil {
		return PresenceSnapshot{}
	}
	u.mu.RLock()
	presence := PresenceSnapshot{
		Experience:       u.Account.XP,
		ChainProgression: u.Account.ChainProgression,
		PlaygroupID:      u.PlaygroupID,
	}
	u.mu.RUnlock()
	return presence
}

// FriendAssociationList is the build-103 AssociationLists type used for the
// player's friend list. Other list types must never contribute profile events.
const FriendAssociationList uint32 = 5

// UpdateAssociations applies one client association-list mutation.
func (u *User) UpdateAssociations(listType uint32, members []AssociationMember, isAddition bool) {
	if u == nil || listType == 0 || len(members) == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Associations == nil {
		u.Associations = make(map[uint32][]AssociationMember)
	}
	current := u.Associations[listType]
	if !isAddition {
		remaining := current[:0]
		for _, candidate := range current {
			isRemoved := false
			for _, member := range members {
				if candidate.ID == member.ID {
					isRemoved = true
					break
				}
			}
			if !isRemoved {
				remaining = append(remaining, candidate)
			}
		}
		u.Associations[listType] = remaining
		return
	}
	for _, member := range members {
		if member.ID <= 0 {
			continue
		}
		isFound := false
		for index := range current {
			if current[index].ID != member.ID {
				continue
			}
			current[index] = member
			isFound = true
			break
		}
		if !isFound {
			current = append(current, member)
		}
	}
	u.Associations[listType] = current
}

// AssociationSnapshot returns a detached association list.
func (u *User) AssociationSnapshot(listType uint32) []AssociationMember {
	if u == nil {
		return nil
	}
	u.mu.RLock()
	members := append([]AssociationMember(nil), u.Associations[listType]...)
	u.mu.RUnlock()
	return members
}

// NewUser creates an account with the C++ defaults and three squad slots.
func NewUser(displayName, loginName, password string) *User {
	return &User{
		DisplayName: displayName, LoginName: loginName, Password: password,
		CreateDT:     time.Now().UTC(),
		Account:      defaultAccount(),
		Squads:       []Squad{NewSquad(1), NewSquad(2), NewSquad(3)},
		Associations: make(map[uint32][]AssociationMember),
		Settings:     make(map[string]string),
	}
}

// UpdateState changes state and reports whether it changed.
func (u *User) UpdateState(state uint32) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.State == state {
		return false
	}
	u.State = state
	return true
}

// SquadByID finds a squad by its persistent ID.
func (u *User) SquadByID(id uint32) *Squad {
	u.mu.Lock()
	defer u.mu.Unlock()
	for index := range u.Squads {
		if u.Squads[index].ID == id {
			return &u.Squads[index]
		}
	}
	return nil
}

// ClaimGame atomically prevents one active user from joining multiple worlds.
func (u *User) ClaimGame(id uint32) bool {
	if id == 0 {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.GameID != 0 && u.GameID != id {
		return false
	}
	u.GameID = id
	return true
}

// ReleaseGame clears membership only when it still belongs to the given world.
func (u *User) ReleaseGame(id uint32) {
	u.mu.Lock()
	if u.GameID == id {
		u.GameID = 0
	}
	u.mu.Unlock()
}

// CurrentGameID returns the user's active world without exposing its lock.
func (u *User) CurrentGameID() uint32 {
	u.mu.RLock()
	id := u.GameID
	u.mu.RUnlock()
	return id
}

// ClaimPlaygroup atomically prevents one active user from joining multiple parties.
func (u *User) ClaimPlaygroup(id uint32) bool {
	if id == 0 {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.PlaygroupID != 0 && u.PlaygroupID != id {
		return false
	}
	u.PlaygroupID = id
	return true
}

// ReleasePlaygroup clears membership only when it still belongs to the given party.
func (u *User) ReleasePlaygroup(id uint32) {
	u.mu.Lock()
	if u.PlaygroupID == id {
		u.PlaygroupID = 0
	}
	u.mu.Unlock()
}

// CurrentPlaygroupID returns active party membership without exposing the user's lock.
func (u *User) CurrentPlaygroupID() uint32 {
	u.mu.RLock()
	id := u.PlaygroupID
	u.mu.RUnlock()
	return id
}

// UserManager owns active users and delegates durable profiles to a repository.
type UserManager struct {
	repository       UserRepository
	partScorer       PartScorer
	mu               sync.RWMutex
	activeUsers      map[string]*User
	activeUsersByID  map[int64]*User
	usersByToken     map[string]*User
	template         *TemplateDatabase
	worldPlayerLimit uint32
	sessions         [256]sync.Mutex
}

// PartScorer is the itemization-owned port used to derive authoritative editor
// scores and authored prices without coupling the profile feature to content storage.
type PartScorer interface {
	EquipmentScore([]Part) (float32, float32)
	PartCost(uint16) uint32
}

// SetPartScorer installs immutable itemization rules during server composition.
func (m *UserManager) SetPartScorer(partScorer PartScorer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.partScorer = partScorer
	m.mu.Unlock()
}

// LoginResult mirrors recap_server's success/already-logged-in result.
type LoginResult struct {
	User              *User
	IsSuccess         bool
	IsAlreadyLoggedIn bool
	IsWorldFull       bool
}

// SetWorldPlayerLimit sets the maximum number of simultaneously authenticated
// users. A zero limit permits an unlimited number of users.
func (m *UserManager) SetWorldPlayerLimit(limit uint32) {
	m.mu.Lock()
	m.worldPlayerLimit = limit
	m.mu.Unlock()
}

// Registration is the application command used by the recap registration
// endpoint. Transport parsing stays outside the SporeNet domain.
type Registration struct {
	DisplayName       string
	LoginName         string
	Password          string
	AvatarID          uint32
	IsTutorialSkipped bool
}

// NewUserManagerWithRepository creates a user manager with an injected
// persistence adapter. Concrete storage is selected by the composition root.
func NewUserManagerWithRepository(repository UserRepository, templates ...*TemplateDatabase) (*UserManager, error) {
	if repository == nil {
		return nil, errors.New("create user manager: nil repository")
	}
	var templateDatabase *TemplateDatabase
	if len(templates) > 0 {
		templateDatabase = templates[0]
	}
	return &UserManager{
		repository: repository, activeUsers: make(map[string]*User),
		activeUsersByID: make(map[int64]*User), usersByToken: make(map[string]*User),
		template: templateDatabase,
	}, nil
}

// SignUp creates and persists a unique user with the domain defaults.
func (m *UserManager) SignUp(ctx context.Context, displayName, loginName, password string) (*User, error) {
	user, err := m.createUser(ctx, displayName, loginName, password, nil)
	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}
	return user, nil
}

// Register creates the elevated account used by the local recap registration
// endpoint. The transport submits data; this application service owns defaults
// and persistence.
func (m *UserManager) Register(ctx context.Context, command Registration) (*User, error) {
	configure := func(user *User) {
		user.Account.AvatarID = registrationAvatarID(command.AvatarID)
		user.Account.Level = 100
		user.Account.CreatureRewards = 100
		user.Account.DNA = 10000000
		user.Account.XP = 10000
		user.Account.IsAllAccessGranted = true
		user.Account.IsOnlineAccessGranted = true
	}
	user, err := m.createUser(ctx, command.DisplayName, command.LoginName, command.Password, configure)
	if err != nil {
		return nil, fmt.Errorf("registration: %w", err)
	}
	return user, nil
}

// RegisterDesktop creates a normal playable profile submitted by a trusted
// Darkspinner account-registration endpoint.
func (m *UserManager) RegisterDesktop(ctx context.Context, command Registration) (*User, error) {
	configure := func(user *User) {
		user.Account.AvatarID = registrationAvatarID(command.AvatarID)
		user.IsTutorialCompletionPending = command.IsTutorialSkipped
	}
	user, err := m.createUser(ctx, command.DisplayName, command.LoginName, command.Password, configure)
	if err != nil {
		return nil, fmt.Errorf("desktopRegistration: %w", err)
	}
	if !command.IsTutorialSkipped {
		return user, nil
	}
	login := m.LoginTrusted(ctx, command.LoginName)
	if !login.IsSuccess || login.User == nil {
		// The persisted pending marker keeps completion recoverable at server startup.
		return user, nil
	}
	tutorialCompletion, completeErr := m.CompleteTutorial(ctx, login.User.Account.ID)
	logoutErr := m.Logout(ctx, login.User)
	if logoutErr != nil {
		return nil, fmt.Errorf("tutorialLogout: %w", logoutErr)
	}
	if completeErr != nil {
		// Missing content leaves the durable pending marker for startup reconciliation.
		return user, nil
	}
	if tutorialCompletion.CumulativeXP < int32(tutorialCompletionExperience) {
		// An incomplete result retains the original durable registration snapshot.
		return user, nil
	}
	return login.User, nil
}

func registrationAvatarID(avatarID uint32) uint32 {
	if avatarID < 1 || avatarID > 15 {
		return 0
	}
	return avatarID
}

func (m *UserManager) createUser(ctx context.Context, displayName, loginName, password string, configure func(*User)) (*User, error) {
	session := m.sessionLock(loginName)
	session.Lock()
	defer session.Unlock()
	m.mu.RLock()
	if _, isFound := m.activeUsers[loginKey(loginName)]; isFound {
		m.mu.RUnlock()
		return nil, errors.New("there is already a registered user with that e-mail")
	}
	m.mu.RUnlock()

	storedPassword, err := passwordForStorage(password)
	if err != nil {
		return nil, fmt.Errorf("passwordHash: %w", err)
	}
	user := NewUser(displayName, loginName, storedPassword)
	if configure != nil {
		configure(user)
	}
	id, err := m.repository.Create(ctx, user.Record())
	if errors.Is(err, ErrUserExists) {
		return nil, errors.New("there is already a registered user with that e-mail")
	}
	if err != nil {
		return nil, fmt.Errorf("userCreate: %w", err)
	}
	user.Account.ID = id
	return user, nil
}

// Login loads and activates a user when credentials match.
func (m *UserManager) Login(ctx context.Context, loginName, password string) LoginResult {
	verify := func(storedPassword string) bool {
		return isPasswordMatch(storedPassword, password)
	}
	return m.login(ctx, loginName, verify)
}

// Authenticate verifies credentials without activating a game session. It is
// used by the desktop broker before issuing a short-lived launch credential.
func (m *UserManager) Authenticate(
	ctx context.Context, loginName, password string,
) (UserIdentity, error) {
	if m == nil || m.repository == nil {
		return UserIdentity{}, errors.New("user repository unavailable")
	}
	record, err := m.repository.LoadByLoginName(ctx, strings.TrimSpace(loginName))
	if err != nil {
		return UserIdentity{}, fmt.Errorf("credentialLoad: %w", err)
	}
	if !isPasswordMatch(record.Password, password) {
		return UserIdentity{}, errors.New("invalid account or password")
	}
	return UserIdentity{
		LoginName: record.LoginName, DisplayName: record.DisplayName,
		CreateDT: record.CreateDT, LastConnectionDT: record.LastConnectionDT,
		AvatarID: record.Account.AvatarID, Level: record.Account.Level,
		XP: record.Account.XP, ChainProgression: record.Account.ChainProgression,
		IsTutorialCompleted:         record.Account.IsTutorialCompleted(),
		IsTutorialCompletionPending: record.IsTutorialCompletionPending,
	}, nil
}

func passwordForStorage(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	contents, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcryptGenerate: %w", err)
	}
	return string(contents), nil
}

func isPasswordMatch(storedPassword, password string) bool {
	if strings.HasPrefix(storedPassword, "$2") {
		err := bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(password))
		return err == nil
	}
	return storedPassword == password
}

// LoginTrusted activates an account whose identity was established by an
// external authentication verifier. It never compares or exposes the stored
// account password.
func (m *UserManager) LoginTrusted(ctx context.Context, loginName string) LoginResult {
	return m.login(ctx, loginName, func(string) bool { return true })
}

// FindUserID resolves a durable profile without activating a login session.
func (m *UserManager) FindUserID(ctx context.Context, loginName string) (int64, error) {
	if m == nil || m.repository == nil {
		return 0, errors.New("user repository unavailable")
	}
	if strings.TrimSpace(loginName) == "" {
		return 0, fmt.Errorf("userIdentity: %w", ErrUserNotFound)
	}
	m.mu.RLock()
	active := m.activeUsers[loginKey(loginName)]
	m.mu.RUnlock()
	if active != nil {
		active.mu.RLock()
		userID := active.Account.ID
		active.mu.RUnlock()
		return userID, nil
	}
	record, err := m.repository.LoadByLoginName(ctx, loginName)
	if err != nil {
		return 0, fmt.Errorf("userLookup: %w", err)
	}
	return record.Account.ID, nil
}

func (m *UserManager) login(ctx context.Context, loginName string, verify func(string) bool) LoginResult {
	session := m.sessionLock(loginName)
	session.Lock()
	defer session.Unlock()
	m.mu.RLock()
	active := m.activeUsers[loginKey(loginName)]
	m.mu.RUnlock()
	if active != nil {
		active.mu.RLock()
		isVerified := verify != nil && verify(active.Password)
		active.mu.RUnlock()
		if !isVerified {
			return LoginResult{}
		}
		return LoginResult{User: active, IsAlreadyLoggedIn: true}
	}
	record, err := m.repository.LoadByLoginName(ctx, loginName)
	if err != nil {
		return LoginResult{}
	}
	user := NewUserFromRecord(record, m.template)
	m.mu.RLock()
	partScorer := m.partScorer
	m.mu.RUnlock()
	if partScorer != nil {
		for index := range user.Parts {
			if index < len(record.Parts) && record.Parts[index].Cost == 0 {
				user.Parts[index].Cost = partScorer.PartCost(user.Parts[index].Level)
			}
		}
	}
	if verify == nil || !verify(record.Password) {
		return LoginResult{User: user}
	}
	err = m.prepareProfileStart(ctx, user)
	if err != nil {
		return LoginResult{User: user}
	}
	token, err := randomToken()
	if err != nil {
		return LoginResult{User: user}
	}
	m.mu.Lock()
	if m.worldPlayerLimit != 0 && uint32(len(m.activeUsers)) >= m.worldPlayerLimit {
		m.mu.Unlock()
		return LoginResult{User: user, IsWorldFull: true}
	}
	playStartedAt := time.Now()
	user.AuthToken = token
	user.LastConnectionDT = playStartedAt.UTC()
	user.startPlayTime(playStartedAt)
	m.activeUsers[loginKey(user.LoginName)] = user
	m.activeUsersByID[user.Account.ID] = user
	m.usersByToken[user.AuthToken] = user
	m.mu.Unlock()
	return LoginResult{User: user, IsSuccess: true}
}

// Logout saves and removes an active user.
func (m *UserManager) Logout(ctx context.Context, user *User) error {
	if user == nil {
		return nil
	}
	session := m.sessionLock(user.LoginName)
	session.Lock()
	defer session.Unlock()
	user.mutation.Lock()
	user.accruePlayTime(time.Now())
	err := m.repository.Save(ctx, user.Record())
	user.mutation.Unlock()
	if err != nil {
		return fmt.Errorf("logoutSave: %w", err)
	}
	m.mu.Lock()
	key := loginKey(user.LoginName)
	if m.activeUsers[key] == user {
		delete(m.activeUsers, key)
		delete(m.activeUsersByID, user.Account.ID)
		delete(m.usersByToken, user.AuthToken)
	}
	m.mu.Unlock()
	return nil
}

// DeleteProfile removes an inactive durable profile and all of its owned data.
func (m *UserManager) DeleteProfile(ctx context.Context, loginName string) error {
	if strings.TrimSpace(loginName) == "" {
		return fmt.Errorf("deleteIdentity: %w", ErrUserNotFound)
	}
	repository, isSupported := m.repository.(UserDeletionRepository)
	if !isSupported {
		return errors.New("delete repository unavailable")
	}
	session := m.sessionLock(loginName)
	session.Lock()
	defer session.Unlock()
	m.mu.RLock()
	active := m.activeUsers[loginKey(loginName)]
	m.mu.RUnlock()
	if active != nil {
		return fmt.Errorf("deleteActive: %w", ErrUserActive)
	}
	err := repository.DeleteByLoginName(ctx, loginName)
	if err != nil {
		return fmt.Errorf("deleteProfile: %w", err)
	}
	return nil
}

func (m *UserManager) sessionLock(loginName string) *sync.Mutex {
	loginName = loginKey(loginName)
	var hash uint32 = 2166136261
	for index := 0; index < len(loginName); index++ {
		hash ^= uint32(loginName[index])
		hash *= 16777619
	}
	return &m.sessions[hash%uint32(len(m.sessions))]
}

func (m *UserManager) UserByID(id int64) *User {
	m.mu.RLock()
	user := m.activeUsersByID[id]
	m.mu.RUnlock()
	return user
}

func (m *UserManager) UserByLoginName(loginName string) *User {
	m.mu.RLock()
	user := m.activeUsers[loginKey(loginName)]
	m.mu.RUnlock()
	return user
}

// UserByDisplayName resolves an active public persona without case sensitivity.
func (m *UserManager) UserByDisplayName(displayName string) *User {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, user := range m.activeUsersByID {
		if strings.EqualFold(user.DisplayName, displayName) {
			return user
		}
	}
	return nil
}

func loginKey(loginName string) string {
	return strings.ToLower(strings.TrimSpace(loginName))
}

func (m *UserManager) UserByAuthToken(token string) *User {
	m.mu.RLock()
	user := m.usersByToken[token]
	m.mu.RUnlock()
	return user
}

func (m *UserManager) Users() []*User {
	m.mu.RLock()
	users := make([]*User, 0, len(m.activeUsers))
	for _, user := range m.activeUsers {
		users = append(users, user)
	}
	m.mu.RUnlock()
	return users
}

// PublicProfile loads a detached player-visible profile by one public key.
// An active aggregate takes precedence over its durable snapshot so accepted
// but not-yet-flushed lifetime counters remain visible.
func (m *UserManager) PublicProfile(ctx context.Context, accountID int64, displayName string) (UserView, error) {
	if accountID <= 0 && strings.TrimSpace(displayName) == "" {
		return UserView{}, errors.New("public profile key missing")
	}
	if accountID > 0 && strings.TrimSpace(displayName) != "" {
		return UserView{}, errors.New("public profile key ambiguous")
	}
	profileRepository, isAvailable := m.repository.(UserProfileRepository)
	if !isAvailable {
		return UserView{}, errors.New("public profile repository unavailable")
	}
	var record UserRecord
	var err error
	if accountID > 0 {
		record, err = profileRepository.LoadByID(ctx, accountID)
	} else {
		record, err = profileRepository.LoadByDisplayName(ctx, strings.TrimSpace(displayName))
	}
	if err != nil {
		return UserView{}, fmt.Errorf("publicProfileLoad: %w", err)
	}
	active := m.UserByLoginName(record.LoginName)
	if active != nil {
		return active.View(), nil
	}
	return NewUserFromRecord(record, m.template).View(), nil
}

// Flush persists every active user during graceful server shutdown.
func (m *UserManager) Flush(ctx context.Context) error {
	users := m.Users()
	var flushErr error
	for index, user := range users {
		session := m.sessionLock(user.LoginName)
		session.Lock()
		user.mutation.Lock()
		user.accruePlayTime(time.Now())
		err := m.repository.Save(ctx, user.Record())
		user.mutation.Unlock()
		session.Unlock()
		if err != nil {
			flushErr = errors.Join(flushErr, fmt.Errorf("userFlush[%d]: %w", index, err))
		}
	}
	return flushErr
}

func randomToken() (string, error) {
	contents := make([]byte, 24)
	_, err := rand.Read(contents)
	if err != nil {
		return "", fmt.Errorf("randomToken: %w", err)
	}
	return hex.EncodeToString(contents), nil
}

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const defaultRemoteServerPort uint16 = 42127

const remoteRefreshInterval = 48 * time.Hour

// RemoteServer is one Darkspinner instance found on the local network.
type RemoteServer struct {
	Address       string `json:"address"`
	ServerVersion string `json:"serverVersion"`
	GameVersion   string `json:"gameVersion"`
}

// RemoteProfile is locally cached metadata for one account on another server.
type RemoteProfile struct {
	ServerAddress           string `json:"serverAddress"`
	LoginName               string `json:"loginName"`
	DisplayName             string `json:"displayName"`
	CreateDT                string `json:"createDt"`
	LastConnectedDT         string `json:"lastConnectedDt"`
	ServerVersion           string `json:"serverVersion"`
	GameVersion             string `json:"gameVersion"`
	LastCheckedDT           string `json:"lastCheckedDt"`
	LastOnlineDT            string `json:"lastOnlineDt"`
	LastProfileRefreshDT    string `json:"lastProfileRefreshDt"`
	AvatarID                uint32 `json:"avatarId"`
	AvatarURL               string `json:"avatarUrl"`
	CrogenitorLevel         uint32 `json:"crogenitorLevel"`
	CumulativeXP            uint32 `json:"cumulativeXp"`
	HighestCampaignUnlocked uint32 `json:"highestCampaignUnlocked"`
	IsPasswordRemembered    bool   `json:"isPasswordRemembered"`
	IsServerOnline          bool   `json:"isServerOnline"`
}

type remoteRefreshEntry struct {
	ServerAddress string
	LoginName     string
	Password      string
	LastCheckedDT string
}

type remoteProfileResponse struct {
	LoginName               string `json:"login_name"`
	DisplayName             string `json:"display_name"`
	CreateDT                string `json:"create_dt"`
	AvatarID                uint32 `json:"avatar_id"`
	CrogenitorLevel         uint32 `json:"crogenitor_level"`
	CumulativeXP            uint32 `json:"cumulative_xp"`
	HighestCampaignUnlocked uint32 `json:"highest_campaign_unlocked"`
}

type remoteStartResponse struct {
	Code    string                `json:"code"`
	Profile remoteProfileResponse `json:"profile"`
}

// ScanRemoteServers probes the configured endpoint and private IPv4 interfaces
// for Darkspin readiness endpoints.
func (a *App) ScanRemoteServers(configuredAddress string) ([]RemoteServer, error) {
	configuredAddress = strings.TrimSpace(configuredAddress)
	configuredPort := defaultRemoteServerPort
	if configuredAddress != "" {
		normalizedAddress, err := normalizeRemoteServerAddress(configuredAddress)
		if err != nil {
			return nil, fmt.Errorf("serverValidate: %w", err)
		}
		_, port, err := net.SplitHostPort(normalizedAddress)
		if err != nil {
			return nil, fmt.Errorf("serverParse: %w", err)
		}
		parsedPort, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("portParse: %w", err)
		}
		configuredAddress = normalizedAddress
		configuredPort = uint16(parsedPort)
	}
	addresses := remoteScanAddresses(configuredAddress, configuredPort)
	ctx := a.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	scanCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 450 * time.Millisecond}
	servers := make([]RemoteServer, 0)
	var mu sync.Mutex
	var waitGroup sync.WaitGroup
	limit := make(chan struct{}, 48)
	for _, address := range addresses {
		address := address
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			select {
			case limit <- struct{}{}:
			case <-scanCtx.Done():
				return
			}
			defer func() { <-limit }()
			server, err := probeRemoteServer(scanCtx, client, address)
			if err != nil {
				return
			}
			mu.Lock()
			servers = append(servers, server)
			mu.Unlock()
		}()
	}
	waitGroup.Wait()
	sort.Slice(servers, func(first, second int) bool {
		return servers[first].Address < servers[second].Address
	})
	return servers, nil
}

func remoteScanAddresses(configuredAddress string, configuredPort uint16) []string {
	addressesByHost := make(map[string]struct{})
	localHosts := map[string]struct{}{
		"127.0.0.1": {},
		"localhost": {},
	}
	localHostname, err := os.Hostname()
	if err == nil {
		localHosts[strings.ToLower(strings.TrimSuffix(localHostname, "."))] = struct{}{}
	}
	interfaceAddresses, err := net.InterfaceAddrs()
	privateIPs := make([]net.IP, 0)
	if err == nil {
		for _, interfaceAddress := range interfaceAddresses {
			network, isNetwork := interfaceAddress.(*net.IPNet)
			if !isNetwork {
				continue
			}
			ip := network.IP.To4()
			if ip == nil || ip.IsLoopback() || !ip.IsPrivate() {
				continue
			}
			localHosts[ip.String()] = struct{}{}
			privateIPs = append(privateIPs, ip)
		}
	}
	for _, ip := range privateIPs {
		for host := 1; host < 255; host++ {
			candidate := net.IPv4(ip[0], ip[1], ip[2], byte(host)).String()
			if _, isLocal := localHosts[candidate]; isLocal {
				continue
			}
			addressesByHost[net.JoinHostPort(candidate, strconv.Itoa(int(configuredPort)))] = struct{}{}
		}
	}
	if configuredAddress != "" && !isLocalRemoteAddress(configuredAddress, localHosts) {
		addressesByHost[configuredAddress] = struct{}{}
	}
	addresses := make([]string, 0, len(addressesByHost))
	for address := range addressesByHost {
		addresses = append(addresses, address)
	}
	return addresses
}

func isLocalRemoteAddress(address string, localHosts map[string]struct{}) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if _, isLocal := localHosts[host]; isLocal {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func probeRemoteServer(ctx context.Context, client *http.Client, address string) (RemoteServer, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/recap/server", nil)
	if err != nil {
		return RemoteServer{}, fmt.Errorf("probeRequest: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return RemoteServer{}, fmt.Errorf("probeSend: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return RemoteServer{}, fmt.Errorf("probeStatus: %s", response.Status)
	}
	payload := struct {
		ServerVersion string `json:"server_version"`
		GameVersion   string `json:"game_version"`
	}{}
	err = json.NewDecoder(response.Body).Decode(&payload)
	if err != nil {
		return RemoteServer{}, fmt.Errorf("probeDecode: %w", err)
	}
	return RemoteServer{
		Address: address, ServerVersion: payload.ServerVersion,
		GameVersion: payload.GameVersion,
	}, nil
}

// GetRemoteProfiles lists accounts previously used from this launcher.
func (a *App) GetRemoteProfiles() ([]RemoteProfile, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.TODO()
	}
	database, err := a.openRemoteDatabase(ctx)
	if err != nil {
		return nil, fmt.Errorf("remoteOpen: %w", err)
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, `
		SELECT server_address, login_name, display_name, password, avatar_id,
			crogenitor_level, cumulative_xp, highest_campaign_unlocked, create_dt,
			last_connected_dt, server_version, game_version, last_checked_dt,
			last_online_dt, last_profile_refresh_dt, is_server_online
		FROM remote_profile ORDER BY server_address, display_name`)
	if err != nil {
		return nil, fmt.Errorf("remoteQuery: %w", err)
	}
	defer rows.Close()
	profiles := make([]RemoteProfile, 0)
	for rows.Next() {
		profile := RemoteProfile{}
		password := ""
		err = rows.Scan(
			&profile.ServerAddress, &profile.LoginName, &profile.DisplayName, &password,
			&profile.AvatarID, &profile.CrogenitorLevel, &profile.CumulativeXP,
			&profile.HighestCampaignUnlocked, &profile.CreateDT,
			&profile.LastConnectedDT, &profile.ServerVersion, &profile.GameVersion,
			&profile.LastCheckedDT, &profile.LastOnlineDT, &profile.LastProfileRefreshDT,
			&profile.IsServerOnline,
		)
		if err != nil {
			return nil, fmt.Errorf("remoteScan: %w", err)
		}
		profile.IsPasswordRemembered = password != ""
		profile.AvatarURL, _ = a.profileAvatarURL(profile.AvatarID)
		profiles = append(profiles, profile)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("remoteRows: %w", err)
	}
	return profiles, nil
}

// RefreshRemoteProfiles refreshes cached servers that have not been checked in
// the last two days, then returns the current cache without creating a launch grant.
func (a *App) RefreshRemoteProfiles() ([]RemoteProfile, error) {
	a.remoteRefreshMu.Lock()
	defer a.remoteRefreshMu.Unlock()
	ctx := a.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	err := a.refreshRemoteCache(ctx, "", "", "", false)
	if err != nil {
		return nil, fmt.Errorf("remoteRefresh: %w", err)
	}
	return a.GetRemoteProfiles()
}

// RegisterRemoteProfile signs into an existing matching account or creates a
// new account through a LAN server, then caches its connection metadata.
func (a *App) RegisterRemoteProfile(
	serverAddress, identity, password string, avatarID uint32, isPasswordRemembered bool,
	isTutorialSkipped bool,
) error {
	serverAddress, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return fmt.Errorf("serverValidate: %w", err)
	}
	err = validateNewProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("identityValidate: %w", err)
	}
	if password == "" {
		return errors.New("password is required for remote registration")
	}
	response := struct {
		Profile remoteProfileResponse `json:"profile"`
	}{}
	ctx := a.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	availabilityCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	remoteServer, err := probeRemoteServer(
		availabilityCtx, &http.Client{Timeout: 2 * time.Second}, serverAddress,
	)
	cancel()
	if err != nil {
		return fmt.Errorf("remoteUnavailable[%s]: %w", serverAddress, err)
	}
	if remoteServer.Address == "" {
		return fmt.Errorf("remote server %s returned an empty address", serverAddress)
	}
	_, existingProfile, resolvedPassword, loginErr := a.remoteCredential(
		serverAddress, identity, password,
	)
	if loginErr == nil {
		err = a.saveRemoteProfile(
			serverAddress, existingProfile, resolvedPassword, isPasswordRemembered,
		)
		if err != nil {
			return fmt.Errorf("existingSave: %w", err)
		}
		return nil
	}
	// A failed sign-in is expected for a new account. Registration below
	// returns the server's specific conflict or validation result when needed.
	err = validateProfileAvatarID(avatarID)
	if err != nil {
		return fmt.Errorf("avatarValidate: %w", err)
	}
	err = postAuthJSON(ctx, &http.Client{Timeout: 15 * time.Second},
		"http://"+serverAddress+"/api/desktop/register", map[string]any{
			"account": identity, "display_name": identity, "password": password,
			"avatar_id": avatarID, "is_tutorial_skipped": isTutorialSkipped,
		}, &response)
	if err != nil {
		return fmt.Errorf("remoteRegister: %w", err)
	}
	err = a.saveRemoteProfile(serverAddress, response.Profile, password, isPasswordRemembered)
	if err != nil {
		return fmt.Errorf("remoteSave: %w", err)
	}
	return nil
}

// LoginRemoteProfile verifies a remote account and refreshes cached metadata.
func (a *App) LoginRemoteProfile(
	serverAddress, identity, password string, isPasswordRemembered bool,
) error {
	_, profile, resolvedPassword, err := a.remoteCredential(serverAddress, identity, password)
	if err != nil {
		return fmt.Errorf("remoteCredential: %w", err)
	}
	serverAddress, _ = normalizeRemoteServerAddress(serverAddress)
	err = a.saveRemoteProfile(serverAddress, profile, resolvedPassword, isPasswordRemembered)
	if err != nil {
		return fmt.Errorf("remoteSave: %w", err)
	}
	return nil
}

// DeleteRemoteProfile authenticates and permanently removes an account from
// its host server before deleting the launcher's cached connection metadata.
func (a *App) DeleteRemoteProfile(serverAddress, identity, password string) error {
	serverAddress, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return fmt.Errorf("serverValidate: %w", err)
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return errors.New("identity is required")
	}
	if password == "" {
		password, err = a.loadRemotePassword(serverAddress, identity)
		if err != nil {
			return fmt.Errorf("passwordLoad: %w", err)
		}
	}
	if password == "" {
		return errors.New("password is required to delete a remote Crogenitor")
	}
	response := struct {
		IsDeleted bool `json:"deleted"`
	}{}
	ctx := a.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	err = postAuthJSON(ctx, &http.Client{Timeout: 15 * time.Second},
		"http://"+serverAddress+"/api/desktop/delete",
		map[string]string{"account": identity, "password": password}, &response)
	if err != nil {
		return fmt.Errorf("remoteDelete: %w", err)
	}
	if !response.IsDeleted {
		return errors.New("remote server did not confirm account deletion")
	}
	err = a.deleteCachedRemoteProfile(ctx, serverAddress, identity)
	if err != nil {
		return fmt.Errorf("cacheDelete: %w", err)
	}
	return nil
}

// LaunchRemoteProfile authenticates and starts the game against the selected
// LAN server without changing the local profile or server flow.
func (a *App) LaunchRemoteProfile(
	serverAddress, identity, password string, isPasswordRemembered bool,
) error {
	a.mu.Lock()
	if a.cancel != nil || a.gameDone != nil {
		a.mu.Unlock()
		return errors.New("another launcher or game operation is active")
	}
	if !a.status.IsPatchComplete || !a.status.IsGameReady {
		a.mu.Unlock()
		return errors.New("game verification must finish before remote launch")
	}
	a.mu.Unlock()
	token, profile, resolvedPassword, err := a.remoteCredential(serverAddress, identity, password)
	if err != nil {
		return fmt.Errorf("remoteCredential: %w", err)
	}
	serverAddress, _ = normalizeRemoteServerAddress(serverAddress)
	err = a.saveRemoteProfile(serverAddress, profile, resolvedPassword, isPasswordRemembered)
	if err != nil {
		return fmt.Errorf("remoteSave: %w", err)
	}
	host, _, err := net.SplitHostPort(serverAddress)
	if err != nil {
		return fmt.Errorf("remoteHost: %w", err)
	}
	clientProfile := identity + "@" + host
	a.mu.Lock()
	ctx, gameDone, gameCancel := a.beginGameLaunchLocked()
	a.status.State = "launching"
	a.status.Message = "Launching remote Crogenitor"
	arguments := append([]string(nil), a.arguments...)
	a.emitStatusLocked()
	a.mu.Unlock()
	go a.runRemoteGameLaunch(ctx, gameLaunchRequest{
		account: identity, clientProfile: clientProfile, serverAddress: serverAddress,
		token: token, arguments: arguments, done: gameDone, cancel: gameCancel,
	}, resolvedPassword)
	return nil
}

func (a *App) runRemoteGameLaunch(ctx context.Context, req gameLaunchRequest, password string) {
	a.runGameLaunch(ctx, req)
	refreshCtx := a.lifecycleCtx
	if refreshCtx == nil {
		refreshCtx = context.TODO()
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(refreshCtx), 10*time.Second)
	defer cancel()
	a.remoteRefreshMu.Lock()
	err := a.refreshRemoteCache(refreshCtx, req.serverAddress, req.account, password, true)
	a.remoteRefreshMu.Unlock()
	if err != nil {
		a.log(fmt.Sprintf("Remote profile refresh after game exit failed: %v", err))
	}
}

func (a *App) remoteCredential(
	serverAddress, identity, password string,
) (string, remoteProfileResponse, string, error) {
	serverAddress, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return "", remoteProfileResponse{}, "", fmt.Errorf("serverValidate: %w", err)
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return "", remoteProfileResponse{}, "", errors.New("identity is required")
	}
	if password == "" {
		password, err = a.loadRemotePassword(serverAddress, identity)
		if err != nil {
			return "", remoteProfileResponse{}, "", fmt.Errorf("passwordLoad: %w", err)
		}
	}
	if password == "" {
		return "", remoteProfileResponse{}, "", errors.New("password is required")
	}
	start := remoteStartResponse{}
	client := &http.Client{Timeout: 15 * time.Second}
	ctx := a.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	err = postAuthJSON(ctx, client, "http://"+serverAddress+"/api/desktop/start",
		map[string]string{"account": identity, "password": password}, &start)
	if err != nil {
		return "", remoteProfileResponse{}, "", fmt.Errorf("remoteLogin: %w", err)
	}
	exchange := struct {
		LaunchToken string `json:"launch_token"`
	}{}
	err = postAuthJSON(ctx, client, "http://"+serverAddress+"/api/desktop/exchange",
		map[string]string{"code": start.Code}, &exchange)
	if err != nil {
		return "", remoteProfileResponse{}, "", fmt.Errorf("remoteExchange: %w", err)
	}
	token, err := normalizeJWT(exchange.LaunchToken)
	if err != nil {
		return "", remoteProfileResponse{}, "", fmt.Errorf("remoteToken: %w", err)
	}
	return token, start.Profile, password, nil
}

func normalizeRemoteServerAddress(serverAddress string) (string, error) {
	serverAddress = strings.TrimSpace(serverAddress)
	if serverAddress == "" {
		return "", errors.New("server address is required")
	}
	if strings.Contains(serverAddress, "://") || strings.ContainsAny(serverAddress, "/?#\\") {
		return "", errors.New("server address must be a hostname or IPv4 address, optionally followed by a port")
	}
	if !strings.Contains(serverAddress, ":") {
		serverAddress = net.JoinHostPort(serverAddress, strconv.Itoa(int(defaultRemoteServerPort)))
	}
	host, port, err := net.SplitHostPort(serverAddress)
	if err != nil {
		return "", fmt.Errorf("addressParse: %w", err)
	}
	host, err = normalizeRemoteHost(host)
	if err != nil {
		return "", fmt.Errorf("hostValidate: %w", err)
	}
	if port == "" {
		return "", errors.New("server port is required")
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", errors.New("server port must be between 1 and 65535")
	}
	return net.JoinHostPort(host, strconv.FormatUint(parsedPort, 10)), nil
}

func normalizeRemoteHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return "", errors.New("server host is required")
	}
	ip := net.ParseIP(host)
	if ip != nil {
		ip = ip.To4()
		if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
			return "", errors.New("server IP must be a private IPv4 address")
		}
		return ip.String(), nil
	}
	if len(host) > 253 {
		return "", errors.New("server hostname is too long")
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("server hostname is invalid")
		}
		for _, character := range label {
			isLetter := character >= 'a' && character <= 'z'
			isDigit := character >= '0' && character <= '9'
			if !isLetter && !isDigit && character != '-' {
				return "", errors.New("server hostname is invalid")
			}
		}
	}
	return host, nil
}

func (a *App) openRemoteDatabase(ctx context.Context) (*sql.DB, error) {
	basePath, err := executableDirectory()
	if err != nil {
		return nil, fmt.Errorf("baseResolve: %w", err)
	}
	pathSet, err := resolveSpinnerPaths(basePath)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(pathSet.remotePath), 0o755)
	if err != nil {
		return nil, fmt.Errorf("directoryCreate: %w", err)
	}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(ON)")
	databaseDSN := "file:" + filepath.ToSlash(pathSet.remotePath) + "?" + query.Encode()
	database, err := sql.Open("sqlite", databaseDSN)
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	err = database.PingContext(ctx)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("databasePing: %w", err)
	}
	err = os.Chmod(pathSet.remotePath, 0o600)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("databaseProtect: %w", err)
	}
	_, err = database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS remote_profile (
		server_address TEXT NOT NULL,
		login_name TEXT NOT NULL,
		display_name TEXT NOT NULL,
		password TEXT NOT NULL DEFAULT '',
		avatar_id INTEGER NOT NULL DEFAULT 0,
		crogenitor_level INTEGER NOT NULL DEFAULT 0,
		cumulative_xp INTEGER NOT NULL DEFAULT 0,
		highest_campaign_unlocked INTEGER NOT NULL DEFAULT 1,
		create_dt TEXT NOT NULL DEFAULT '',
		last_connected_dt TEXT NOT NULL DEFAULT '',
		server_version TEXT NOT NULL DEFAULT '',
		game_version TEXT NOT NULL DEFAULT '',
		last_checked_dt TEXT NOT NULL DEFAULT '',
		last_online_dt TEXT NOT NULL DEFAULT '',
		last_profile_refresh_dt TEXT NOT NULL DEFAULT '',
		is_server_online INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (server_address, login_name)
	)`)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("schemaCreate: %w", err)
	}
	createDTColumnCount := 0
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pragma_table_info('remote_profile')
		WHERE name = 'create_dt'`).Scan(&createDTColumnCount)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("schemaColumnCheck: %w", err)
	}
	if createDTColumnCount == 0 {
		_, err = database.ExecContext(ctx, `
			ALTER TABLE remote_profile
			ADD COLUMN create_dt TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("schemaColumnAdd: %w", err)
		}
	}
	migrationDT := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.ExecContext(ctx, `
		UPDATE remote_profile
		SET create_dt = ?
		WHERE create_dt = ''`, migrationDT)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("schemaCreateDT: %w", err)
	}
	lastConnectedDTColumnCount := 0
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pragma_table_info('remote_profile')
		WHERE name = 'last_connected_dt'`).Scan(&lastConnectedDTColumnCount)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("schemaLastConnectedCheck: %w", err)
	}
	if lastConnectedDTColumnCount == 0 {
		_, err = database.ExecContext(ctx, `
			ALTER TABLE remote_profile
			ADD COLUMN last_connected_dt TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("schemaLastConnectedAdd: %w", err)
		}
	}
	_, err = database.ExecContext(ctx, `
		UPDATE remote_profile
		SET last_connected_dt = ?
		WHERE last_connected_dt = ''`, migrationDT)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("schemaLastConnectedDT: %w", err)
	}
	cacheColumns := []struct {
		name       string
		definition string
	}{
		{name: "server_version", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "game_version", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "last_checked_dt", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "last_online_dt", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "last_profile_refresh_dt", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "is_server_online", definition: "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, cacheColumn := range cacheColumns {
		columnCount := 0
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM pragma_table_info('remote_profile')
			WHERE name = ?`, cacheColumn.name).Scan(&columnCount)
		if err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("schemaCacheCheck[%s]: %w", cacheColumn.name, err)
		}
		if columnCount != 0 {
			continue
		}
		statement := "ALTER TABLE remote_profile ADD COLUMN " + cacheColumn.name + " " + cacheColumn.definition
		_, err = database.ExecContext(ctx, statement)
		if err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("schemaCacheAdd[%s]: %w", cacheColumn.name, err)
		}
	}
	return database, nil
}

func (a *App) saveRemoteProfile(
	serverAddress string, profile remoteProfileResponse, password string, isPasswordRemembered bool,
) error {
	if !isPasswordRemembered {
		password = ""
	}
	createDT := strings.TrimSpace(profile.CreateDT)
	if createDT == "" {
		createDT = time.Now().UTC().Format(time.RFC3339Nano)
	}
	lastConnectedDT := time.Now().UTC().Format(time.RFC3339Nano)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.TODO()
	}
	database, err := a.openRemoteDatabase(ctx)
	if err != nil {
		return fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	_, err = database.ExecContext(ctx, `INSERT INTO remote_profile (
		server_address, login_name, display_name, password, avatar_id,
		crogenitor_level, cumulative_xp, highest_campaign_unlocked, create_dt,
		last_connected_dt, last_online_dt, last_profile_refresh_dt, is_server_online
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	ON CONFLICT(server_address, login_name) DO UPDATE SET
		display_name = excluded.display_name,
		password = excluded.password,
		avatar_id = excluded.avatar_id,
		crogenitor_level = excluded.crogenitor_level,
		cumulative_xp = excluded.cumulative_xp,
		highest_campaign_unlocked = excluded.highest_campaign_unlocked,
		create_dt = excluded.create_dt,
		last_connected_dt = excluded.last_connected_dt,
		last_online_dt = excluded.last_connected_dt,
		last_profile_refresh_dt = excluded.last_connected_dt,
		is_server_online = 1`,
		serverAddress, profile.LoginName, profile.DisplayName, password, profile.AvatarID,
		profile.CrogenitorLevel, profile.CumulativeXP, profile.HighestCampaignUnlocked,
		createDT, lastConnectedDT, lastConnectedDT, lastConnectedDT,
	)
	if err != nil {
		return fmt.Errorf("profileUpsert: %w", err)
	}
	return nil
}

func (a *App) refreshRemoteCache(
	ctx context.Context, targetServerAddress, targetIdentity, targetPassword string, isForced bool,
) error {
	database, err := a.openRemoteDatabase(ctx)
	if err != nil {
		return fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	query := `SELECT server_address, login_name, password, last_checked_dt FROM remote_profile`
	queryArgs := make([]any, 0)
	if targetServerAddress != "" && targetIdentity != "" {
		query += ` WHERE server_address = ? AND login_name = ?`
		queryArgs = append(queryArgs, targetServerAddress, targetIdentity)
	}
	rows, err := database.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return fmt.Errorf("entryQuery: %w", err)
	}
	entries := make([]remoteRefreshEntry, 0)
	for rows.Next() {
		entry := remoteRefreshEntry{}
		err = rows.Scan(&entry.ServerAddress, &entry.LoginName, &entry.Password, &entry.LastCheckedDT)
		if err != nil {
			closeErr := rows.Close()
			if closeErr != nil {
				return fmt.Errorf("entryScanClose: %v: %w", closeErr, err)
			}
			return fmt.Errorf("entryScan: %w", err)
		}
		if targetPassword != "" {
			entry.Password = targetPassword
		}
		if !isForced && !isRemoteRefreshDue(entry.LastCheckedDT, time.Now()) {
			continue
		}
		entries = append(entries, entry)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return fmt.Errorf("entryRows: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("entryClose: %w", closeErr)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	serversByAddress := make(map[string]RemoteServer)
	offlineServers := make(map[string]bool)
	for _, entry := range entries {
		server, isCached := serversByAddress[entry.ServerAddress]
		isOffline := offlineServers[entry.ServerAddress]
		checkedDT := time.Now().UTC().Format(time.RFC3339Nano)
		if !isCached && !isOffline {
			server, err = probeRemoteServer(ctx, client, entry.ServerAddress)
			if err != nil {
				offlineServers[entry.ServerAddress] = true
				updateResult, updateErr := database.ExecContext(ctx, `
					UPDATE remote_profile SET last_checked_dt = ?, is_server_online = 0
					WHERE server_address = ?`, checkedDT, entry.ServerAddress)
				if updateErr != nil {
					return fmt.Errorf("offlineUpdate: %w", updateErr)
				}
				updatedCount, updateErr := updateResult.RowsAffected()
				if updateErr != nil {
					return fmt.Errorf("offlineCount: %w", updateErr)
				}
				if updatedCount < 1 {
					return errors.New("offlineCount: no cached remote profiles updated")
				}
				continue
			}
			serversByAddress[entry.ServerAddress] = server
			onlineResult, updateErr := database.ExecContext(ctx, `
				UPDATE remote_profile SET server_version = ?, game_version = ?,
					last_checked_dt = ?, last_online_dt = ?, is_server_online = 1
				WHERE server_address = ?`, server.ServerVersion, server.GameVersion,
				checkedDT, checkedDT, entry.ServerAddress)
			if updateErr != nil {
				return fmt.Errorf("onlineUpdate: %w", updateErr)
			}
			updatedCount, updateErr := onlineResult.RowsAffected()
			if updateErr != nil {
				return fmt.Errorf("onlineCount: %w", updateErr)
			}
			if updatedCount < 1 {
				return errors.New("onlineCount: no cached remote profiles updated")
			}
		}
		if isOffline || entry.Password == "" {
			continue
		}
		profile, err := fetchRemoteProfile(ctx, client, entry.ServerAddress, entry.LoginName, entry.Password)
		if err != nil {
			continue
		}
		profileRefreshDT := time.Now().UTC().Format(time.RFC3339Nano)
		profileResult, updateErr := database.ExecContext(ctx, `
			UPDATE remote_profile SET display_name = ?, avatar_id = ?, crogenitor_level = ?,
				cumulative_xp = ?, highest_campaign_unlocked = ?, create_dt = ?,
				last_profile_refresh_dt = ?
			WHERE server_address = ? AND login_name = ?`, profile.DisplayName, profile.AvatarID,
			profile.CrogenitorLevel, profile.CumulativeXP, profile.HighestCampaignUnlocked,
			profile.CreateDT, profileRefreshDT, entry.ServerAddress, entry.LoginName)
		if updateErr != nil {
			return fmt.Errorf("profileUpdate: %w", updateErr)
		}
		updatedCount, updateErr := profileResult.RowsAffected()
		if updateErr != nil {
			return fmt.Errorf("profileCount: %w", updateErr)
		}
		if updatedCount != 1 {
			return fmt.Errorf("profileCount: updated %d cached profiles", updatedCount)
		}
	}
	return nil
}

func isRemoteRefreshDue(lastCheckedDT string, now time.Time) bool {
	lastCheckedDT = strings.TrimSpace(lastCheckedDT)
	if lastCheckedDT == "" {
		return true
	}
	checkedDT, err := time.Parse(time.RFC3339Nano, lastCheckedDT)
	if err != nil {
		return true
	}
	return !now.Before(checkedDT.Add(remoteRefreshInterval))
}

func fetchRemoteProfile(
	ctx context.Context, client *http.Client, serverAddress, identity, password string,
) (remoteProfileResponse, error) {
	response := struct {
		Profile remoteProfileResponse `json:"profile"`
	}{}
	err := postAuthJSON(ctx, client, "http://"+serverAddress+"/api/desktop/profile",
		map[string]string{"account": identity, "password": password}, &response)
	if err != nil {
		return remoteProfileResponse{}, fmt.Errorf("profileFetch: %w", err)
	}
	return response.Profile, nil
}

func (a *App) loadRemotePassword(serverAddress, identity string) (string, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.TODO()
	}
	database, err := a.openRemoteDatabase(ctx)
	if err != nil {
		return "", fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	password := ""
	err = database.QueryRowContext(ctx, `
		SELECT password FROM remote_profile
		WHERE server_address = ? AND login_name = ?`, serverAddress, identity,
	).Scan(&password)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("passwordQuery: %w", err)
	}
	return password, nil
}

func (a *App) deleteCachedRemoteProfile(ctx context.Context, serverAddress, identity string) error {
	database, err := a.openRemoteDatabase(ctx)
	if err != nil {
		return fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	result, err := database.ExecContext(ctx, `
		DELETE FROM remote_profile WHERE server_address = ? AND login_name = ?`,
		serverAddress, identity,
	)
	if err != nil {
		return fmt.Errorf("profileDelete: %w", err)
	}
	deletedCount, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleteCount: %w", err)
	}
	if deletedCount > 1 {
		return fmt.Errorf("deleteCount: removed %d cached profiles", deletedCount)
	}
	return nil
}

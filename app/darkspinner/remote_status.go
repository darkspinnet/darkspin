package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/buildinfo"
)

// RemoteServerStatus is a live unauthenticated endpoint check, separate from
// the two-day profile metadata cache.
type RemoteServerStatus struct {
	Address          string `json:"address"`
	IsOnline         bool   `json:"isOnline"`
	ServerVersion    string `json:"serverVersion"`
	ClientVersion    string `json:"clientVersion"`
	IsVersionMatched bool   `json:"isVersionMatched"`
	Error            string `json:"error"`
	CheckedDT        string `json:"checkedDt"`
}

// CheckRemoteServers probes each normalized address once without credentials.
func (e *App) CheckRemoteServers(addresses []string) ([]RemoteServerStatus, error) {
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	uniqueAddresses := make([]string, 0, len(addresses))
	seenAddresses := make(map[string]bool)
	for _, address := range addresses {
		normalizedAddress, err := normalizeRemoteServerAddress(address)
		if err != nil {
			normalizedAddress = address
		}
		if seenAddresses[normalizedAddress] {
			continue
		}
		seenAddresses[normalizedAddress] = true
		uniqueAddresses = append(uniqueAddresses, normalizedAddress)
	}
	checks := make([]RemoteServerStatus, len(uniqueAddresses))
	client := remoteHTTPClient(2 * time.Second)
	defer client.CloseIdleConnections()
	jobs := make(chan int, len(uniqueAddresses))
	for index := range uniqueAddresses {
		jobs <- index
	}
	close(jobs)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < min(8, len(uniqueAddresses)); worker++ {
		waitGroup.Add(1)
		go checkRemoteWorker(ctx, client, uniqueAddresses, checks, jobs, &waitGroup)
	}
	waitGroup.Wait()
	// Preserve each caller's address identity while probing canonical endpoints
	// once, so aliases and leading-zero ports remain associated with their row.
	checksByAddress := make(map[string]RemoteServerStatus, len(checks))
	for _, check := range checks {
		checksByAddress[check.Address] = check
	}
	results := make([]RemoteServerStatus, len(addresses))
	for index, address := range addresses {
		normalizedAddress, err := normalizeRemoteServerAddress(address)
		if err != nil {
			normalizedAddress = address
		}
		results[index] = checksByAddress[normalizedAddress]
		results[index].Address = address
	}
	return results, nil
}

func checkRemoteWorker(
	ctx context.Context, client *http.Client, addresses []string,
	checks []RemoteServerStatus, jobs <-chan int, waitGroup *sync.WaitGroup,
) {
	defer waitGroup.Done()
	for index := range jobs {
		checks[index] = checkRemoteServer(ctx, client, addresses[index])
	}
}

func checkRemoteServer(ctx context.Context, client *http.Client, address string) RemoteServerStatus {
	status := RemoteServerStatus{Address: address, ClientVersion: Version}
	normalizedAddress, err := normalizeRemoteServerAddress(address)
	if err != nil {
		status.CheckedDT = time.Now().UTC().Format(time.RFC3339Nano)
		status.Error = err.Error()
		return status
	}
	status.Address = normalizedAddress
	server, err := probeRemoteServer(ctx, client, normalizedAddress)
	status.CheckedDT = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		status.Error = fmt.Sprintf("Server unavailable: %v", err)
		return status
	}
	status.IsOnline = true
	status.ServerVersion = server.ServerVersion
	err = buildinfo.CheckCompatibility(Version, server.ServerVersion)
	status.IsVersionMatched = err == nil
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

func requireRemoteCompatibility(ctx context.Context, address string) error {
	client := remoteHTTPClient(3 * time.Second)
	defer client.CloseIdleConnections()
	status := checkRemoteServer(ctx, client, address)
	if !status.IsOnline || !status.IsVersionMatched {
		return fmt.Errorf("remote server %s: %s", address, status.Error)
	}
	return nil
}

func remoteHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout, Transport: &http.Transport{},
		CheckRedirect: rejectRemoteRedirect,
	}
}

func rejectRemoteRedirect(req *http.Request, requests []*http.Request) error {
	return http.ErrUseLastResponse
}

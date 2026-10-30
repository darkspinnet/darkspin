package main

import "sync"

type clientSessionResult struct {
	Process  clientProcessRecord  `json:"process"`
	Failure  *clientFailureRecord `json:"failure,omitempty"`
	IsClosed bool                 `json:"is_closed"`
}

// Only launches issued by this App belong to its session. Native registration
// and failure files are consumed as a temporary mailbox, never as a registry.
type clientSession struct {
	mutex             sync.Mutex
	clientsByLaunchID map[string]clientSessionResult
}

func (e *clientSession) Track(launchID, profile, mode string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.clientsByLaunchID == nil {
		e.clientsByLaunchID = make(map[string]clientSessionResult)
	}
	e.clientsByLaunchID[launchID] = clientSessionResult{Process: clientProcessRecord{LaunchID: launchID, Profile: profile, Mode: mode}}
}

func (e *clientSession) Owns(launchID string) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	client, isOwned := e.clientsByLaunchID[launchID]
	return isOwned && client.Process.LaunchID == launchID
}

func (e *clientSession) Register(record clientProcessRecord) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	client, isOwned := e.clientsByLaunchID[record.LaunchID]
	if !isOwned || client.IsClosed {
		return false
	}
	client.Process = record
	e.clientsByLaunchID[record.LaunchID] = client
	return true
}

func (e *clientSession) Close(launchID string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	client, isOwned := e.clientsByLaunchID[launchID]
	if !isOwned {
		return
	}
	client.IsClosed = true
	e.clientsByLaunchID[launchID] = client
}

func (e *clientSession) Fail(record clientFailureRecord) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	client, isOwned := e.clientsByLaunchID[record.LaunchID]
	if !isOwned || client.Failure != nil {
		return false
	}
	if client.Process.PID != 0 && client.Process.PID != record.PID {
		return false
	}
	client.Failure = &record
	if client.Process.PID == 0 {
		client.Process.PID = record.PID
	}
	client.IsClosed = true
	e.clientsByLaunchID[record.LaunchID] = client
	return true
}

func (e *clientSession) IsFailed(launchID string) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	client, isOwned := e.clientsByLaunchID[launchID]
	return isOwned && client.Failure != nil
}

func (e *clientSession) Results() []clientSessionResult {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	results := make([]clientSessionResult, 0, len(e.clientsByLaunchID))
	for _, client := range e.clientsByLaunchID {
		if client.Failure != nil {
			failure := *client.Failure
			client.Failure = &failure
		}
		results = append(results, client)
	}
	return results
}

func (e *clientSession) RunningRecords() []clientProcessRecord {
	results := e.Results()
	records := make([]clientProcessRecord, 0, len(results))
	for _, client := range results {
		if !client.IsClosed && client.Process.PID != 0 && isProcessIDRunning(client.Process.PID) {
			records = append(records, client.Process)
		}
	}
	return records
}

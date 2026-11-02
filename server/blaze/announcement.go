package blaze

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type announcementJob struct {
	server *Server
	userID int64
}
type announcementResult struct {
	userID        int64
	deliveryCount int
	err           error
}

// BroadcastSystemChat sends the existing build-103 system-chat notification to
// authenticated sessions on all listeners and returns distinct delivered accounts.
// Attempts share a 45-second deadline and at most 16 concurrent writers.
func BroadcastSystemChat(ctx context.Context, servers []*Server, body string) (int, error) {
	if ctx == nil || len(servers) == 0 || strings.TrimSpace(body) == "" {
		return 0, errors.New("system announcement unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	err := ctx.Err()
	if err != nil {
		return 0, fmt.Errorf("announceStart: %w", err)
	}
	seenServers := make(map[*Server]bool)
	var jobs []announcementJob
	for _, server := range servers {
		if server == nil || seenServers[server] {
			continue
		}
		seenServers[server] = true
		server.mu.Lock()
		for userID := range server.sessionsByUser {
			jobs = append(jobs, announcementJob{server: server, userID: userID})
		}
		server.mu.Unlock()
	}
	if len(seenServers) == 0 {
		return 0, errors.New("system announcement listeners unavailable")
	}
	queuedJobs := make(chan announcementJob, len(jobs))
	results := make(chan announcementResult, len(jobs))
	for _, job := range jobs {
		queuedJobs <- job
	}
	close(queuedJobs)
	workerCount := min(16, len(jobs))
	for index := 0; index < workerCount; index++ {
		go broadcastWorker(ctx, queuedJobs, results, body)
	}
	deliveredUserIDs := make(map[int64]bool)
	var notifyErrors []error
	for index := 0; index < len(jobs); index++ {
		result := <-results
		if result.deliveryCount > 0 {
			deliveredUserIDs[result.userID] = true
		}
		if result.err != nil {
			notifyErrors = append(notifyErrors, fmt.Errorf("announceUser[%d]: %w", index, result.err))
		}
	}
	err = ctx.Err()
	if err != nil {
		notifyErrors = append(notifyErrors, fmt.Errorf("announceDeadline: %w", err))
	}
	if len(notifyErrors) != 0 {
		return len(deliveredUserIDs), fmt.Errorf("announceSend: %w", errors.Join(notifyErrors...))
	}
	return len(deliveredUserIDs), nil
}

func broadcastWorker(ctx context.Context, jobs <-chan announcementJob, results chan<- announcementResult, body string) {
	for job := range jobs {
		result := announcementResult{userID: job.userID}
		notification := systemChatNotification(job.userID, 0, body)
		for _, session := range job.server.sessionsForUser(job.userID) {
			err := ctx.Err()
			if err != nil {
				result.err = fmt.Errorf("announceContext: %w", err)
				break
			}
			err = session.notifyWithContext(ctx, MessagingComponentID, 0x01, notification)
			if err != nil {
				result.err = errors.Join(result.err, fmt.Errorf("sessionNotify: %w", err))
				continue
			}
			result.deliveryCount++
		}
		results <- result
	}
}

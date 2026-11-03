package pvp

import "time"

// QueueObservation describes current human queue members, grouped by queue
// shape. WaitSeconds is the sum of their current ages, not completed waits.
type QueueObservation struct {
	Criteria           MatchmakingCriteria
	QueuedCount        int64
	WaitSeconds        float64
	MaximumWaitSeconds float64
}

// ObserveQueues captures the five supported queues without changing their
// reservations. Campaign difficulties share one observation.
func (e *Matchmaking) ObserveQueues(at time.Time) []QueueObservation {
	if e == nil {
		return nil
	}
	observations := []QueueObservation{
		{Criteria: MatchmakingCriteria{GameType: CampaignGameType,
			ExpectedPlayerCount: CampaignExpectedPlayerCount, TeamSize: CampaignTeamSize}},
		{Criteria: MatchmakingCriteria{GameType: ArenaGameType,
			ExpectedPlayerCount: ArenaOneVersusOnePlayerCount,
			TeamSize:            ArenaOneVersusOneTeamSize, IsRanked: true}},
		{Criteria: MatchmakingCriteria{GameType: ArenaGameType,
			ExpectedPlayerCount: ArenaOneVersusOnePlayerCount,
			TeamSize:            ArenaOneVersusOneTeamSize}},
		{Criteria: MatchmakingCriteria{GameType: ArenaGameType,
			ExpectedPlayerCount: ArenaExpectedPlayerCount, TeamSize: ArenaTeamSize,
			IsRanked: true}},
		{Criteria: MatchmakingCriteria{GameType: ArenaGameType,
			ExpectedPlayerCount: ArenaExpectedPlayerCount, TeamSize: ArenaTeamSize}},
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for _, reservation := range e.sessions {
		criteria := reservation.Criteria
		criteria.SelectedDifficulty = ""
		for index := range observations {
			observation := &observations[index]
			if observation.Criteria != criteria {
				continue
			}
			memberCount := int64(len(reservation.UserIDs))
			if memberCount == 0 {
				break
			}
			ageSeconds := at.Sub(reservation.CreatedAt).Seconds()
			if ageSeconds < 0 {
				ageSeconds = 0
			}
			observation.QueuedCount += memberCount
			observation.WaitSeconds += ageSeconds * float64(memberCount)
			observation.MaximumWaitSeconds = max(observation.MaximumWaitSeconds, ageSeconds)
			break
		}
	}
	return observations
}

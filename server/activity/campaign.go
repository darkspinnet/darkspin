package activity

import "time"

// CampaignLevel ranks observed successful completions of a selected threat.
// RepeatCount is the subset after the account's first observed completion.
type CampaignLevel struct {
	CampaignNumber  uint32
	LevelNumber     uint32
	CompletionCount int64
	RepeatCount     int64
}

// CampaignCompletion identifies one human participant's successful campaign zone.
// ResultID survives checkpoint restores; campaign and level identify the selected threat.
type CampaignCompletion struct {
	UserID         int64
	ResultID       uint64
	CampaignNumber uint32
	LevelNumber    uint32
	At             time.Time
}

// CampaignSample counts durable successful player completions on a UTC day.
// RepeatCount is the subset after an account's first observed level completion.
type CampaignSample struct {
	At              time.Time
	CompletionCount int64
	RepeatCount     int64
}

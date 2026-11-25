package gameplay

// Fixed stance recovery and idle delays retain their authored timestamps.
// Their owning callback performs its existing run or action validation; these
// waits do not use the live crowd-control polling policy.
type campaignNPCTimedResumeStep struct {
	timestamp uint64
	resume    campaignNPCArrival
}

func (e campaignNPCTimedResumeStep) produce() ([][]byte, error) {
	return e.resume(e.timestamp)
}

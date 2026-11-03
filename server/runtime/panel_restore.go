package runtime

import (
	"time"

	"github.com/darkspinnet/darkspin/server/bootstrap"
	"github.com/darkspinnet/darkspin/server/controlpanel"
)

func observeRestore(runtimeDirectory string) controlpanel.RestoreHealth {
	observation := controlpanel.RestoreHealth{Status: "unknown"}
	record, err := bootstrap.ReadRestoreObservation(runtimeDirectory)
	if err != nil {
		// A missing or invalid record cannot establish any recovery outcome.
		return observation
	}
	if record.UpdatedAt.After(time.Now().UTC()) {
		return observation
	}
	observation.BackupID, observation.StartedAt = record.BackupID, record.StartedAt
	observation.UpdatedAt, observation.RestoredAt, observation.HealthyAt = record.UpdatedAt, record.RestoredAt, record.HealthyAt
	observation.FailureCode = record.FailureCode
	switch record.Status {
	case "restored", "healthy", "failed":
		observation.Status = record.Status
	default:
		// An unfinished attempt may have been interrupted by a crash. The panel
		// never claims an ongoing restore merely because an attempt was recorded.
	}
	return observation
}

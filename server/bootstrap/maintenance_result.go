package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaintenanceResult contains public-safe progress, separate from private
// bootstrap descriptors, snapshots, and diagnostic failure text.
type MaintenanceResult struct {
	OperationID      string    `json:"operation_id"`
	Action           string    `json:"action"`
	Phase            string    `json:"phase"`
	UpdatedAt        time.Time `json:"updated_at"`
	InstalledVersion string    `json:"installed_version,omitempty"`
	CandidateVersion string    `json:"candidate_version,omitempty"`
	IsSuccess        *bool     `json:"is_success,omitempty"`
	FailureCode      string    `json:"failure_code,omitempty"`
}

// OperationAudit completes an already accepted operation in the shared audit
// journal. Its implementation must make repeated terminal observations idempotent.
type OperationAudit interface {
	CompleteServerOperation(context.Context, string, bool, string) error
}

func (e *supervisor) auditResult(result MaintenanceResult) {
	if e.option.OperationAudit == nil || result.IsSuccess == nil ||
		(result.Phase != "healthy" && result.Phase != "rolled_back" && result.Phase != "failed") {
		return
	}
	// The durable result is retained before this write. Child reconciliation and
	// the next supervisor startup retry the same accepted operation on failure.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	isSuccess := result.Phase == "healthy" && *result.IsSuccess
	outcome := result.Phase
	if !isSuccess && result.Phase == "healthy" {
		outcome = "update_failed"
	}
	err := e.option.OperationAudit.CompleteServerOperation(ctx, result.OperationID, isSuccess, outcome)
	if err != nil {
		e.option.Logger.Error("maintenance outcome audit deferred; durable result retained", "operation_id", result.OperationID, "error", err)
	}
}

// ReadMaintenanceResult takes the directory containing darkspin.toml, not its
// darkspin child. Absence preserves errors.Is(err, os.ErrNotExist).
func ReadMaintenanceResult(runtimeDirectory string) (MaintenanceResult, error) {
	var result MaintenanceResult
	r, err := os.Open(filepath.Join(runtimeDirectory, "maintenance-result.json"))
	if err != nil {
		return result, fmt.Errorf("resultOpen: %w", err)
	}
	payload, err := io.ReadAll(io.LimitReader(r, 16385))
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return result, fmt.Errorf("resultRead: %w", errors.Join(err, closeErr))
	}
	if len(payload) > 16384 {
		return result, errors.New("maintenance result exceeds size limit")
	}
	err = json.Unmarshal(payload, &result)
	if err != nil {
		return result, fmt.Errorf("resultDecode: %w", err)
	}
	err = validateMaintenanceResult(result)
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("resultValidate: %w", err)
	}
	return result, nil
}

func validateMaintenanceResult(result MaintenanceResult) error {
	if !safeResultText(result.OperationID, 128) || result.OperationID == "" ||
		(result.Action != "restart" && result.Action != "update") || result.UpdatedAt.IsZero() ||
		!safeResultText(result.InstalledVersion, 128) || !safeResultText(result.CandidateVersion, 128) ||
		!safeResultText(result.FailureCode, 80) {
		return errors.New("invalid maintenance result identity")
	}
	switch result.Phase {
	case "backing_up", "checking", "starting", "validating", "healthy", "rolled_back", "failed":
	default:
		return errors.New("invalid maintenance result phase")
	}
	return nil
}

func safeResultText(text string, limit int) bool {
	if len(text) > limit || !utf8.ValidString(text) {
		return false
	}
	for _, character := range text {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (e *supervisor) progress(phase string) error {
	if e.operation == nil {
		return nil
	}
	e.operation.Phase = phase
	e.operation.UpdatedAt = time.Now().UTC()
	e.operation.IsSuccess = nil
	if phase == "healthy" || phase == "rolled_back" || phase == "failed" {
		isSuccess := phase == "healthy" && e.operation.FailureCode == ""
		e.operation.IsSuccess = &isSuccess
	}
	err := validateMaintenanceResult(*e.operation)
	if err != nil {
		return fmt.Errorf("progressValidate: %w", err)
	}
	err = e.storage.writeJSON(e.storage.path("runtime", "maintenance-result.json"), *e.operation)
	if err != nil {
		return fmt.Errorf("progressWrite: %w", err)
	}
	e.auditResult(*e.operation)
	return nil
}

func (e *supervisor) failOperation(code string) {
	if e.operation == nil {
		return
	}
	if e.operation.FailureCode == "" || code == "rollback_failed" {
		e.operation.FailureCode = code
	}
	err := e.progress("failed")
	if err != nil {
		e.option.Logger.Error("record maintenance failure", "error", err)
	}
}

func (e *supervisor) loadOperation() error {
	result, err := ReadMaintenanceResult(filepath.Dir(e.option.ConfigPath))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("operationRead: %w", err)
	}
	if err == nil {
		e.auditResult(result)
	}
	if err == nil && result.Phase != "healthy" && result.Phase != "rolled_back" {
		e.operation = &result
		e.isRollback = result.FailureCode == "candidate_rolled_back"
		if result.Phase == "backing_up" || result.Phase == "checking" {
			e.operation.FailureCode = "update_interrupted"
		}
	}
	var transaction updateTransaction
	err = readJSON(e.storage.path("firstboot.json"), &transaction)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("operationTransaction: %w", err)
	}
	if transaction.OperationID == "" {
		return nil
	}
	if transaction.Phase == "healthy" && result.OperationID == transaction.OperationID &&
		(result.Phase == "healthy" || result.Phase == "rolled_back") {
		return nil
	}
	if e.operation == nil || e.operation.OperationID != transaction.OperationID {
		e.operation = &MaintenanceResult{
			OperationID: transaction.OperationID, Action: "update", Phase: "validating",
			UpdatedAt: time.Now().UTC(), InstalledVersion: transaction.InstalledVersion,
			CandidateVersion: transaction.CandidateVersion,
		}
	}
	if e.operation.InstalledVersion == "" {
		e.operation.InstalledVersion = transaction.InstalledVersion
	}
	e.operation.CandidateVersion = transaction.CandidateVersion
	return nil
}

func (e *supervisor) markRecovered() error {
	if e.operation == nil {
		return nil
	}
	e.isRollback = true
	e.operation.FailureCode = "candidate_rolled_back"
	return e.progress("starting")
}

func (e *Storage) recordRestoredOperation(transaction updateTransaction) error {
	if transaction.OperationID == "" {
		return nil
	}
	result := MaintenanceResult{
		OperationID: transaction.OperationID, Action: "update", Phase: "starting",
		UpdatedAt: time.Now().UTC(), InstalledVersion: transaction.InstalledVersion,
		CandidateVersion: transaction.CandidateVersion, FailureCode: "candidate_rolled_back",
	}
	err := e.writeJSON(e.path("runtime", "maintenance-result.json"), result)
	if err != nil {
		return fmt.Errorf("restoredProgress: %w", err)
	}
	return nil
}

func (e *supervisor) completeHealthyOperation() error {
	if e.operation == nil {
		return nil
	}
	phase := "healthy"
	if e.isRollback {
		phase = "rolled_back"
	} else if e.operation.CandidateVersion != "" && e.operation.FailureCode == "" {
		e.operation.InstalledVersion = e.operation.CandidateVersion
	}
	err := e.progress(phase)
	if err != nil {
		return fmt.Errorf("healthyProgress: %w", err)
	}
	e.operation = nil
	e.isRollback = false
	return nil
}

func (e *supervisor) installedIdentity(ctx context.Context) (installedBuildIdentity, error) {
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	identity, err := e.readInstalledIdentity(buildCtx)
	if err != nil {
		return identity, fmt.Errorf("installedRead: %w", err)
	}
	return identity, nil
}

type installedBuildIdentity struct {
	BuildID string `json:"build_id"`
	Version string `json:"version"`
}

func (e *supervisor) readInstalledIdentity(ctx context.Context) (installedBuildIdentity, error) {
	var identity installedBuildIdentity
	cmd := exec.CommandContext(ctx, e.binaryPath, "build-info")
	cmd.Dir = filepath.Dir(e.option.ConfigPath)
	cmd.Env = e.childEnvs()
	payload, err := cmd.Output()
	if err != nil {
		return identity, fmt.Errorf("identityRead: %w", err)
	}
	err = json.Unmarshal(payload, &identity)
	if err != nil {
		return identity, fmt.Errorf("identityDecode: %w", err)
	}
	if identity.BuildID == "" || !safeResultText(identity.Version, 128) {
		return identity, errors.New("invalid installed build identity")
	}
	return identity, nil
}

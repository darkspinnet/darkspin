package sporenet

import (
	"context"
	"fmt"
)

// AdmissionPolicy checks current account access before credentials or gameplay
// admission are accepted. Install it before the manager accepts requests.
type AdmissionPolicy interface {
	CheckAdmission(context.Context, int64) error
}

func (e *UserManager) SetAdmissionPolicy(policy AdmissionPolicy) {
	e.mu.Lock()
	e.admissionPolicy = policy
	e.mu.Unlock()
}

func (e *UserManager) checkAdmission(ctx context.Context, userID int64) error {
	e.mu.RLock()
	policy := e.admissionPolicy
	e.mu.RUnlock()
	err := validateAdmissionCredential(ctx, policy, userID)
	if err != nil {
		return fmt.Errorf("credentialAdmission: %w", err)
	}
	if policy == nil {
		return nil
	}
	err = policy.CheckAdmission(ctx, userID)
	if err != nil {
		return fmt.Errorf("accountAdmission: %w", err)
	}
	return nil
}

// AuthorizeSession rechecks current account admission for an already authenticated
// transport or pending launch grant. It does not activate a gameplay session.
func (e *UserManager) AuthorizeSession(ctx context.Context, userID int64) error {
	err := e.checkAdmission(ctx, userID)
	if err != nil {
		return fmt.Errorf("sessionAdmission: %w", err)
	}
	return nil
}

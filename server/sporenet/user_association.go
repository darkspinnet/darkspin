package sporenet

import (
	"context"
	"errors"
	"fmt"
)

// UpdateUserAssociations applies and durably stores one association-list
// mutation. A failed save restores the accepted in-memory aggregate.
func (m *UserManager) UpdateUserAssociations(
	ctx context.Context, user *User, listType uint32, members []AssociationMember, isAddition bool,
) error {
	if user == nil {
		return errors.New("association user missing")
	}
	if listType == 0 {
		return errors.New("association list missing")
	}
	if isAddition {
		for _, member := range members {
			if member.ID == user.Account.ID {
				return errors.New("association self target")
			}
		}
	}
	if len(members) == 0 {
		return nil
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	previous := user.associationSnapshot()
	user.UpdateAssociations(listType, members, isAddition)
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAssociations(previous)
		return fmt.Errorf("associationSave: %w", err)
	}
	return nil
}

func (u *User) associationSnapshot() map[uint32][]AssociationMember {
	u.mu.RLock()
	associations := cloneAssociations(u.Associations)
	u.mu.RUnlock()
	return associations
}

func (u *User) restoreAssociations(associations map[uint32][]AssociationMember) {
	u.mu.Lock()
	u.Associations = cloneAssociations(associations)
	u.mu.Unlock()
}

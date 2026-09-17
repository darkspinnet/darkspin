// Package local adapts active SporeNet users to party membership projection.
package local

import "github.com/darkspinnet/darkspin/server/sporenet"

// Membership projects committed party state onto active users.
type Membership struct {
	userManager *sporenet.UserManager
}

// NewMembership creates the active-user party adapter.
func NewMembership(userManager *sporenet.UserManager) Membership {
	return Membership{userManager: userManager}
}

// ClaimParty reserves one active user for the party.
func (e Membership) ClaimParty(userID int64, partyID uint32) bool {
	if e.userManager == nil {
		return false
	}
	user := e.userManager.UserByID(userID)
	if user == nil {
		return false
	}
	return user.ClaimPlaygroup(partyID)
}

// ReleaseParty clears one matching active-user party projection.
func (e Membership) ReleaseParty(userID int64, partyID uint32) {
	if e.userManager == nil {
		return
	}
	user := e.userManager.UserByID(userID)
	if user == nil {
		return
	}
	user.ReleasePlaygroup(partyID)
}

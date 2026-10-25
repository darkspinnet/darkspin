package blaze

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type publicProfileReader interface {
	PublicProfile(context.Context, int64, string) (sporenet.UserView, error)
}

func lookupSocialIdentity(ctx context.Context, userManager UserManager, fields []tdf.Field) (sporenet.UserView, error) {
	name := strings.TrimSpace(fieldString(fields, "NAME"))
	id := fieldInteger(fields, "ID")
	accountID := fieldInteger(fields, "AID")
	if id == 0 {
		id = accountID
	}
	if id > math.MaxInt64 || (accountID != 0 && accountID != id) || (id == 0 && name == "") {
		return sporenet.UserView{}, sporenet.ErrUserNotFound
	}
	var user *sporenet.User
	if id != 0 {
		for _, candidate := range userManager.Users() {
			if candidate != nil && candidate.Account.ID == int64(id) {
				user = candidate
				break
			}
		}
	} else {
		user = userManager.UserByLoginName(name)
		if user == nil {
			user = userManager.UserByDisplayName(name)
		}
	}
	var profile sporenet.UserView
	if user != nil {
		profile = user.View()
	} else {
		reader, isAvailable := userManager.(publicProfileReader)
		if !isAvailable {
			return sporenet.UserView{}, sporenet.ErrUserNotFound
		}
		lookupName := name
		if id != 0 {
			lookupName = ""
		}
		var err error
		profile, err = reader.PublicProfile(ctx, int64(id), lookupName)
		if err != nil {
			return sporenet.UserView{}, fmt.Errorf("identityProfile: %w", err)
		}
	}
	if id != 0 && name != "" && !strings.EqualFold(name, profile.DisplayName) && !strings.EqualFold(name, profile.LoginName) {
		return sporenet.UserView{}, sporenet.ErrUserNotFound
	}
	return profile, nil
}

func isSocialUserOnline(request *Request, userID int64) bool {
	return request != nil && request.Session != nil && request.Session.server != nil &&
		len(request.Session.server.sessionsForUser(userID)) != 0
}

func storedAssociationMembers(user *sporenet.User, listType uint32, fields []tdf.Field) ([]sporenet.AssociationMember, bool) {
	requested, isFound := tdf.Find(fields, "BIDL")
	if !isFound || requested.Type != tdf.List {
		return nil, false
	}
	storedMembers := user.AssociationSnapshot(listType)
	members := make([]sporenet.AssociationMember, 0, len(requested.Items))
	for _, item := range requested.Items {
		memberFields := item.Fields
		info, hasInfo := tdf.Find(memberFields, "INFO")
		if hasInfo && info.Type == tdf.Struct {
			memberFields = info.Fields
		}
		identity, hasIdentity := tdf.Find(memberFields, "LMID")
		if hasIdentity && identity.Type == tdf.Struct {
			memberFields = identity.Fields
		}
		id := fieldInteger(memberFields, "BLID")
		name := strings.TrimSpace(fieldString(memberFields, "PNAM"))
		isMatched := false
		for _, member := range storedMembers {
			if (id != 0 && id <= math.MaxInt64 && member.ID == int64(id)) ||
				(id == 0 && name != "" && strings.EqualFold(member.Name, name)) {
				members = append(members, member)
				isMatched = true
				break
			}
		}
		if !isMatched {
			return nil, false
		}
	}
	return members, len(members) != 0
}

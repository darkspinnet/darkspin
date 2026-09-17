package sporenet

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const maximumCopiedUserIdentityLength = 20

// UserProfileCopy reports one complete durable profile copy.
type UserProfileCopy struct {
	SourceLoginName      string `json:"source_login_name"`
	DestinationLoginName string `json:"destination_login_name"`
	DestinationUserID    int64  `json:"destination_user_id"`
	SquadCount           int    `json:"squad_count"`
	CreatureCount        int    `json:"creature_count"`
	PartCount            int    `json:"part_count"`
}

// CopyUserProfile copies one consistent persisted profile into a fresh public
// identity. Active world membership is never inherited by the destination.
func CopyUserProfile(
	ctx context.Context, repository UserRepository,
	sourceLoginName string, destinationLoginName string,
) (UserProfileCopy, error) {
	if ctx == nil {
		return UserProfileCopy{}, errors.New("nil context")
	}
	err := ctx.Err()
	if err != nil {
		return UserProfileCopy{}, fmt.Errorf("contextCheck: %w", err)
	}
	if repository == nil {
		return UserProfileCopy{}, errors.New("user repository unavailable")
	}
	sourceLoginName = strings.TrimSpace(sourceLoginName)
	destinationLoginName = strings.TrimSpace(destinationLoginName)
	if sourceLoginName == "" {
		return UserProfileCopy{}, errors.New("source login name is required")
	}
	err = validateCopiedUserIdentity(destinationLoginName)
	if err != nil {
		return UserProfileCopy{}, fmt.Errorf("destinationIdentity: %w", err)
	}
	if strings.EqualFold(sourceLoginName, destinationLoginName) {
		return UserProfileCopy{}, errors.New("source and destination users must differ")
	}
	record, err := repository.LoadByLoginName(ctx, sourceLoginName)
	if err != nil {
		return UserProfileCopy{}, fmt.Errorf("sourceLoad: %w", err)
	}
	canonicalSourceLoginName := record.LoginName
	record.LoginName = destinationLoginName
	record.DisplayName = destinationLoginName
	record.Account.CurrentGameID = 0
	record.Account.CurrentPlaygroupID = 0
	destinationUserID, err := repository.Create(ctx, record)
	if err != nil {
		return UserProfileCopy{}, fmt.Errorf("destinationCreate: %w", err)
	}
	return UserProfileCopy{
		SourceLoginName:      canonicalSourceLoginName,
		DestinationLoginName: destinationLoginName,
		DestinationUserID:    destinationUserID,
		SquadCount:           len(record.Squads),
		CreatureCount:        len(record.Creatures),
		PartCount:            len(record.Parts),
	}, nil
}

func validateCopiedUserIdentity(identity string) error {
	if identity == "" {
		return errors.New("destination login name is required")
	}
	if len(identity) > maximumCopiedUserIdentityLength {
		return fmt.Errorf(
			"destination login name cannot exceed %d characters",
			maximumCopiedUserIdentityLength,
		)
	}
	for _, character := range identity {
		isLowercase := character >= 'a' && character <= 'z'
		isUppercase := character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLowercase && !isUppercase && !isDigit {
			return errors.New("destination login name can contain only letters and numbers")
		}
	}
	return nil
}

// PrepareCreatedUserRecord applies the newly allocated persistent identity to
// one record before its dependent rows are inserted.
func PrepareCreatedUserRecord(record UserRecord, userID int64) (UserRecord, error) {
	if userID <= 0 {
		return UserRecord{}, errors.New("created user ID is invalid")
	}
	previousUserID := record.Account.ID
	record.Account.ID = userID
	record.Account.CurrentGameID = 0
	record.Account.CurrentPlaygroupID = 0
	for index := range record.Parts {
		part := &record.Parts[index]
		isPreviousReference := previousUserID > 0 &&
			part.ReferenceID>>32 == uint64(previousUserID)
		if part.ID != 0 && (part.ReferenceID == 0 || isPreviousReference) {
			part.ReferenceID = uint64(userID)<<32 | part.ID
		}
	}
	for _, creature := range record.Creatures {
		if creature == nil || previousUserID <= 0 ||
			creature.CreatorID != previousUserID {
			continue
		}
		creature.CreatorID = userID
	}
	return record, nil
}

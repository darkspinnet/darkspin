package blaze

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Build 103 C2C640 immediately resolves every friend through E14AE0 and
// dereferences the resulting Blaze user at DFF280. Association member data
// alone does not create that user, particularly for offline friends. Publish
// the allowlisted identity and status before the association reply callback.
func queueAssociationMemberVisibility(req *Request, member sporenet.AssociationMember) error {
	if member.ID <= 0 || member.Name == "" {
		return fmt.Errorf("invalid association member identity %d", member.ID)
	}
	identityFields := []tdf.Field{
		tdf.FieldNamed("AID", tdf.IntegerValue(uint64(member.ID))),
		tdf.FieldNamed("ALOC", tdf.IntegerValue(0)),
		tdf.FieldNamed("EXBB", tdf.BinaryValue(nil)),
		tdf.FieldNamed("EXID", tdf.IntegerValue(0)),
		tdf.FieldNamed("ID", tdf.IntegerValue(uint64(member.ID))),
		tdf.FieldNamed("NAME", tdf.StringValue(member.Name)),
	}
	err := req.QueueNotificationBeforeReply(UserSessionComponentID, userSessionUserAdded, []tdf.Field{
		tdf.FieldNamed("DATA", tdf.StructValue(extendedDataFields(nil)...)),
		tdf.FieldNamed("USER", tdf.StructValue(identityFields...)),
	})
	if err != nil {
		return fmt.Errorf("memberAdded: %w", err)
	}
	flags := uint64(1)
	if isSocialUserOnline(req, member.ID) {
		flags = 3
	}
	err = req.QueueNotificationBeforeReply(UserSessionComponentID, userSessionUserUpdated, []tdf.Field{
		tdf.FieldNamed("FLGS", tdf.IntegerValue(flags)),
		tdf.FieldNamed("ID", tdf.IntegerValue(uint64(member.ID))),
	})
	if err != nil {
		return fmt.Errorf("memberStatus: %w", err)
	}
	return nil
}

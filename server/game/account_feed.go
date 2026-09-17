package game

import (
	"sort"
	"strconv"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

const accountFeedLimit = 100

type accountFeedEntry struct {
	accountID   int64
	displayName string
	event       sporenet.UserEvent
}

func accountFeedNode(view sporenet.UserView, isPublic bool, friendViews ...sporenet.UserView) string {
	entries := make([]accountFeedEntry, 0, len(view.Events))
	for _, event := range view.Events {
		if isPublic && !event.IsPublic {
			continue
		}
		entries = append(entries, accountFeedEntry{
			accountID: view.Account.ID, displayName: view.DisplayName, event: event,
		})
	}
	for _, friendView := range friendViews {
		for _, event := range friendView.Events {
			if !event.IsPublic {
				continue
			}
			entries = append(entries, accountFeedEntry{
				accountID: friendView.Account.ID, displayName: friendView.DisplayName, event: event,
			})
		}
	}
	sort.SliceStable(entries, func(firstIndex, secondIndex int) bool {
		return entries[firstIndex].event.OccurredAt > entries[secondIndex].event.OccurredAt
	})
	items := make([]string, 0, min(len(entries), accountFeedLimit))
	for _, entry := range entries {
		items = append(items, xmlNode("item",
			xmlText("account_id", strconv.FormatInt(entry.accountID, 10)),
			xmlText("message_id", number(entry.event.MessageID)),
			xmlText("metadata", entry.event.Metadata),
			xmlText("name", entry.displayName),
			xmlText("time", strconv.FormatInt(entry.event.OccurredAt, 10)),
		))
		if len(items) == accountFeedLimit {
			break
		}
	}
	return xmlNode("feed", items...)
}

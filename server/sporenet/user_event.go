package sporenet

import (
	"fmt"
	"time"
)

const (
	UserEventMessageCreature  uint32 = 2
	UserEventMessageMilestone uint32 = 4
)

type userEventAppend struct {
	previousEvents []UserEvent
	isChanged      bool
}

func (u *User) appendEvents(events ...UserEvent) userEventAppend {
	if u == nil {
		return userEventAppend{}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	previous := append([]UserEvent(nil), u.Events...)
	if len(events) == 0 {
		return userEventAppend{previousEvents: previous}
	}
	keySet := make(map[string]struct{}, len(u.Events)+len(events))
	for _, event := range u.Events {
		keySet[event.Key] = struct{}{}
	}
	isChanged := false
	for _, event := range events {
		if event.Key == "" || event.MessageID == 0 || event.Metadata == "" {
			continue
		}
		if _, isFound := keySet[event.Key]; isFound {
			continue
		}
		if event.OccurredAt <= 0 {
			event.OccurredAt = time.Now().Unix()
		}
		u.Events = append(u.Events, event)
		keySet[event.Key] = struct{}{}
		isChanged = true
	}
	return userEventAppend{previousEvents: previous, isChanged: isChanged}
}

func (u *User) restoreEvents(events []UserEvent) {
	if u == nil {
		return
	}
	u.mu.Lock()
	u.Events = append([]UserEvent(nil), events...)
	u.mu.Unlock()
}

func creatureAcquiredEvent(template *TemplateCreature, creatureID uint32) UserEvent {
	if template == nil || creatureID == 0 {
		return UserEvent{}
	}
	metadata := fmt.Sprintf("%d;%d;%s;%s", template.Noun, creatureID, template.NameLocaleID, template.Name)
	return UserEvent{
		Key: fmt.Sprintf("creature-acquired:%d", creatureID), MessageID: UserEventMessageCreature,
		Metadata: metadata, IsPublic: true,
	}
}

func levelMilestoneEvents(previousLevel, level uint32) []UserEvent {
	if previousLevel >= 4 || level < 4 {
		return nil
	}
	return []UserEvent{
		{Key: "level:4", MessageID: UserEventMessageMilestone, Metadata: "Reached Crogenitor level 4.", IsPublic: true},
		{Key: "hero-available:arakna", MessageID: UserEventMessageMilestone, Metadata: "Arakna, the Scout Collector, is now available.", IsPublic: true},
		{Key: "hero-available:vex", MessageID: UserEventMessageMilestone, Metadata: "Vex, the Chrono Shifter, is now available.", IsPublic: true},
		{Key: "hero-available:viper", MessageID: UserEventMessageMilestone, Metadata: "Viper, the Toxic Ravager, is now available.", IsPublic: true},
		{Key: "upgrade-available:hero-genetic", MessageID: UserEventMessageMilestone, Metadata: "A hero genetic upgrade is now available in the Upgrade Store.", IsPublic: true},
	}
}

func campaignCompletedEvent(completedIndex uint32) UserEvent {
	if completedIndex == 0 {
		return UserEvent{}
	}
	metadata := fmt.Sprintf("Completed campaign mission %d.", completedIndex)
	if completedIndex == 1 {
		metadata = "Completed campaign level 1-1."
	}
	return UserEvent{
		Key:       fmt.Sprintf("campaign-complete:%d", completedIndex),
		MessageID: UserEventMessageMilestone, Metadata: metadata,
	}
}

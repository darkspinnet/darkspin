package game

import "context"

// TutorialEndPublisher delivers the session-ending event through the control
// connection. The adapter owns the wire notification and client leave exchange.
type TutorialEndPublisher interface {
	PublishTutorialEnd(context.Context, int64, uint32) error
}

// UseTutorialEndPublisher is configured before accepting gameplay connections.
func (e *GameplayJoin) UseTutorialEndPublisher(publisher TutorialEndPublisher) {
	e.tutorialEndPublisher = publisher
}

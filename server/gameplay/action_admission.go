package gameplay

import "context"

type gameplayActionDispositionKey struct{}

// gameplayActionDisposition belongs to one synchronous admission. A repeated
// basic input can retain an already accepted cycle without starting an action
// or sending a native rejection that would clear that cycle.
type gameplayActionDisposition struct {
	isBasicRetained bool
}

func retainBasicAction(ctx context.Context) {
	disposition, isFound := ctx.Value(gameplayActionDispositionKey{}).(*gameplayActionDisposition)
	if isFound {
		disposition.isBasicRetained = true
	}
}

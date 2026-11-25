package gameplay

import "sort"

type AccountTransport struct {
	Address    string
	Generation uint64
	GameID     uint32
}

type accountTransportSource interface {
	AccountTransports(int64) []AccountTransport
}

// The provider is connected to its registry during synchronous handler
// construction. Its identity remains stable across copied Lifecycle handles.
type gameplayAccountTransportSource struct {
	registry *gameplaySessionRegistry
}

func (e *gameplayAccountTransportSource) AccountTransports(userID int64) []AccountTransport {
	if e == nil || e.registry == nil || userID <= 0 {
		return nil
	}
	e.registry.mutex.Lock()
	transports := make([]AccountTransport, 0)
	for address, session := range e.registry.sessions {
		if session.binding.UserID != uint64(userID) || session.transportGeneration == 0 {
			continue
		}
		transports = append(transports, AccountTransport{
			Address: address, Generation: session.transportGeneration, GameID: session.binding.GameID,
		})
	}
	e.registry.mutex.Unlock()
	sort.Slice(transports, func(first, second int) bool {
		return transports[first].Address < transports[second].Address
	})
	return transports
}

// AccountTransports snapshots exact transport generations; callers must never
// retire a peer merely by endpoint because that endpoint can be reused.
func (e Lifecycle) AccountTransports(userID int64) []AccountTransport {
	if e.accountTransportSource == nil {
		return nil
	}
	return e.accountTransportSource.AccountTransports(userID)
}

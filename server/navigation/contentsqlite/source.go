package contentsqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/darkspinnet/darkspin/server/navigation"
)

type levelNavigationStore interface {
	LevelNavigation(context.Context, string) ([]byte, error)
}

type Source struct {
	store         levelNavigationStore
	mutex         sync.RWMutex
	meshesByLevel map[string]*navigation.Mesh
}

func NewSource(store levelNavigationStore) (*Source, error) {
	if store == nil {
		return nil, errors.New("navigation source nil store")
	}
	return &Source{
		store: store, meshesByLevel: make(map[string]*navigation.Mesh),
	}, nil
}

func (s *Source) LoadCampaignNavigation(ctx context.Context, levelName string) (*navigation.Mesh, error) {
	levelKey := strings.ToLower(strings.TrimSpace(levelName))
	if levelKey == "" {
		return nil, errors.New("navigation level empty")
	}
	s.mutex.RLock()
	cached := s.meshesByLevel[levelKey]
	s.mutex.RUnlock()
	if cached != nil {
		return cached, nil
	}
	data, err := s.store.LevelNavigation(ctx, levelName)
	if err != nil {
		return nil, fmt.Errorf("navigationRead: %w", err)
	}
	mesh, err := navigation.ParseBFX(data)
	if err != nil {
		return nil, fmt.Errorf("navigationParse: %w", err)
	}
	s.mutex.Lock()
	cached = s.meshesByLevel[levelKey]
	if cached == nil {
		s.meshesByLevel[levelKey] = mesh
		cached = mesh
	}
	s.mutex.Unlock()
	return cached, nil
}

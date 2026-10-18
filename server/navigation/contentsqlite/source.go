package contentsqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/navigation"
)

type levelNavigationStore interface {
	LevelNavigation(context.Context, string) ([]byte, error)
}

type navigationTuningStore interface {
	NavigationTuning(context.Context) ([]contentstore.NavigationTuningRow, error)
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
	tuningStore, isAvailable := s.store.(navigationTuningStore)
	if !isAvailable {
		return nil, errors.New("navigation tuning source unavailable")
	}
	rows, err := tuningStore.NavigationTuning(ctx)
	if err != nil {
		return nil, fmt.Errorf("navigationTuning: %w", err)
	}
	tunings := make([]navigation.LayerTuning, 0, len(rows))
	for _, row := range rows {
		if row.Ordinal < 0 || row.Ordinal > 255 {
			return nil, errors.New("navigation tuning ordinal invalid")
		}
		tunings = append(tunings, navigation.LayerTuning{
			Ordinal: uint8(row.Ordinal), AgentRadius: row.AgentRadius,
		})
	}
	err = mesh.AttachTuning(tunings)
	if err != nil {
		return nil, fmt.Errorf("navigationAttach: %w", err)
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

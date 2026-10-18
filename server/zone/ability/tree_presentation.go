package ability

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

type TreeOfLifePhase uint8

const (
	TreeOfLifeSpawned TreeOfLifePhase = iota + 1
	TreeOfLifeReleased
)

// TreeOfLifePresentation retains visibility, independently of a connection's
// ability run. Times are absolute offsets on the original zone's simulation
// clock; reconnect never restarts this lifetime or the healing behavior.
type TreeOfLifePresentation struct {
	ObjectID       uint32
	SourceObjectID uint32
	NounName       string
	Position       game.Vec3
	Team           uint8
	Phase          TreeOfLifePhase
	SpawnedAt      time.Duration
	ExpiresAt      time.Duration
}

type TreeOfLifePresentationSession struct {
	mu       sync.RWMutex
	trees    map[uint32]TreeOfLifePresentation
	revision uint64
}

func NewTreeOfLifePresentationSession() *TreeOfLifePresentationSession {
	return &TreeOfLifePresentationSession{trees: make(map[uint32]TreeOfLifePresentation)}
}

func (e *TreeOfLifePresentationSession) Commit(tree TreeOfLifePresentation) error {
	if tree.ObjectID == 0 || tree.SourceObjectID == 0 || tree.NounName == "" ||
		!isFinitePosition(tree.Position) || tree.Phase != TreeOfLifeSpawned ||
		tree.SpawnedAt < 0 || tree.ExpiresAt <= tree.SpawnedAt {
		return errors.New("invalid tree presentation")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	existingTree, isFound := e.trees[tree.ObjectID]
	if isFound {
		return fmt.Errorf("tree presentation already committed: %d", existingTree.ObjectID)
	}
	e.trees[tree.ObjectID] = tree
	e.revision++
	return nil
}

func (e *TreeOfLifePresentationSession) Release(objectID uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	tree, isFound := e.trees[objectID]
	if !isFound || tree.Phase == TreeOfLifeReleased {
		return
	}
	tree.Phase = TreeOfLifeReleased
	e.trees[objectID] = tree
	e.revision++
}

func (e *TreeOfLifePresentationSession) Retire(objectID uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	tree, isFound := e.trees[objectID]
	if !isFound {
		return
	}
	delete(e.trees, tree.ObjectID)
	e.revision++
}

func (e *TreeOfLifePresentationSession) Revision() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.revision
}

func (e *TreeOfLifePresentationSession) SnapshotsAt(now time.Duration) []TreeOfLifePresentation {
	e.mu.RLock()
	defer e.mu.RUnlock()
	trees := make([]TreeOfLifePresentation, 0, len(e.trees))
	for _, tree := range e.trees {
		if now < tree.SpawnedAt || now >= tree.ExpiresAt {
			continue
		}
		trees = append(trees, tree)
	}
	slices.SortFunc(trees, func(first TreeOfLifePresentation, second TreeOfLifePresentation) int {
		return cmp.Compare(first.ObjectID, second.ObjectID)
	})
	return trees
}

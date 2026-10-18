package sqlite

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const decodedResourceCacheLimit = 32 << 20

type importCacheKey struct{}

type decodedResourceKey struct {
	reader      *dbpf.Reader
	offset      uint32
	storedSize  uint32
	decodedSize uint32
	compression uint16
}

type decodedResourceEntry struct {
	key     decodedResourceKey
	payload []byte
}

// importPackageCache owns readers and a bounded decoded-payload LRU for one build.
type importPackageCache struct {
	mutex               sync.Mutex
	readersByPath       map[string]*dbpf.Reader
	filesByPath         map[string]*os.File
	decodedEntriesByKey map[decodedResourceKey]*list.Element
	decodedOrder        list.List
	decodedBytes        int
}

func newImportPackageCache() *importPackageCache {
	return &importPackageCache{
		readersByPath:       make(map[string]*dbpf.Reader),
		filesByPath:         make(map[string]*os.File),
		decodedEntriesByKey: make(map[decodedResourceKey]*list.Element),
	}
}

func (e *importPackageCache) close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	var closeErrors []error
	for path, r := range e.filesByPath {
		err := r.Close()
		if err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("packageClose[%s]: %w", path, err))
		}
	}
	clear(e.filesByPath)
	clear(e.readersByPath)
	clear(e.decodedEntriesByKey)
	e.decodedOrder.Init()
	return errors.Join(closeErrors...)
}

func importPackageReader(ctx context.Context, r *os.File, size int64) (*dbpf.Reader, error) {
	cache, isCached := ctx.Value(importCacheKey{}).(*importPackageCache)
	if !isCached {
		reader, err := dbpf.NewReader(r, size)
		if err != nil {
			return nil, fmt.Errorf("packageReader: %w", err)
		}
		return reader, nil
	}
	path := r.Name()
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if reader, isFound := cache.readersByPath[path]; isFound {
		return reader, nil
	}
	ownedReader, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	reader, err := dbpf.NewReader(ownedReader, size)
	if err != nil {
		closeErr := ownedReader.Close()
		return nil, fmt.Errorf("packageReader: %w", errors.Join(err, closeErr))
	}
	cache.filesByPath[path] = ownedReader
	cache.readersByPath[path] = reader
	return reader, nil
}

func readImportResource(ctx context.Context, pkg *dbpf.Reader, entry dbpf.Entry) ([]byte, error) {
	cache, isCached := ctx.Value(importCacheKey{}).(*importPackageCache)
	if !isCached {
		return openDecodedResource(pkg, entry)
	}
	key := decodedResourceKey{reader: pkg, offset: entry.Offset,
		storedSize: entry.StoredSize, decodedSize: entry.Size, compression: entry.Compression}
	cache.mutex.Lock()
	if element, isFound := cache.decodedEntriesByKey[key]; isFound {
		cache.decodedOrder.MoveToFront(element)
		payload := bytes.Clone(element.Value.(decodedResourceEntry).payload)
		cache.mutex.Unlock()
		return payload, nil
	}
	cache.mutex.Unlock()
	payload, err := openDecodedResource(pkg, entry)
	if err != nil {
		return nil, fmt.Errorf("resourceOpen: %w", err)
	}
	if len(payload) == 0 || len(payload) > decodedResourceCacheLimit {
		return payload, nil
	}
	cache.mutex.Lock()
	for cache.decodedBytes+len(payload) > decodedResourceCacheLimit {
		oldest := cache.decodedOrder.Back()
		if oldest == nil {
			break
		}
		cached := oldest.Value.(decodedResourceEntry)
		delete(cache.decodedEntriesByKey, cached.key)
		cache.decodedBytes -= len(cached.payload)
		cache.decodedOrder.Remove(oldest)
	}
	element := cache.decodedOrder.PushFront(decodedResourceEntry{key: key, payload: bytes.Clone(payload)})
	cache.decodedEntriesByKey[key] = element
	cache.decodedBytes += len(payload)
	cache.mutex.Unlock()
	return payload, nil
}

func openDecodedResource(pkg *dbpf.Reader, entry dbpf.Entry) ([]byte, error) {
	r, err := pkg.Open(entry)
	if err != nil {
		return nil, fmt.Errorf("resourceOpen: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("resourceRead: %w", err)
	}
	return payload, nil
}

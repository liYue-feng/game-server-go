package game

import (
	"errors"
	"testing"

	"game-server/internal/store"
)

func TestArchiveServiceLoadsEmptyBeforeSaveAndRoundTripsExactData(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewArchiveService(developmentStore)

	data, err := service.Load(1)
	if err != nil || data != "" {
		t.Fatalf("Load(before save) = %q, %v; want empty, nil", data, err)
	}

	const archiveData = `{"stage":2}`
	if err := service.Save(1, archiveData); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err = service.Load(1)
	if err != nil || data != archiveData {
		t.Fatalf("Load(after save) = %q, %v; want %q, nil", data, err, archiveData)
	}
}

func TestArchiveServicePropagatesNonNotFoundLoadError(t *testing.T) {
	loadErr := errors.New("archive database unavailable")
	service := NewArchiveService(&archiveRepositoryStub{loadErr: loadErr})

	if _, err := service.Load(1); !errors.Is(err, loadErr) {
		t.Fatalf("Load() error = %v, want repository error", err)
	}
}

type archiveRepositoryStub struct {
	loadErr error
}

func (s *archiveRepositoryStub) GetArchive(int64) (*store.Archive, error) {
	return nil, s.loadErr
}

func (s *archiveRepositoryStub) SaveArchive(*store.Archive) error {
	return nil
}

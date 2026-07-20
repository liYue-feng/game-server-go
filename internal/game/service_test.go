package game

import (
	"errors"
	"testing"

	"game-server/internal/store"
)

func TestArchiveServiceReturnsEmptyOnlyForNotFoundAndRoundTripsData(t *testing.T) {
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

func TestArchiveServicePropagatesRepositoryFailures(t *testing.T) {
	loadErr := errors.New("archive load unavailable")
	service := NewArchiveService(&archiveRepositoryStub{loadErr: loadErr})
	if _, err := service.Load(1); !errors.Is(err, loadErr) {
		t.Fatalf("Load() error = %v, want repository error", err)
	}

	saveErr := errors.New("archive save unavailable")
	service = NewArchiveService(&archiveRepositoryStub{saveErr: saveErr})
	if err := service.Save(1, "data"); !errors.Is(err, saveErr) {
		t.Fatalf("Save() error = %v, want repository error", err)
	}
}

func TestArchiveServiceRejectsNilArchiveWithoutTreatingItAsNotFound(t *testing.T) {
	service := NewArchiveService(&archiveRepositoryStub{returnNil: true})
	if data, err := service.Load(1); err == nil || data != "" {
		t.Fatalf("Load() = %q, %v; want empty data with repository contract error", data, err)
	}
}

type archiveRepositoryStub struct {
	archive   *store.Archive
	loadErr   error
	saveErr   error
	returnNil bool
}

func (s *archiveRepositoryStub) GetArchive(int64) (*store.Archive, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.returnNil {
		return nil, nil
	}
	if s.archive == nil {
		return nil, store.ErrNotFound
	}
	copy := *s.archive
	return &copy, nil
}

func (s *archiveRepositoryStub) SaveArchive(archive *store.Archive) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *archive
	s.archive = &copy
	return nil
}

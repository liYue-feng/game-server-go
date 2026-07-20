package game

import (
	"errors"
	"testing"

	"game-server/internal/protocolpb"
	"game-server/internal/store"
)

func TestArchiveServiceRoundTripsProtobufBytes(t *testing.T) {
	service := NewArchiveService(store.NewMemoryDevelopmentStore())
	want := &protocolpb.PlayerArchive{SchemaVersion: 1, Gold: 7, UnlockedStyles: []int32{1, 3}}
	if err := service.Save(1, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := service.Load(1)
	if err != nil || !found || got.GetGold() != want.Gold || len(got.UnlockedStyles) != 2 {
		t.Fatalf("Load() = %#v, %v, %v", got, found, err)
	}
}

func TestArchiveServiceCopiesInputAndLoadedArchive(t *testing.T) {
	service := NewArchiveService(store.NewMemoryDevelopmentStore())
	input := &protocolpb.PlayerArchive{Gold: 7, UnlockedStyles: []int32{1, 3}}
	if err := service.Save(1, input); err != nil {
		t.Fatal(err)
	}
	input.Gold = 99
	input.UnlockedStyles[0] = 99

	loaded, found, err := service.Load(1)
	if err != nil || !found {
		t.Fatalf("Load() = %#v, %v, %v", loaded, found, err)
	}
	if loaded.GetGold() != 7 || loaded.GetUnlockedStyles()[0] != 1 {
		t.Fatalf("Load() = %#v, want original archive values", loaded)
	}
	loaded.Gold = 55
	loaded.UnlockedStyles[0] = 55

	again, found, err := service.Load(1)
	if err != nil || !found || again.GetGold() != 7 || again.GetUnlockedStyles()[0] != 1 {
		t.Fatalf("second Load() = %#v, %v, %v; want independent stored archive", again, found, err)
	}
}

func TestArchiveServiceMapsMissingArchiveToFoundFalse(t *testing.T) {
	archive, found, err := NewArchiveService(store.NewMemoryDevelopmentStore()).Load(404)
	if err != nil || found || archive != nil {
		t.Fatalf("Load(missing) = %#v, %v, %v; want nil, false, nil", archive, found, err)
	}
}

func TestArchiveServiceRejectsMalformedStoredArchive(t *testing.T) {
	service := NewArchiveService(&archiveRepositoryStub{archive: &store.Archive{PlayerID: 1, Data: []byte{0xff}}})
	_, _, err := service.Load(1)
	if !errors.Is(err, ErrMalformedStoredArchive) {
		t.Fatalf("Load() error = %v", err)
	}
}

type archiveRepositoryStub struct{ archive *store.Archive }

func (s *archiveRepositoryStub) GetArchive(int64) (*store.Archive, error) {
	if s.archive == nil {
		return nil, store.ErrNotFound
	}
	return s.archive, nil
}
func (s *archiveRepositoryStub) SaveArchive(a *store.Archive) error { s.archive = a; return nil }

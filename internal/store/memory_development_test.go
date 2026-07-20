package store

import (
	"fmt"
	"sync"
	"testing"

	"gorm.io/gorm"
)

func TestIsNotFoundRecognizesDevelopmentAndGORMErrors(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Fatal("IsNotFound(ErrNotFound) = false, want true")
	}
	if !IsNotFound(gorm.ErrRecordNotFound) {
		t.Fatal("IsNotFound(gorm.ErrRecordNotFound) = false, want true")
	}
	if IsNotFound(nil) {
		t.Fatal("IsNotFound(nil) = true, want false")
	}
}

func TestMemoryDevelopmentStoreCreatesIndependentPlayerCopies(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStore()
	player := &Player{OpenID: "dev:alice"}

	if err := developmentStore.CreatePlayer(player); err != nil {
		t.Fatalf("CreatePlayer() error = %v", err)
	}
	if player.ID != 1 {
		t.Fatalf("CreatePlayer() ID = %d, want 1", player.ID)
	}

	player.Nickname = "mutated caller"
	stored, err := developmentStore.GetPlayerByOpenID("dev:alice")
	if err != nil {
		t.Fatalf("GetPlayerByOpenID() error = %v", err)
	}
	if stored.Nickname != "" {
		t.Fatalf("stored nickname = %q, want empty", stored.Nickname)
	}

	stored.Nickname = "mutated getter"
	storedAgain, err := developmentStore.GetPlayerByID(player.ID)
	if err != nil {
		t.Fatalf("GetPlayerByID() error = %v", err)
	}
	if storedAgain.Nickname != "" {
		t.Fatalf("stored nickname after getter mutation = %q, want empty", storedAgain.Nickname)
	}

	second := &Player{OpenID: "dev:bob"}
	if err := developmentStore.CreatePlayer(second); err != nil {
		t.Fatalf("CreatePlayer(second) error = %v", err)
	}
	if second.ID != 2 {
		t.Fatalf("CreatePlayer(second) ID = %d, want 2", second.ID)
	}
	if err := developmentStore.CreatePlayer(&Player{OpenID: "dev:alice"}); err == nil {
		t.Fatal("CreatePlayer(duplicate OpenID) returned nil error")
	}

	update, err := developmentStore.GetPlayerByID(player.ID)
	if err != nil {
		t.Fatalf("GetPlayerByID(update) error = %v", err)
	}
	update.OpenID = "dev:alice-renamed"
	update.Nickname = "Alice"
	if err := developmentStore.UpdatePlayer(update); err != nil {
		t.Fatalf("UpdatePlayer() error = %v", err)
	}
	if _, err := developmentStore.GetPlayerByOpenID("dev:alice"); !IsNotFound(err) {
		t.Fatalf("GetPlayerByOpenID(old) error = %v, want not found", err)
	}
	updated, err := developmentStore.GetPlayerByOpenID("dev:alice-renamed")
	if err != nil {
		t.Fatalf("GetPlayerByOpenID(new) error = %v", err)
	}
	if updated.ID != player.ID || updated.Nickname != "Alice" {
		t.Fatalf("updated player = %#v, want ID %d and nickname Alice", updated, player.ID)
	}

	conflicting, err := developmentStore.GetPlayerByID(second.ID)
	if err != nil {
		t.Fatalf("GetPlayerByID(conflicting) error = %v", err)
	}
	conflicting.OpenID = "dev:alice-renamed"
	if err := developmentStore.UpdatePlayer(conflicting); err == nil {
		t.Fatal("UpdatePlayer(conflicting OpenID) returned nil error")
	}
	if got, err := developmentStore.GetPlayerByOpenID("dev:bob"); err != nil || got.ID != second.ID {
		t.Fatalf("GetPlayerByOpenID(dev:bob) = %#v, %v; want ID %d", got, err, second.ID)
	}
	if got, err := developmentStore.GetPlayerByOpenID("dev:alice-renamed"); err != nil || got.ID != player.ID {
		t.Fatalf("GetPlayerByOpenID(dev:alice-renamed) = %#v, %v; want ID %d", got, err, player.ID)
	}

	if err := developmentStore.UpdatePlayer(&Player{ID: 999, OpenID: "dev:missing"}); !IsNotFound(err) {
		t.Fatalf("UpdatePlayer(missing) error = %v, want not found", err)
	}
	if _, err := developmentStore.GetPlayerByID(999); !IsNotFound(err) {
		t.Fatalf("GetPlayerByID(missing) error = %v, want not found", err)
	}
}

func TestMemoryDevelopmentStoreStoresAndRefreshesSessions(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStore()
	session := &SessionData{Uid: 1, Nickname: "player-1", Token: "token-a"}

	if err := developmentStore.SetSession(1, session); err != nil {
		t.Fatalf("SetSession() error = %v", err)
	}
	session.Token = "mutated caller"
	stored, err := developmentStore.GetSession(1)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if stored.Token != "token-a" {
		t.Fatalf("stored token = %q, want token-a", stored.Token)
	}

	stored.Token = "mutated getter"
	storedAgain, err := developmentStore.GetSession(1)
	if err != nil {
		t.Fatalf("GetSession(second) error = %v", err)
	}
	if storedAgain.Token != "token-a" {
		t.Fatalf("stored token after getter mutation = %q, want token-a", storedAgain.Token)
	}

	if err := developmentStore.DelSession(1); err != nil {
		t.Fatalf("DelSession() error = %v", err)
	}
	if err := developmentStore.DelSession(1); err != nil {
		t.Fatalf("DelSession(idempotent) error = %v", err)
	}
	missing, err := developmentStore.GetSession(1)
	if err != nil || missing != nil {
		t.Fatalf("GetSession(deleted) = %#v, %v; want nil, nil", missing, err)
	}
}

func TestMemoryDevelopmentStoreSavesAndLoadsArchive(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStore()
	if _, err := developmentStore.GetArchive(1); !IsNotFound(err) {
		t.Fatalf("GetArchive(missing) error = %v, want not found", err)
	}

	archive := &Archive{PlayerID: 1, Data: []byte(`{"coins":3}`)}
	if err := developmentStore.SaveArchive(archive); err != nil {
		t.Fatalf("SaveArchive() error = %v", err)
	}
	archive.Data = []byte(`{"coins":999}`)
	stored, err := developmentStore.GetArchive(1)
	if err != nil {
		t.Fatalf("GetArchive() error = %v", err)
	}
	if string(stored.Data) != `{"coins":3}` {
		t.Fatalf("GetArchive() data = %q, want exact saved data", stored.Data)
	}

	stored.Data = []byte(`{"coins":0}`)
	storedAgain, err := developmentStore.GetArchive(1)
	if err != nil {
		t.Fatalf("GetArchive(second) error = %v", err)
	}
	if string(storedAgain.Data) != `{"coins":3}` {
		t.Fatalf("GetArchive() data after getter mutation = %q, want exact saved data", storedAgain.Data)
	}
}

func TestMemoryDevelopmentStoreArchiveCopiesDataBackingArrays(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStore()
	callerData := []byte{1, 2, 3}
	if err := developmentStore.SaveArchive(&Archive{PlayerID: 1, Data: callerData}); err != nil {
		t.Fatal(err)
	}
	callerData[0] = 9
	loaded, err := developmentStore.GetArchive(1)
	if err != nil || loaded.Data[0] != 1 {
		t.Fatalf("saved data = %v, %v; want independent copy", loaded.Data, err)
	}
	loaded.Data[1] = 8
	again, err := developmentStore.GetArchive(1)
	if err != nil || again.Data[1] != 2 {
		t.Fatalf("loaded data = %v, %v; want independent copy", again.Data, err)
	}
}

func TestMemoryDevelopmentStoreConcurrentPlayerCreation(t *testing.T) {
	const playerCount = 32

	developmentStore := NewMemoryDevelopmentStore()
	ids := make([]int64, playerCount)
	errs := make([]error, playerCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(playerCount)

	for i := 0; i < playerCount; i++ {
		go func(index int) {
			defer waitGroup.Done()
			player := &Player{OpenID: fmt.Sprintf("dev:user-%d", index)}
			errs[index] = developmentStore.CreatePlayer(player)
			ids[index] = player.ID
		}(i)
	}
	waitGroup.Wait()

	seen := make(map[int64]struct{}, playerCount)
	for i := 0; i < playerCount; i++ {
		if errs[i] != nil {
			t.Fatalf("CreatePlayer(%d) error = %v", i, errs[i])
		}
		if ids[i] < 1 {
			t.Fatalf("CreatePlayer(%d) ID = %d, want positive", i, ids[i])
		}
		if _, exists := seen[ids[i]]; exists {
			t.Fatalf("duplicate player ID %d", ids[i])
		}
		seen[ids[i]] = struct{}{}
	}
}

func TestMemoryDevelopmentStoreRejectsNilInputs(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStore()

	if err := developmentStore.CreatePlayer(nil); err == nil {
		t.Fatal("CreatePlayer(nil) returned nil error")
	}
	if err := developmentStore.UpdatePlayer(nil); err == nil {
		t.Fatal("UpdatePlayer(nil) returned nil error")
	}
	if err := developmentStore.SetSession(1, nil); err == nil {
		t.Fatal("SetSession(nil) returned nil error")
	}
	if err := developmentStore.SaveArchive(nil); err == nil {
		t.Fatal("SaveArchive(nil) returned nil error")
	}
}

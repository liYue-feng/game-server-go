package store

import (
	"fmt"
	"sync"
)

type MemoryDevelopmentStore struct {
	mu               sync.RWMutex
	nextPlayerID     int64
	playersByID      map[int64]Player
	playerIDByOpenID map[string]int64
	sessionsByUID    map[int64]SessionData
	archivesByUID    map[int64]Archive
}

func NewMemoryDevelopmentStore() *MemoryDevelopmentStore {
	return &MemoryDevelopmentStore{
		nextPlayerID:     1,
		playersByID:      make(map[int64]Player),
		playerIDByOpenID: make(map[string]int64),
		sessionsByUID:    make(map[int64]SessionData),
		archivesByUID:    make(map[int64]Archive),
	}
}

func (s *MemoryDevelopmentStore) GetPlayerByID(id int64) (*Player, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	player, ok := s.playersByID[id]
	if !ok {
		return nil, fmt.Errorf("player %d: %w", id, ErrNotFound)
	}
	return &player, nil
}

func (s *MemoryDevelopmentStore) GetPlayerByOpenID(openID string) (*Player, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.playerIDByOpenID[openID]
	if !ok {
		return nil, fmt.Errorf("player open ID %q: %w", openID, ErrNotFound)
	}
	player, ok := s.playersByID[id]
	if !ok {
		return nil, fmt.Errorf("player open ID %q: %w", openID, ErrNotFound)
	}
	return &player, nil
}

func (s *MemoryDevelopmentStore) CreatePlayer(player *Player) error {
	if player == nil {
		return fmt.Errorf("create player: nil player")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.playerIDByOpenID[player.OpenID]; exists {
		return fmt.Errorf("player open ID %q already exists", player.OpenID)
	}

	player.ID = s.nextPlayerID
	s.nextPlayerID++

	stored := *player
	s.playersByID[stored.ID] = stored
	s.playerIDByOpenID[stored.OpenID] = stored.ID
	return nil
}

func (s *MemoryDevelopmentStore) UpdatePlayer(player *Player) error {
	if player == nil {
		return fmt.Errorf("update player: nil player")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, exists := s.playersByID[player.ID]
	if !exists {
		return fmt.Errorf("player %d: %w", player.ID, ErrNotFound)
	}
	if indexedID, indexed := s.playerIDByOpenID[player.OpenID]; indexed && indexedID != player.ID {
		return fmt.Errorf("player open ID %q already exists", player.OpenID)
	}

	updated := *player
	if stored.OpenID != updated.OpenID {
		delete(s.playerIDByOpenID, stored.OpenID)
	}
	s.playersByID[updated.ID] = updated
	s.playerIDByOpenID[updated.OpenID] = updated.ID
	return nil
}

func (s *MemoryDevelopmentStore) SetSession(uid int64, session *SessionData) error {
	if session == nil {
		return fmt.Errorf("set session: nil session")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessionsByUID[uid] = *session
	return nil
}

func (s *MemoryDevelopmentStore) GetSession(uid int64) (*SessionData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessionsByUID[uid]
	if !ok {
		return nil, nil
	}
	return &session, nil
}

func (s *MemoryDevelopmentStore) DelSession(uid int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessionsByUID, uid)
	return nil
}

func (s *MemoryDevelopmentStore) GetArchive(playerID int64) (*Archive, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	archive, ok := s.archivesByUID[playerID]
	if !ok {
		return nil, fmt.Errorf("archive for player %d: %w", playerID, ErrNotFound)
	}
	return &archive, nil
}

func (s *MemoryDevelopmentStore) SaveArchive(archive *Archive) error {
	if archive == nil {
		return fmt.Errorf("save archive: nil archive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.archivesByUID[archive.PlayerID] = *archive
	return nil
}

var (
	_ PlayerRepository  = (*MemoryDevelopmentStore)(nil)
	_ SessionRepository = (*MemoryDevelopmentStore)(nil)
	_ ArchiveRepository = (*MemoryDevelopmentStore)(nil)
)

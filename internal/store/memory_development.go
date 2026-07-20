package store

import (
	"fmt"
	"sync"

	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

type memorySettlementKey struct {
	playerID int64
	runID    string
}

type MemoryDevelopmentStore struct {
	mu               sync.RWMutex
	nextPlayerID     int64
	playersByID      map[int64]Player
	playerIDByOpenID map[string]int64
	sessionsByUID    map[int64]SessionData
	archivesByUID    map[int64]Archive
	settlementPolicy CombatRewardPolicy
	settlements      map[memorySettlementKey][]byte
	playerLevels     map[int64]int32
}

func NewMemoryDevelopmentStore() *MemoryDevelopmentStore {
	return NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{})
}

func NewMemoryDevelopmentStoreWithSettlementPolicy(policy CombatRewardPolicy) *MemoryDevelopmentStore {
	if err := validateCombatRewardPolicy(policy); err != nil {
		panic(err)
	}
	return &MemoryDevelopmentStore{
		nextPlayerID:     1,
		playersByID:      make(map[int64]Player),
		playerIDByOpenID: make(map[string]int64),
		sessionsByUID:    make(map[int64]SessionData),
		archivesByUID:    make(map[int64]Archive),
		settlementPolicy: policy,
		settlements:      make(map[memorySettlementKey][]byte),
		playerLevels:     make(map[int64]int32),
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
	archive.Data = append([]byte(nil), archive.Data...)
	return &archive, nil
}

func (s *MemoryDevelopmentStore) SaveArchive(archive *Archive) error {
	if archive == nil {
		return fmt.Errorf("save archive: nil archive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored := *archive
	stored.Data = append([]byte(nil), archive.Data...)
	s.archivesByUID[stored.PlayerID] = stored
	return nil
}

// Settle applies all changes and stores the first protobuf response under one mutex.
func (s *MemoryDevelopmentStore) Settle(playerID int64, req *protocolpb.CombatResultReq) (*protocolpb.CombatResultResp, error) {
	if req == nil {
		return nil, fmt.Errorf("settle combat: nil request")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := memorySettlementKey{playerID: playerID, runID: req.RunId}
	if stored, found := s.settlements[key]; found {
		response := &protocolpb.CombatResultResp{}
		if err := proto.Unmarshal(stored, response); err != nil {
			return nil, fmt.Errorf("decode stored settlement: %w", err)
		}
		response.Duplicate = true
		response.RunId = req.RunId
		return response, nil
	}

	archive := &protocolpb.PlayerArchive{SchemaVersion: 1}
	if stored, found := s.archivesByUID[playerID]; found {
		if err := proto.Unmarshal(stored.Data, archive); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedSettlementArchive, err)
		}
	}
	response, err := settleArchive(archive, req, s.settlementPolicy)
	if err != nil {
		return nil, err
	}
	archiveData, err := proto.Marshal(response.Archive)
	if err != nil {
		return nil, fmt.Errorf("encode settlement archive: %w", err)
	}
	responseData, err := proto.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode settlement response: %w", err)
	}

	s.archivesByUID[playerID] = Archive{PlayerID: playerID, Data: archiveData}
	if req.PlayerLevel > s.playerLevels[playerID] {
		s.playerLevels[playerID] = req.PlayerLevel
	}
	s.settlements[key] = responseData
	return response, nil
}

func (s *MemoryDevelopmentStore) GetDevelopmentPlayerLevel(playerID int64) (int32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if level := s.playerLevels[playerID]; level > 0 {
		return level, nil
	}
	return 1, nil
}

var (
	_ PlayerRepository                 = (*MemoryDevelopmentStore)(nil)
	_ SessionRepository                = (*MemoryDevelopmentStore)(nil)
	_ ArchiveRepository                = (*MemoryDevelopmentStore)(nil)
	_ CombatSettlementRepository       = (*MemoryDevelopmentStore)(nil)
	_ DevelopmentPlayerStatsRepository = (*MemoryDevelopmentStore)(nil)
)

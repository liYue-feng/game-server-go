package game

import (
	"errors"
	"fmt"

	"game-server/internal/protocolpb"
	"game-server/internal/store"

	"google.golang.org/protobuf/proto"
)

var (
	ErrInvalidArchiveRepositoryResult = errors.New("game: archive repository returned nil archive")
	ErrMalformedStoredArchive         = errors.New("game: malformed stored protobuf archive")
)

type ArchiveService struct{ archives store.ArchiveRepository }

func NewArchiveService(archives store.ArchiveRepository) *ArchiveService {
	return &ArchiveService{archives: archives}
}

func (s *ArchiveService) Save(uid int64, archive *protocolpb.PlayerArchive) error {
	if archive == nil {
		return errors.New("game: nil archive")
	}
	data, err := proto.Marshal(archive)
	if err != nil {
		return fmt.Errorf("marshal archive: %w", err)
	}
	return s.archives.SaveArchive(&store.Archive{PlayerID: uid, Data: data})
}

func (s *ArchiveService) Load(uid int64) (*protocolpb.PlayerArchive, bool, error) {
	archive, err := s.archives.GetArchive(uid)
	if store.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if archive == nil {
		return nil, false, ErrInvalidArchiveRepositoryResult
	}
	result := &protocolpb.PlayerArchive{}
	if err := proto.Unmarshal(archive.Data, result); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrMalformedStoredArchive, err)
	}
	return result, true, nil
}

package game

import "game-server/internal/store"

type ArchiveService struct {
	archives store.ArchiveRepository
}

func NewArchiveService(archives store.ArchiveRepository) *ArchiveService {
	return &ArchiveService{archives: archives}
}

func (s *ArchiveService) Save(uid int64, data string) error {
	return s.archives.SaveArchive(&store.Archive{PlayerID: uid, Data: data})
}

func (s *ArchiveService) Load(uid int64) (string, error) {
	archive, err := s.archives.GetArchive(uid)
	if store.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return archive.Data, nil
}

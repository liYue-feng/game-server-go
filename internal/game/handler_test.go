package game

import (
	"context"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"testing"
)

func TestHandlerSavesAndLoadsGeneratedArchive(t *testing.T) {
	handler := NewHandlerWithService(NewArchiveService(&archiveRepositoryStub{}))
	s := session.New(nil)
	s.Bind(42)
	ctx := session.WithSession(context.Background(), s)
	if _, err := handler.SaveArchive(ctx, &protocolpb.SaveArchiveReq{Archive: &protocolpb.PlayerArchive{Gold: 9}}); err != nil {
		t.Fatal(err)
	}
	response, err := handler.LoadArchive(ctx, &protocolpb.LoadArchiveReq{})
	if err != nil || !response.Found || response.Archive.GetGold() != 9 {
		t.Fatalf("LoadArchive() = %#v, %v", response, err)
	}
}

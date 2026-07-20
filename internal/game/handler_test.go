package game

import (
	"context"
	"errors"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/session"
)

func TestHandlerSaveAndLoadArchiveUseBoundSessionUID(t *testing.T) {
	repository := &archiveRepositoryStub{}
	handler := NewHandlerWithService(NewArchiveService(repository))
	kernelSession := session.New(nil)
	kernelSession.Bind(42)
	ctx := session.WithSession(context.Background(), kernelSession)

	saveResp, err := handler.SaveArchive(ctx, &protocol.SaveArchiveReq{Data: `{"gold":7}`})
	if err != nil || !saveResp.Success {
		t.Fatalf("SaveArchive() = %#v, %v; want success", saveResp, err)
	}
	if repository.archive == nil || repository.archive.PlayerID != 42 {
		t.Fatalf("stored archive = %#v, want player ID 42", repository.archive)
	}

	loadResp, err := handler.LoadArchive(ctx, &protocol.LoadArchiveReq{})
	if err != nil || loadResp.Data != `{"gold":7}` {
		t.Fatalf("LoadArchive() = %#v, %v; want stored data", loadResp, err)
	}
}

func TestHandlerMapsArchiveRepositoryFailures(t *testing.T) {
	kernelSession := session.New(nil)
	kernelSession.Bind(42)
	ctx := session.WithSession(context.Background(), kernelSession)

	t.Run("save", func(t *testing.T) {
		handler := NewHandlerWithService(NewArchiveService(&archiveRepositoryStub{saveErr: errors.New("save unavailable")}))
		_, err := handler.SaveArchive(ctx, &protocol.SaveArchiveReq{Data: "data"})
		assertBizErrorCode(t, err, protocol.ErrArchiveSaveFailed)
	})

	t.Run("load", func(t *testing.T) {
		handler := NewHandlerWithService(NewArchiveService(&archiveRepositoryStub{loadErr: errors.New("load unavailable")}))
		_, err := handler.LoadArchive(ctx, &protocol.LoadArchiveReq{})
		assertBizErrorCode(t, err, protocol.ErrInternal)
	})
}

func TestHandlerLoadArchiveReturnsEmptyForNotFound(t *testing.T) {
	handler := NewHandlerWithService(NewArchiveService(&archiveRepositoryStub{}))
	kernelSession := session.New(nil)
	kernelSession.Bind(1)

	resp, err := handler.LoadArchive(session.WithSession(context.Background(), kernelSession), &protocol.LoadArchiveReq{})
	if err != nil || resp.Data != "" {
		t.Fatalf("LoadArchive() = %#v, %v; want empty, nil", resp, err)
	}
}

func assertBizErrorCode(t *testing.T, err error, want int) {
	t.Helper()
	var bizErr *protocol.BizError
	if !errors.As(err, &bizErr) || bizErr.Code != want {
		t.Fatalf("error = %T %v, want BizError code %d", err, err, want)
	}
}

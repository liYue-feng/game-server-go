package game

import (
	"context"
	"errors"
	"strings"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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

func TestHandlerLoadArchiveLogsUIDAndDataLengthOnSuccess(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
	}{
		{name: "empty first load", data: ""},
		{name: "reload", data: strings.Repeat("x", 24)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logs := observeGlobalLogs(t)
			repository := &archiveRepositoryStub{}
			if tt.data != "" {
				repository.archive = &store.Archive{PlayerID: 42, Data: tt.data}
			}
			handler := NewHandlerWithService(NewArchiveService(repository))
			kernelSession := session.New(nil)
			kernelSession.Bind(42)

			if _, err := handler.LoadArchive(session.WithSession(context.Background(), kernelSession), &protocol.LoadArchiveReq{}); err != nil {
				t.Fatalf("LoadArchive() error = %v", err)
			}

			entries := logs.FilterMessage("加载存档成功").All()
			if len(entries) != 1 {
				t.Fatalf("success log count = %d, want 1; logs = %#v", len(entries), logs.AllUntimed())
			}
			fields := entries[0].ContextMap()
			if fields["uid"] != int64(42) || fields["dataLen"] != int64(len(tt.data)) {
				t.Fatalf("success log fields = %#v, want uid=42 dataLen=%d", fields, len(tt.data))
			}
		})
	}
}

func TestHandlerLoadArchiveFailureDoesNotLogSuccess(t *testing.T) {
	logs := observeGlobalLogs(t)
	handler := NewHandlerWithService(NewArchiveService(&archiveRepositoryStub{loadErr: errors.New("load unavailable")}))
	kernelSession := session.New(nil)
	kernelSession.Bind(42)

	_, err := handler.LoadArchive(session.WithSession(context.Background(), kernelSession), &protocol.LoadArchiveReq{})
	assertBizErrorCode(t, err, protocol.ErrInternal)
	if got := logs.FilterMessage("加载存档成功").Len(); got != 0 {
		t.Fatalf("success log count = %d after failed load, want 0", got)
	}
	if got := logs.FilterMessage("加载存档失败").Len(); got != 1 {
		t.Fatalf("failure log count = %d, want 1", got)
	}
}

func assertBizErrorCode(t *testing.T, err error, want int) {
	t.Helper()
	var bizErr *protocol.BizError
	if !errors.As(err, &bizErr) || bizErr.Code != want {
		t.Fatalf("error = %T %v, want BizError code %d", err, err, want)
	}
}

func observeGlobalLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)
	return logs
}

package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestInitConfiguresZapGlobalLogger(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "server.log")

	restoreGlobals := zap.ReplaceGlobals(zap.NewNop())
	t.Cleanup(restoreGlobals)
	t.Cleanup(Close)

	Init("info", logFile, 1, 1, 1)
	zap.L().Info("global logger reaches configured file")
	Sync()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read configured log file: %v", err)
	}
	if !strings.Contains(string(contents), "global logger reaches configured file") {
		t.Fatalf("configured log file did not receive zap.L output: %q", contents)
	}
}

func TestInitPreservesCallerForGlobalAndWrapperLoggers(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "server.log")

	restoreGlobals := zap.ReplaceGlobals(zap.NewNop())
	t.Cleanup(restoreGlobals)
	t.Cleanup(Close)

	Init("info", logFile, 1, 1, 1)
	logThroughZapGlobal()
	logThroughPackageWrapper()
	Sync()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read configured log file: %v", err)
	}

	callers := logCallers(t, contents)
	for _, message := range []string{"global caller", "wrapper caller"} {
		caller, ok := callers[message]
		if !ok {
			t.Fatalf("missing %q log entry", message)
		}
		if !strings.Contains(caller, "logger_test.go") {
			t.Errorf("%q caller = %q, want test call site", message, caller)
		}
		if strings.Contains(caller, "logger.go") {
			t.Errorf("%q caller = %q, must not point at package wrapper", message, caller)
		}
	}
}

func logThroughZapGlobal() {
	zap.L().Info("global caller")
}

func logThroughPackageWrapper() {
	Info("wrapper caller")
}

func logCallers(t *testing.T, contents []byte) map[string]string {
	t.Helper()
	callers := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		var entry struct {
			Caller  string `json:"caller"`
			Message string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		callers[entry.Message] = entry.Caller
	}
	return callers
}

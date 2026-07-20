package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

func TestInitRoutesGlobalAndWrapperLogsToConfiguredFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "server.log")
	restoreGlobals := zap.ReplaceGlobals(zap.NewNop())
	t.Cleanup(restoreGlobals)
	t.Cleanup(Close)

	Init("info", logFile, 1, 1, 1)
	zap.L().Info("global log entry")
	Info("wrapper log entry")
	Sync()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read configured log file: %v", err)
	}
	for _, message := range []string{"global log entry", "wrapper log entry"} {
		if !strings.Contains(string(contents), message) {
			t.Errorf("configured log file missing %q: %q", message, contents)
		}
	}
}

func TestInitPreservesCallerForGlobalAndWrapperLoggers(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "server.log")
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

func TestCloseIsIdempotentAndAllowsReinitialization(t *testing.T) {
	firstLog := filepath.Join(t.TempDir(), "first.log")
	secondLog := filepath.Join(t.TempDir(), "second.log")
	t.Cleanup(Close)

	Init("info", firstLog, 1, 1, 1)
	Info("first writer")
	Close()
	Close()

	Init("info", secondLog, 1, 1, 1)
	Info("second writer")
	Close()

	contents, err := os.ReadFile(secondLog)
	if err != nil {
		t.Fatalf("read second log file: %v", err)
	}
	if !strings.Contains(string(contents), "second writer") {
		t.Fatalf("second log missing reinitialized entry: %q", contents)
	}
}

func TestConcurrentInitCloseAndWrapperWritesRemainSafe(t *testing.T) {
	Close()
	baseline := zap.NewNop()
	restoreGlobals := zap.ReplaceGlobals(baseline)
	t.Cleanup(restoreGlobals)
	t.Cleanup(Close)

	const (
		lifecycleWorkers = 4
		writerWorkers    = 8
		iterations       = 250
	)
	start := make(chan struct{})
	panics := make(chan interface{}, (lifecycleWorkers+writerWorkers)*iterations)
	var wg sync.WaitGroup

	run := func(work func(int)) {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						panics <- recovered
					}
				}()
				work(i)
			}()
		}
	}

	for worker := 0; worker < lifecycleWorkers; worker++ {
		wg.Add(1)
		go run(func(i int) {
			Init("error", "", 1, 1, 1)
			if i%2 == 0 {
				Close()
			}
		})
	}
	for worker := 0; worker < writerWorkers; worker++ {
		worker := worker
		wg.Add(1)
		go run(func(i int) {
			Infof("concurrent logger write worker=%d iteration=%d", worker, i)
		})
	}

	close(start)
	wg.Wait()
	Close()
	close(panics)

	var recovered []interface{}
	for value := range panics {
		recovered = append(recovered, value)
	}
	if len(recovered) != 0 {
		t.Fatalf("concurrent logger lifecycle recovered %d panics, first: %v", len(recovered), recovered[0])
	}
	if got := zap.L(); got != baseline {
		t.Fatalf("zap global not restored after concurrent lifecycle: got %p, want %p", got, baseline)
	}
}

func TestCachedGlobalLoggerCannotReopenClosedLogFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "server.log")
	restoreGlobals := zap.ReplaceGlobals(zap.NewNop())
	t.Cleanup(restoreGlobals)
	t.Cleanup(Close)

	Init("info", logFile, 1, 1, 1)
	cached := zap.L()
	cached.Info("before close")
	Close()

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("cached logger panicked after Close: %v", recovered)
			}
		}()
		cached.Info("after close")
	}()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read closed log file: %v", err)
	}
	if !strings.Contains(string(contents), "before close") {
		t.Fatalf("closed log file missing pre-close entry: %q", contents)
	}
	if strings.Contains(string(contents), "after close") {
		t.Fatalf("cached logger reopened closed log file: %q", contents)
	}
}

func TestConcurrentReinitializationNeverExposesRestoredGlobal(t *testing.T) {
	Close()
	baseline := zap.NewNop()
	restoreGlobals := zap.ReplaceGlobals(baseline)
	t.Cleanup(restoreGlobals)
	t.Cleanup(Close)

	Init("error", "", 1, 1, 1)
	stop := make(chan struct{})
	var exposed atomic.Bool
	var observer sync.WaitGroup
	observer.Add(1)
	go func() {
		defer observer.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if zap.L() == baseline {
					exposed.Store(true)
				}
			}
		}
	}()

	for i := 0; i < 1000; i++ {
		Init("error", "", 1, 1, 1)
	}
	close(stop)
	observer.Wait()

	if exposed.Load() {
		t.Fatal("concurrent zap.L observed the restored global during reinitialization")
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

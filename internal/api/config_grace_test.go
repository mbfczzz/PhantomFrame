package api

import (
	"testing"
	"time"
)

func TestWorkerShutdownGraceParsing(t *testing.T) {
	for value, expected := range map[string]time.Duration{"": 20 * time.Second, "20s": 20 * time.Second, "45": 45 * time.Second, "1.5": 1500 * time.Millisecond, "1m": time.Minute, "0": 0} {
		t.Setenv("WORKER_SHUTDOWN_GRACE", value)
		t.Setenv("DATA_DIR", t.TempDir())
		c, err := LoadConfig()
		if err != nil || c.WorkerShutdownGrace != expected {
			t.Fatalf("%q -> %v, %v; want %v", value, c.WorkerShutdownGrace, err, expected)
		}
	}
	t.Setenv("WORKER_SHUTDOWN_GRACE", "soon")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("an invalid grace value must stop startup")
	}
}

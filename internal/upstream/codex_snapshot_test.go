package upstream

import (
	"net/http"
	"testing"
	"time"
)

func TestParseCodexSnapshot_NormalizesPrimaryAndSecondaryHeaders(t *testing.T) {
	headers := http.Header{
		"X-Codex-Primary-Used-Percent":          []string{"25"},
		"X-Codex-Primary-Reset-After-Seconds":   []string{"604800"},
		"X-Codex-Primary-Window-Minutes":        []string{"10080"},
		"X-Codex-Secondary-Used-Percent":        []string{"75"},
		"X-Codex-Secondary-Reset-After-Seconds": []string{"3600"},
		"X-Codex-Secondary-Window-Minutes":      []string{"300"},
	}

	snapshot := ParseCodexSnapshot(headers, time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC))
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Codex5hUsedPercent == nil || *snapshot.Codex5hUsedPercent != 75 {
		t.Fatalf("unexpected 5h usage: %#v", snapshot.Codex5hUsedPercent)
	}
	if snapshot.Codex7dUsedPercent == nil || *snapshot.Codex7dUsedPercent != 25 {
		t.Fatalf("unexpected 7d usage: %#v", snapshot.Codex7dUsedPercent)
	}
}

func TestParseCodexSnapshot_SupportsLegacy5hAnd7dHeaders(t *testing.T) {
	headers := http.Header{
		"X-Codex-5h-Used-Percent":         []string{"55"},
		"X-Codex-5h-Reset-After-Seconds":  []string{"1800"},
		"X-Codex-7d-Used-Percent":         []string{"20"},
		"X-Codex-7d-Reset-After-Seconds":  []string{"86400"},
	}

	snapshot := ParseCodexSnapshot(headers, time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC))
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Codex5hUsedPercent == nil || *snapshot.Codex5hUsedPercent != 55 {
		t.Fatalf("unexpected 5h usage: %#v", snapshot.Codex5hUsedPercent)
	}
	if snapshot.Codex7dUsedPercent == nil || *snapshot.Codex7dUsedPercent != 20 {
		t.Fatalf("unexpected 7d usage: %#v", snapshot.Codex7dUsedPercent)
	}
}

package upstream

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CodexSnapshot struct {
	Codex5hUsedPercent *float64
	Codex5hResetAt     *time.Time
	Codex7dUsedPercent *float64
	Codex7dResetAt     *time.Time
}

func ParseCodexSnapshot(headers http.Header, now time.Time) *CodexSnapshot {
	if headers == nil {
		return nil
	}

	if snapshot := parsePrimarySecondaryCodexSnapshot(headers, now); snapshot != nil {
		return snapshot
	}
	return parseLegacyCodexSnapshot(headers, now)
}

func parsePrimarySecondaryCodexSnapshot(headers http.Header, now time.Time) *CodexSnapshot {
	primaryUsed, hasPrimaryUsed := parseFloatHeader(headers, "X-Codex-Primary-Used-Percent")
	primaryReset, hasPrimaryReset := parseSecondsReset(headers, "X-Codex-Primary-Reset-After-Seconds", now)
	primaryWindow, hasPrimaryWindow := parseIntHeader(headers, "X-Codex-Primary-Window-Minutes")

	secondaryUsed, hasSecondaryUsed := parseFloatHeader(headers, "X-Codex-Secondary-Used-Percent")
	secondaryReset, hasSecondaryReset := parseSecondsReset(headers, "X-Codex-Secondary-Reset-After-Seconds", now)
	secondaryWindow, hasSecondaryWindow := parseIntHeader(headers, "X-Codex-Secondary-Window-Minutes")

	if !hasPrimaryUsed && !hasPrimaryReset && !hasPrimaryWindow && !hasSecondaryUsed && !hasSecondaryReset && !hasSecondaryWindow {
		return nil
	}

	result := &CodexSnapshot{}
	assignPrimaryTo5h := false
	assignPrimaryTo7d := false

	switch {
	case hasPrimaryWindow && hasSecondaryWindow:
		assignPrimaryTo5h = primaryWindow < secondaryWindow
		assignPrimaryTo7d = !assignPrimaryTo5h
	case hasPrimaryWindow:
		assignPrimaryTo5h = primaryWindow <= 360
		assignPrimaryTo7d = !assignPrimaryTo5h
	case hasSecondaryWindow:
		assignPrimaryTo7d = secondaryWindow <= 360
		assignPrimaryTo5h = !assignPrimaryTo7d
	default:
		assignPrimaryTo7d = true
	}

	if assignPrimaryTo5h {
		if hasPrimaryUsed {
			result.Codex5hUsedPercent = &primaryUsed
		}
		if hasPrimaryReset {
			result.Codex5hResetAt = &primaryReset
		}
		if hasSecondaryUsed {
			result.Codex7dUsedPercent = &secondaryUsed
		}
		if hasSecondaryReset {
			result.Codex7dResetAt = &secondaryReset
		}
	} else if assignPrimaryTo7d {
		if hasPrimaryUsed {
			result.Codex7dUsedPercent = &primaryUsed
		}
		if hasPrimaryReset {
			result.Codex7dResetAt = &primaryReset
		}
		if hasSecondaryUsed {
			result.Codex5hUsedPercent = &secondaryUsed
		}
		if hasSecondaryReset {
			result.Codex5hResetAt = &secondaryReset
		}
	}

	return result
}

func parseLegacyCodexSnapshot(headers http.Header, now time.Time) *CodexSnapshot {
	result := &CodexSnapshot{}
	hasAny := false

	if value, ok := parseFloatHeader(headers, "X-Codex-5h-Used-Percent"); ok {
		result.Codex5hUsedPercent = &value
		hasAny = true
	}
	if reset, ok := parseSecondsReset(headers, "X-Codex-5h-Reset-After-Seconds", now); ok {
		result.Codex5hResetAt = &reset
		hasAny = true
	}
	if value, ok := parseFloatHeader(headers, "X-Codex-7d-Used-Percent"); ok {
		result.Codex7dUsedPercent = &value
		hasAny = true
	}
	if reset, ok := parseSecondsReset(headers, "X-Codex-7d-Reset-After-Seconds", now); ok {
		result.Codex7dResetAt = &reset
		hasAny = true
	}
	if !hasAny {
		return nil
	}
	return result
}

func parseIntHeader(headers http.Header, key string) (int, bool) {
	value := strings.TrimSpace(headers.Get(key))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

package avatarsdkgo

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateLogIDFormat(t *testing.T) {
	now := time.Date(2025, time.October, 27, 14, 30, 34, 0, time.UTC)

	id, err := generateLogID(now)
	if err != nil {
		t.Fatalf("generateLogID returned error: %v", err)
	}

	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 {
		t.Fatalf("log ID %q should contain one underscore separator", id)
	}

	if parts[0] != "20251027143034" {
		t.Fatalf("unexpected timestamp prefix: got %q, want %q", parts[0], "20251027143034")
	}

	if len(parts[1]) != logIDNanoIDLength {
		t.Fatalf("unexpected nanoid length: got %d, want %d", len(parts[1]), logIDNanoIDLength)
	}

	if parts[1] == "" {
		t.Fatalf("nanoid suffix should not be empty")
	}
}

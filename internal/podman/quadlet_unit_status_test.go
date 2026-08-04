package podman

import (
	"testing"
	"time"
)

func TestUnitStatus_CacheReturnsCachedValueWithinTTL(t *testing.T) {
	t.Cleanup(func() {
		InvalidateUnitStatusCache("servlo-test")
		UnitLifecycle = nil
	})

	UnitLifecycle = nil
	InvalidateUnitStatusCache("servlo-test")

	// Seed the cache with a known value, then assert UnitStatus reads it
	// without falling through to the DBus path.
	unitStatusCacheMu.Lock()
	unitStatusCache["servlo-test"] = unitStatusEntry{state: "active", at: time.Now()}
	unitStatusCacheMu.Unlock()

	for i := 0; i < 5; i++ {
		got, err := UnitStatus("servlo-test")
		if err != nil {
			t.Fatalf("UnitStatus: %v", err)
		}
		if got != "active" {
			t.Errorf("call #%d: got %q, want active (cached value should be returned)", i, got)
		}
	}
}

func TestUnitStatus_StaleEntryIsReplaced(t *testing.T) {
	t.Cleanup(func() { InvalidateUnitStatusCache("servlo-expire") })

	UnitLifecycle = nil
	InvalidateUnitStatusCache("servlo-expire")

	unitStatusCacheMu.Lock()
	unitStatusCache["servlo-expire"] = unitStatusEntry{
		state: "active", at: time.Now().Add(-2 * unitStatusCacheTTL),
	}
	unitStatusCacheMu.Unlock()

	_, _ = UnitStatus("servlo-expire")

	unitStatusCacheMu.Lock()
	entry := unitStatusCache["servlo-expire"]
	unitStatusCacheMu.Unlock()
	if time.Since(entry.at) > unitStatusCacheTTL {
		t.Error("expected stale entry to be replaced after TTL expiry")
	}
}

func TestInvalidateUnitStatusCache_DropsEntry(t *testing.T) {
	UnitLifecycle = nil
	t.Cleanup(func() { InvalidateUnitStatusCache("servlo-invalidate") })

	unitStatusCacheMu.Lock()
	unitStatusCache["servlo-invalidate"] = unitStatusEntry{state: "active", at: time.Now()}
	unitStatusCacheMu.Unlock()

	InvalidateUnitStatusCache("servlo-invalidate")

	unitStatusCacheMu.Lock()
	_, exists := unitStatusCache["servlo-invalidate"]
	unitStatusCacheMu.Unlock()
	if exists {
		t.Error("expected entry to be removed after InvalidateUnitStatusCache")
	}
}

package serviceops

import (
	"testing"
	"time"
)

func TestUpdateAvailabilityCache_storeAndRetrieve(t *testing.T) {
	t.Cleanup(func() { invalidateUpdateAvailability("svc") })

	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("expected nil before store, got %+v", got)
	}

	want := &UpdateAvailability{Service: "svc", LatestTag: "1.2.3"}
	storeUpdateAvailability("svc", want)

	got := cachedUpdateAvailability("svc")
	if got == nil || got.LatestTag != "1.2.3" {
		t.Fatalf("expected cached entry with tag 1.2.3, got %+v", got)
	}
}

func TestUpdateAvailabilityCache_invalidate(t *testing.T) {
	storeUpdateAvailability("svc", &UpdateAvailability{Service: "svc", LatestTag: "9.9"})
	invalidateUpdateAvailability("svc")

	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("expected nil after invalidate, got %+v", got)
	}
}

func TestUpdateAvailabilityCache_expiresAfterTTL(t *testing.T) {
	t.Cleanup(func() { invalidateUpdateAvailability("svc") })

	updateAvailMu.Lock()
	updateAvailCache["svc"] = updateAvailabilityEntry{
		value: &UpdateAvailability{Service: "svc"},
		at:    time.Now().Add(-2 * updateAvailabilityTTL),
	}
	updateAvailMu.Unlock()

	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("expected nil after TTL expiry, got %+v", got)
	}
}

func TestUpdateAvailabilityCache_storeNilIsNoop(t *testing.T) {
	t.Cleanup(func() { invalidateUpdateAvailability("svc") })

	storeUpdateAvailability("svc", nil)
	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("storing nil should be no-op, got %+v", got)
	}
}

// RefreshUpdateAvailability must drop the cached entry even when the service
// is unknown to resolveServiceForUpdate (so a stale cached entry from a
// previous run can't survive an explicit user-triggered refresh).
func TestRefreshUpdateAvailability_DropsInMemoryCache(t *testing.T) {
	const name = "definitely-not-a-real-service"
	t.Cleanup(func() { invalidateUpdateAvailability(name) })

	storeUpdateAvailability(name, &UpdateAvailability{Service: name, LatestTag: "stale"})
	if got := cachedUpdateAvailability(name); got == nil {
		t.Fatalf("setup: expected entry present before refresh")
	}

	_, _ = RefreshUpdateAvailability(name)

	if got := cachedUpdateAvailability(name); got != nil {
		t.Fatalf("expected cache empty after RefreshUpdateAvailability, got %+v", got)
	}
}

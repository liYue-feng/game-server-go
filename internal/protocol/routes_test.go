package protocol

import "testing"

func TestCanonicalRouteRegistryCoversWebSocketPairsAndAllMessageIDs(t *testing.T) {
	routes := Routes()
	if len(routes) != 15 {
		t.Fatalf("routes = %d, want 15", len(routes))
	}
	seen := map[uint16]struct{}{}
	for _, route := range routes {
		if _, exists := seen[route.RequestID]; exists {
			t.Fatalf("duplicate request ID %d", route.RequestID)
		}
		seen[route.RequestID] = struct{}{}
		if got, ok := RouteFor(route.RequestID); !ok || got.ResponseID != route.ResponseID {
			t.Fatalf("route lookup mismatch for %d", route.RequestID)
		}
	}
	ids := AllMessageIDs()
	if len(ids) != 32 {
		t.Fatalf("message IDs = %d, want 32", len(ids))
	}
	for _, id := range ids {
		if id == 0 {
			t.Fatal("message ID 0 is not a production ID")
		}
	}
}

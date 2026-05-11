package ovh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOVHServer mimics the four OVH endpoints used by MetadataFetcher.Fetch.
//
// Verified response shapes (eu.api.ovh.com/1.0/dedicated/server.json):
//   - GET .../networkInterfaceController → macAddress[] ([]string)
//   - GET .../networkInterfaceController/{mac} → {mac, linkType, virtualNetworkInterface}
//     linkType enum: isolated|private|private_lag|provisioning|provisioning_lag|public|public_lag
//   - GET .../specifications/hardware → {diskGroups: [{diskType, numberOfDisks, diskSize: {value, unit}, ...}]}
//     diskType enum: documented as NVMe|SAS|SATA|SSD|Unknown but real
//     responses use upper case (e.g. "NVME" for advance-1 BHS hardware).
//     Fixtures here mirror real responses, not the doc enum, so callers
//     downstream (SynthesizeDiskPaths) are exercised against actual API
//     casing.
//   - GET .../specifications/network → {routing: {ipv4: {network, ip, gateway}, ipv6: {...}}, ...}
//     The routing.ipv4.gateway is OVH's actual point-to-point gateway
//     (typically in the 100.64.0.0/10 CGN range, NOT in the same /24 as
//     the host's public IP). On-the-wire the host uses /32 routing
//     regardless of what routing.ipv4.network reports. We mirror real
//     responses here.
func fakeOVHServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// GET /dedicated/server/{serverID}/networkInterfaceController
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"AA:BB:CC:00:00:01", "11:22:33:00:00:01"})
	})

	// GET /dedicated/server/{serverID}/networkInterfaceController/{mac}
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/AA:BB:CC:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "public", "mac": "AA:BB:CC:00:00:01"})
		})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/11:22:33:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "private", "mac": "11:22:33:00:00:01"})
		})

	// GET /dedicated/server/{serverID}/specifications/hardware
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/hardware", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"diskGroups": []map[string]any{
				{"diskType": "NVME", "diskSize": map[string]any{"value": 512, "unit": "GB"}, "numberOfDisks": 2},
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	})

	// GET /dedicated/server/{serverID}/specifications/network
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/network", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"routing": map[string]any{
				"ipv4": map[string]any{
					"network": "192.0.2.0/24",
					"ip":      "192.0.2.14",
					"gateway": "100.64.0.1",
				},
				"ipv6": map[string]any{
					"network": "2607:5300:219::/48",
					"ip":      "2607:5300:0219:d600::/56",
					"gateway": "fe80::1",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	})

	return httptest.NewServer(mux)
}

func TestMetadataFetcher_Fetch(t *testing.T) {
	srv := fakeOVHServer(t)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}
	f := &MetadataFetcher{Client: c}

	got, err := f.Fetch(context.Background(), "ns1.example.net")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PublicMAC != "AA:BB:CC:00:00:01" {
		t.Errorf("PublicMAC = %q, want AA:BB:CC:00:00:01", got.PublicMAC)
	}
	if got.PrivateMAC != "11:22:33:00:00:01" {
		t.Errorf("PrivateMAC = %q, want 11:22:33:00:00:01", got.PrivateMAC)
	}
	if len(got.Disks) != 2 {
		t.Fatalf("got %d disks, want 2", len(got.Disks))
	}
	if got.Disks[0].Type != "NVME" || got.Disks[0].CapacityGB != 512 {
		t.Errorf("Disks[0] = %+v, want {NVME 512}", got.Disks[0])
	}
}

// OVH dedicated servers use point-to-point routing: the gateway lives in
// the 100.64.0.0/10 CGN range, NOT in the same /24 as the host's public
// IP, and the host's address is routed as /32. networkd needs the actual
// gateway from the API plus GatewayOnlink=yes to install the default
// route — without this fetch, plasmactl-node would fall through to the
// Scaleway-pattern derivation (gateway = public_ip[:3] + ".1") and the
// resulting Flatcar would have no working public route.
func TestMetadataFetcher_Fetch_Routing(t *testing.T) {
	srv := fakeOVHServer(t)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}
	f := &MetadataFetcher{Client: c}

	got, err := f.Fetch(context.Background(), "ns1.example.net")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PublicGateway != "100.64.0.1" {
		t.Errorf("PublicGateway = %q, want 100.64.0.1 (from /specifications/network routing.ipv4.gateway)", got.PublicGateway)
	}
	if got.PublicPrefix != 32 {
		t.Errorf("PublicPrefix = %d, want 32 (OVH dedicated routes individual /32s on host regardless of routing.ipv4.network)", got.PublicPrefix)
	}
}

func TestMetadataFetcher_Fetch_TBSize_ConvertsToGB(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"AA:BB:CC:00:00:01", "11:22:33:00:00:01"})
	})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/AA:BB:CC:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "public", "mac": "AA:BB:CC:00:00:01"})
		})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/11:22:33:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "private", "mac": "11:22:33:00:00:01"})
		})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/hardware", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"diskGroups": []map[string]any{
				{"diskType": "SATA", "diskSize": map[string]any{"value": 2, "unit": "TB"}, "numberOfDisks": 1},
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/network", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"routing": map[string]any{"ipv4": map[string]any{"gateway": "100.64.0.1"}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}
	f := &MetadataFetcher{Client: c}
	got, err := f.Fetch(context.Background(), "ns1.example.net")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Disks[0].CapacityGB != 2048 {
		t.Errorf("CapacityGB = %d, want 2048 (2 TB → GB)", got.Disks[0].CapacityGB)
	}
}

func TestMetadataFetcher_Fetch_MissingPublicMAC_Errors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"11:22:33:00:00:01"})
	})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/11:22:33:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "private", "mac": "11:22:33:00:00:01"})
		})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/hardware", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"diskGroups": []map[string]any{}})
	})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/network", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"routing": map[string]any{"ipv4": map[string]any{"gateway": "100.64.0.1"}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}
	f := &MetadataFetcher{Client: c}
	_, err := f.Fetch(context.Background(), "ns1.example.net")
	if err == nil {
		t.Fatal("expected error when no public-linkType interface present")
	}
}

// TestMetadataFetcher_Fetch_PrivateLAG verifies that private_lag linkType is
// also accepted as the private MAC (OVH uses private_lag on LAG-bonded servers).
func TestMetadataFetcher_Fetch_PrivateLAG(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"AA:BB:CC:00:00:01", "11:22:33:00:00:01"})
	})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/AA:BB:CC:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "public", "mac": "AA:BB:CC:00:00:01"})
		})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/networkInterfaceController/11:22:33:00:00:01",
		func(w http.ResponseWriter, r *http.Request) {
			// private_lag is a valid linkType for LAG-bonded private interfaces
			_ = json.NewEncoder(w).Encode(map[string]string{"linkType": "private_lag", "mac": "11:22:33:00:00:01"})
		})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/hardware", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"diskGroups": []map[string]any{}})
	})
	mux.HandleFunc("/1.0/dedicated/server/ns1.example.net/specifications/network", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"routing": map[string]any{"ipv4": map[string]any{"gateway": "100.64.0.1"}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}
	f := &MetadataFetcher{Client: c}
	got, err := f.Fetch(context.Background(), "ns1.example.net")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PrivateMAC != "11:22:33:00:00:01" {
		t.Errorf("PrivateMAC = %q, want 11:22:33:00:00:01", got.PrivateMAC)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(ErrNotFound{Path: "/foo"}) {
		t.Error("IsNotFound returned false for direct ErrNotFound")
	}
	wrapped := fmt.Errorf("wrap: %w", ErrNotFound{Path: "/foo"})
	if !IsNotFound(wrapped) {
		t.Error("IsNotFound returned false for wrapped ErrNotFound (errors.As contract)")
	}
	if IsNotFound(fmt.Errorf("nope")) {
		t.Error("IsNotFound returned true for unrelated error")
	}
}

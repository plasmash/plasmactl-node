package ovh

// Verified OVH vRack API shapes (eu.api.ovh.com/1.0/vrack.json +
// eu.api.ovh.com/1.0/dedicated/server.json — fetched 2026-04-29):
//
//   GET  /vrack                                                    → string[]
//   GET  /vrack/{serviceName}                                      → {description, name, serviceName, ...}
//   GET  /dedicated/server/{sn}/virtualNetworkInterface?mode=vrack → uuid[]
//   GET  /dedicated/server/{sn}/virtualNetworkInterface/{uuid}     → {uuid, mode, networkInterfaceController: macAddress[], vrack, enabled, name, ...}
//   GET  /vrack/{serviceName}/dedicatedServerInterface             → string[] (UUIDs of VNIs currently in the vRack)
//   POST /vrack/{serviceName}/dedicatedServerInterface             → vrack.Task   body: {dedicatedServerInterface: <uuid>}
//   DELETE /vrack/{serviceName}/dedicatedServerInterface/{uuid}    → vrack.Task
//   GET  /vrack/{serviceName}/task/{taskId}                        → {id, status, function, ...}
//
// Key differences from task description's assumptions:
//   1. Parameter is a VNI UUID (not a MAC address).
//   2. No "vlan" field in POST body or GET response — VLAN is an OS-level concept.
//   3. "dedicatedServerInterfaceDetails" is GET-only list (returns {dedicatedServer, dedicatedServerInterface, name}).
//   4. Membership check is GET /dedicatedServerInterface (returns list of UUIDs), not a per-MAC GET.
//   5. The MAC→UUID lookup goes through /dedicated/server/{sn}/virtualNetworkInterface.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// vrackState drives the fake server's responses.
type vrackState struct {
	vrackList    []string
	descriptions map[string]string // serviceName → description (legacy field)
	names        map[string]string // serviceName → name (newer "Name" column in OVH UI)
	// vnisByServer: serverID → list of VNIs (each has UUID + MACs)
	vnisByServer map[string][]fakeVNI
	// vrackMembers: serviceName → set of VNI UUIDs currently in the vRack
	vrackMembers map[string]map[string]bool
	postCalls    []postCall
	deleteCalls  []string
}

type fakeVNI struct {
	UUID string
	MACs []string // networkInterfaceController
}

type postCall struct {
	serviceName string
	uuid        string
}

func newVRackServer(t *testing.T, state *vrackState) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// GET /1.0/vrack → []string
	mux.HandleFunc("/1.0/vrack", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/vrack" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(state.vrackList)
	})

	// GET /1.0/vrack/{serviceName} → {description, name, ...}
	// Iterate vrackList so vRacks with name-only (no description) are still served.
	for _, sn := range state.vrackList {
		sn := sn
		desc := state.descriptions[sn]
		name := state.names[sn]
		mux.HandleFunc("/1.0/vrack/"+sn, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/1.0/vrack/"+sn {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"description": desc, "name": name, "serviceName": sn})
		})

		// GET  /1.0/vrack/{serviceName}/dedicatedServerInterface → []string (UUIDs)
		// POST /1.0/vrack/{serviceName}/dedicatedServerInterface → vrack.Task
		mux.HandleFunc("/1.0/vrack/"+sn+"/dedicatedServerInterface", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/1.0/vrack/"+sn+"/dedicatedServerInterface" {
				http.NotFound(w, r)
				return
			}
			switch r.Method {
			case http.MethodGet:
				members := state.vrackMembers[sn]
				uuids := make([]string, 0, len(members))
				for uuid := range members {
					uuids = append(uuids, uuid)
				}
				_ = json.NewEncoder(w).Encode(uuids)
			case http.MethodPost:
				var body struct {
					DedicatedServerInterface string `json:"dedicatedServerInterface"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				state.postCalls = append(state.postCalls, postCall{sn, body.DedicatedServerInterface})
				if state.vrackMembers[sn] == nil {
					state.vrackMembers[sn] = map[string]bool{}
				}
				state.vrackMembers[sn][body.DedicatedServerInterface] = true
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "status": "todo"})
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})

		// DELETE /1.0/vrack/{serviceName}/dedicatedServerInterface/{uuid}
		mux.HandleFunc("/1.0/vrack/"+sn+"/dedicatedServerInterface/", func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/1.0/vrack/"+sn+"/dedicatedServerInterface/"), "/")
			uuid := parts[0]
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			state.deleteCalls = append(state.deleteCalls, uuid)
			delete(state.vrackMembers[sn], uuid)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "status": "todo"})
		})

		// GET /1.0/vrack/{serviceName}/task/12345 → done instantly
		mux.HandleFunc("/1.0/vrack/"+sn+"/task/12345", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "status": "done"})
		})
	}

	// /dedicated/server/{serverID}/virtualNetworkInterface  (list + per-UUID)
	for serverID, vnis := range state.vnisByServer {
		serverID, vnis := serverID, vnis
		mux.HandleFunc("/1.0/dedicated/server/"+serverID+"/virtualNetworkInterface", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/1.0/dedicated/server/"+serverID+"/virtualNetworkInterface" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("mode") != "vrack" {
				t.Errorf("expected ?mode=vrack query, got %q", r.URL.RawQuery)
			}
			uuids := make([]string, len(vnis))
			for i, v := range vnis {
				uuids[i] = v.UUID
			}
			_ = json.NewEncoder(w).Encode(uuids)
		})
		for _, vni := range vnis {
			vni := vni
			mux.HandleFunc("/1.0/dedicated/server/"+serverID+"/virtualNetworkInterface/"+vni.UUID, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"uuid":                     vni.UUID,
					"mode":                     "vrack",
					"networkInterfaceController": vni.MACs,
					"enabled":                  true,
				})
			})
		}
	}

	return httptest.NewServer(mux)
}

// TestVRack_NotMember_AddsToVRack verifies that a server whose private VNI is
// not yet in the vRack gets added via POST.
func TestVRack_NotMember_AddsToVRack(t *testing.T) {
	const vniUUID = "aaaaaaaa-0000-0000-0000-000000000001"
	state := &vrackState{
		vrackList:    []string{"pn-12345"},
		descriptions: map[string]string{"pn-12345": "example-plat"},
		vnisByServer: map[string][]fakeVNI{
			"ns1.example.net": {{UUID: vniUUID, MACs: []string{"11:22:33:00:00:01"}}},
		},
		vrackMembers: map[string]map[string]bool{"pn-12345": {}},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	if err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(state.postCalls) != 1 || state.postCalls[0].uuid != vniUUID {
		t.Fatalf("postCalls = %+v, want one POST with uuid %s", state.postCalls, vniUUID)
	}
	if len(state.deleteCalls) != 0 {
		t.Fatalf("unexpected DELETEs: %v", state.deleteCalls)
	}
}

// TestVRack_AlreadyMember_NoOp verifies that no mutations occur when the VNI
// is already present in the vRack.
func TestVRack_AlreadyMember_NoOp(t *testing.T) {
	const vniUUID = "aaaaaaaa-0000-0000-0000-000000000001"
	state := &vrackState{
		vrackList:    []string{"pn-12345"},
		descriptions: map[string]string{"pn-12345": "example-plat"},
		vnisByServer: map[string][]fakeVNI{
			"ns1.example.net": {{UUID: vniUUID, MACs: []string{"11:22:33:00:00:01"}}},
		},
		vrackMembers: map[string]map[string]bool{"pn-12345": {vniUUID: true}},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	if err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(state.postCalls)+len(state.deleteCalls) != 0 {
		t.Fatalf("expected no mutations, got post=%v delete=%v", state.postCalls, state.deleteCalls)
	}
}

// TestVRack_MatchByName_Succeeds verifies that a vRack whose `name` field
// (not `description`) matches platformName is selected. This mirrors the
// OVH UI's "Name" column, which is the field operators populate today.
// Description is empty (legacy vRacks may have used it; new ones often don't).
func TestVRack_MatchByName_Succeeds(t *testing.T) {
	const vniUUID = "aaaaaaaa-0000-0000-0000-000000000001"
	state := &vrackState{
		vrackList:    []string{"pn-1234560"},
		descriptions: map[string]string{"pn-1234560": ""},        // empty in OVH UI
		names:        map[string]string{"pn-1234560": "example-plat"}, // operator-set name
		vnisByServer: map[string][]fakeVNI{
			"ns1.example.net": {{UUID: vniUUID, MACs: []string{"11:22:33:00:00:01"}}},
		},
		vrackMembers: map[string]map[string]bool{"pn-1234560": {}},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	if err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(state.postCalls) != 1 || state.postCalls[0].uuid != vniUUID {
		t.Fatalf("postCalls = %+v, want one POST with uuid %s", state.postCalls, vniUUID)
	}
}

// TestVRack_MatchByDescriptionWhenNameSet verifies legacy fallback: a vRack
// with non-empty description matching platformName is still selected even when
// `name` is set to something different. Belt-and-braces — protects platforms
// that adopted the description-based convention.
func TestVRack_MatchByDescriptionWhenNameSet(t *testing.T) {
	const vniUUID = "aaaaaaaa-0000-0000-0000-000000000001"
	state := &vrackState{
		vrackList:    []string{"pn-X"},
		descriptions: map[string]string{"pn-X": "example-plat"},
		names:        map[string]string{"pn-X": "some-other-name"},
		vnisByServer: map[string][]fakeVNI{
			"ns1.example.net": {{UUID: vniUUID, MACs: []string{"11:22:33:00:00:01"}}},
		},
		vrackMembers: map[string]map[string]bool{"pn-X": {}},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	if err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(state.postCalls) != 1 || state.postCalls[0].uuid != vniUUID {
		t.Fatalf("postCalls = %+v, want one POST with uuid %s", state.postCalls, vniUUID)
	}
}

// TestVRack_NoMatchByDescription_Errors verifies that an error is returned when
// neither description nor name matches the requested platform name.
func TestVRack_NoMatchByDescription_Errors(t *testing.T) {
	state := &vrackState{
		vrackList:    []string{"pn-99999"},
		descriptions: map[string]string{"pn-99999": "different-platform"},
		vnisByServer: map[string][]fakeVNI{},
		vrackMembers: map[string]map[string]bool{},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01")
	if err == nil {
		t.Fatal("expected error when no vRack matches platform name")
	}
	if !strings.Contains(err.Error(), "no vRack with description or name") {
		t.Fatalf("err = %q, want 'no vRack with description or name'", err.Error())
	}
}

// TestVRack_MultipleMatches_Errors verifies that an error is returned when
// multiple vRacks share the same description (ambiguous).
func TestVRack_MultipleMatches_Errors(t *testing.T) {
	state := &vrackState{
		vrackList:    []string{"pn-A", "pn-B"},
		descriptions: map[string]string{"pn-A": "example-plat", "pn-B": "example-plat"},
		vnisByServer: map[string][]fakeVNI{},
		vrackMembers: map[string]map[string]bool{},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01")
	if err == nil {
		t.Fatal("expected error when multiple vRacks match")
	}
}

// TestVRack_VNINotFound_Errors verifies that an error is returned when no VNI
// on the server has the requested private MAC.
func TestVRack_VNINotFound_Errors(t *testing.T) {
	state := &vrackState{
		vrackList:    []string{"pn-12345"},
		descriptions: map[string]string{"pn-12345": "example-plat"},
		vnisByServer: map[string][]fakeVNI{
			"ns1.example.net": {{UUID: "aaaaaaaa-0000-0000-0000-000000000001", MACs: []string{"AA:BB:CC:00:00:01"}}},
		},
		vrackMembers: map[string]map[string]bool{"pn-12345": {}},
	}
	srv := newVRackServer(t, state)
	defer srv.Close()

	r := &VRackReconciler{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/1.0"}}
	err := r.Reconcile(context.Background(), "example-plat", 7, "ns1.example.net", "11:22:33:00:00:01")
	if err == nil {
		t.Fatal("expected error when no VNI has the requested MAC")
	}
	if !strings.Contains(err.Error(), "no vrack-mode VNI") {
		t.Fatalf("err = %q, want 'no vrack-mode VNI'", err.Error())
	}
}

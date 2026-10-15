package v1

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The same vector is checked by the app (companion_peer_ticket_test.dart),
// so phone A accepts exactly what the server mints.
func TestCompanionPeerTicketVector(t *testing.T) {
	got := companionPeerTicket("s3cret", "phoneA", "phoneB", "read", "/storage/emulated/0/DCIM/x.mp4", time.Unix(1800000000, 0))
	const want = "djEKMTgwMDAwMDAwMApyZWFkCnBob25lQgovc3RvcmFnZS9lbXVsYXRlZC8wL0RDSU0veC5tcDQ.12217092fc8e73bf0fe26a24db9c069536591a34335071a8303872f46352e180"
	if got != want {
		t.Fatalf("ticket %s, want %s", got, want)
	}
}

func peerPhone(t *testing.T, tailnet bool) (*relayPhone, string) {
	t.Helper()
	_, base := useTempCompanionState(t)
	useLoopbackAddressKinds(t)
	p := newRelayPhone("A")
	p.files[relayRoot+"/DCIM/x.mp4"] = 10
	port := freePort(t)
	listenOn(t, p, port, "127.0.0.1")
	var addrs []string
	if tailnet {
		listenOn(t, p, port, "127.0.0.2")
		addrs = []string{"127.0.0.2"}
	}
	companionMu.Lock()
	companionLoaded = true
	companionDevices["A"] = &CompanionDevice{ID: "A", Name: "A", OwnerUserID: "1", Secret: p.secret, Port: port, IP: "127.0.0.1", Addresses: addrs,
		StoragePath: filepath.Join(base, "A"), RootPath: relayRoot, LastSeen: time.Now()}
	companionMu.Unlock()
	return p, filepath.Join(base, "A")
}

func peerTicketReq(t *testing.T, uid int, body map[string]string) (int, map[string]interface{}) {
	t.Helper()
	rec := companionReq(t, PostCompanionPeerTicket, http.MethodPost, "/v1/companion/A/peer-ticket", uid, "A", body)
	var res struct {
		Data map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec.Code, res.Data
}

func TestCompanionPeerTicket(t *testing.T) {
	p, dir := peerPhone(t, true)
	code, data := peerTicketReq(t, 1, map[string]string{"path": dir + "/DCIM/x.mp4", "op": "read"})
	if code != 200 {
		t.Fatalf("owner: %d %v", code, data)
	}
	port := strconv.Itoa(companionDevices["A"].Port)
	if c, _ := data["candidates"].([]interface{}); len(c) != 1 || c[0] != "127.0.0.2:"+port {
		t.Fatalf("candidates %v: want only the Tailscale address", data["candidates"])
	}
	if data["path"] != relayRoot+"/DCIM/x.mp4" {
		t.Fatalf("path %v", data["path"])
	}
	exp, _ := time.Parse(time.RFC3339, data["expires_at"].(string))
	if d := time.Until(exp); d < 119*time.Minute || d > 121*time.Minute {
		t.Fatalf("expires in %s, want 2 h", d)
	}
	if want := companionPeerTicket(p.secret, "A", "", "read", relayRoot+"/DCIM/x.mp4", exp); data["ticket"] != want {
		t.Fatalf("ticket %v, want %s", data["ticket"], want)
	}

	for name, tc := range map[string]struct {
		uid  int
		body map[string]string
		want int
	}{
		"another user":      {2, map[string]string{"path": dir + "/DCIM/x.mp4", "op": "read"}, 404},
		"write op":          {1, map[string]string{"path": dir + "/DCIM/x.mp4", "op": "write"}, 400},
		"escapes the phone": {1, map[string]string{"path": dir + "/../B/x.mp4", "op": "read"}, 400},
		"outside the root":  {1, map[string]string{"path": relayRoot + "/../../data/x", "op": "list"}, 400},
		"phone path":        {1, map[string]string{"path": relayRoot + "/DCIM", "op": "list"}, 200},
	} {
		if code, data := peerTicketReq(t, tc.uid, tc.body); code != tc.want {
			t.Errorf("%s: %d %v, want %d", name, code, data, tc.want)
		}
	}
}

func TestCompanionPeerTicketNeedsTailscale(t *testing.T) {
	_, dir := peerPhone(t, false)
	if code, _ := peerTicketReq(t, 1, map[string]string{"path": dir + "/DCIM/x.mp4", "op": "read"}); code != http.StatusConflict {
		t.Fatalf("no Tailscale address: %d, want 409 (use the relay)", code)
	}
}

package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
)

type wsPeers struct{ service.PeerService }

func (wsPeers) GetPeers() []model2.PeerDriveDBModel              { return nil }
func (wsPeers) CreatePeer(*model2.PeerDriveDBModel)              {}
func (wsPeers) GetPeerByName(string) (m model2.PeerDriveDBModel) { return }

type wsRepo struct{ service.Repository }

func (wsRepo) Peer() service.PeerService { return wsPeers{} }

// A plain GET (no WebSocket upgrade) to /v1/file/ws used to carry on with a
// nil conn and register a client whose pump goroutines then panicked,
// taking the whole core process down.
func TestConnectWebSocketPlainGETDoesNotCrash(t *testing.T) {
	old := service.MyService
	service.MyService = wsRepo{}
	t.Cleanup(func() { service.MyService = old })

	e := NewEcho()
	e.GET("/ws", ConnectWebSocket)
	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400 from the failed upgrade", resp.StatusCode)
	}
	time.Sleep(200 * time.Millisecond) // let any pump goroutine run (and panic)
}

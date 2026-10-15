package external

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

// Calls to other NivaroOS services carry the local token (H1): a bare
// loopback connection is no longer enough.
func TestLocalCallSendsToken(t *testing.T) {
	old := localtoken.Path
	localtoken.Path = filepath.Join(t.TempDir(), "local-automation.token")
	t.Cleanup(func() { localtoken.Path = old })

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(localtoken.Header)
	}))
	defer srv.Close()

	res, err := localCall(http.MethodPost, srv.URL+"/v1/notify/x", []byte("{}"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if want := localtoken.Get(); want == "" || got != want {
		t.Fatalf("token sent = %q, want %q", got, want)
	}
}

package pkg

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckURL(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	down := httptest.NewServer(nil)
	down.Close()

	if err := CheckURL(ok.URL); err != nil {
		t.Errorf("200: got %v, want nil", err)
	}
	if err := CheckURL(bad.URL); err != ErrCheckURLNotOK {
		t.Errorf("500: got %v, want ErrCheckURLNotOK", err)
	}
	if err := CheckURL(down.URL); err == nil {
		t.Error("unreachable: got nil, want error")
	}
	// retry must give up (it used to loop forever on a uint counter).
	if err := CheckURLWithRetry(down.URL, 1); err == nil {
		t.Error("retry unreachable: got nil, want error")
	}
}

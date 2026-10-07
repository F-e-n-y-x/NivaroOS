package route

import (
	"net/http/httptest"
	"testing"
)

func TestQuietRequest(t *testing.T) {
	if !quietRequest(httptest.NewRequest("POST", "/v2/message_bus/event/nivaroos/nivaroos:system:utilization:live", nil)) {
		t.Fatal("live readings must not be logged")
	}
	if quietRequest(httptest.NewRequest("POST", "/v2/message_bus/event/nivaroos/nivaroos:system:utilization", nil)) {
		t.Fatal("the 5 s readings keep their log line")
	}
	if quietRequest(httptest.NewRequest("GET", "/v2/message_bus/event/nivaroos/nivaroos:system:utilization:live", nil)) {
		t.Fatal("only the publish is quiet")
	}
}

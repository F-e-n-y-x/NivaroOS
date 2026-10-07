package route

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/labstack/echo/v4"
)

// The utilization publisher's pace. Normally a reading every 5 s on
// nivaroos:system:utilization (the web UI's widgets). While a client holds
// a fast lease (POST /v1/sys/utilization/live - the mobile app in "Real
// time", renewing while Home is on screen) it samples every 500 ms: the
// 5 s readings still go out under the usual name and the ones between
// under nivaroos:system:utilization:live, so the web widgets, which count
// readings, keep their pace. One timestamp holds the lease, whoever renews
// it: no per-subscriber state.
const (
	utilizationEvery     = 5 * time.Second
	utilizationFastEvery = 500 * time.Millisecond
	utilizationFastLease = 15 * time.Second

	UtilizationEvent     = "nivaroos:system:utilization"
	UtilizationLiveEvent = "nivaroos:system:utilization:live"
)

var utilizationFastUntil atomic.Int64 // unix nanos

// RequestFastUtilization starts (or extends) the fast lease from now.
func RequestFastUtilization(now time.Time) time.Duration {
	utilizationFastUntil.Store(now.Add(utilizationFastLease).UnixNano())
	return utilizationFastLease
}

// utilizationDue says which event a 500 ms tick at now publishes, if any:
// the usual one when 5 s have passed since the last usual one (half a tick
// of slack for timer jitter), the live one between them while the lease
// holds, nothing otherwise.
func utilizationDue(now, lastUsual time.Time, fastUntil int64) string {
	if now.Sub(lastUsual) >= utilizationEvery-utilizationFastEvery/2 {
		return UtilizationEvent
	}
	if now.UnixNano() < fastUntil {
		return UtilizationLiveEvent
	}
	return ""
}

// RunUtilizationPublisher publishes readings until stop closes. It wakes
// every 500 ms either way; an idle wake-up is a clock read.
func RunUtilizationPublisher(stop <-chan struct{}) {
	t := time.NewTicker(utilizationFastEvery)
	defer t.Stop()
	var lastUsual time.Time
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			name := utilizationDue(now, lastUsual, utilizationFastUntil.Load())
			if name == UtilizationEvent {
				lastUsual = now
			}
			if name != "" {
				SendAllHardwareStatusBySocket(name)
			}
		}
	}
}

func postUtilizationLive(c echo.Context) error {
	lease := RequestFastUtilization(time.Now())
	return c.JSON(http.StatusOK, model.Result{Success: http.StatusOK, Message: "ok", Data: map[string]int64{"lease_ms": lease.Milliseconds(), "every_ms": utilizationFastEvery.Milliseconds()}})
}

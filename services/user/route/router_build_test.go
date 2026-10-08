package route

import "testing"

// echo v5 panics on routes it would have silently ignored (nil handler,
// duplicate method+path); building the routers catches that at test time.
func TestRoutersBuild(t *testing.T) {
	InitV2Router()
}

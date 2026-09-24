package jobs

import (
	"testing"
	"time"
)

// Review finding (fixed): at a rotation, every request still carrying the
// old token used to mint another new token, and only the last four stayed
// valid. A phone with several requests in flight (parallel uploads) got 6+
// answers with different new tokens; keeping the first one locked it out.
// The new token is now derived from the old one, so every answer carries
// the same token and whichever one the phone keeps works.
func TestReviewRotationWithManyConcurrentOldTokenRequests(t *testing.T) {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Now()
	row, old, err := st.CreateDevice("p", PlatformAndroid, DefaultDeviceBackupRoot, t0)
	if err != nil {
		t.Fatal(err)
	}
	at := t0.Add(DeviceTokenRotateAfter + time.Hour)
	var handedOut []string
	for i := 0; i < 6; i++ { // six requests sent with the old token at once
		res, ok, err := st.AuthenticateDevice(row.ID, old, at)
		if err != nil || !ok || res.NewToken == "" {
			t.Fatalf("request %d: ok=%v err=%v new=%q", i, ok, err, res.NewToken)
		}
		handedOut = append(handedOut, res.NewToken)
	}
	for i, tok := range handedOut {
		if tok != handedOut[0] {
			t.Fatalf("answer %d carried a different new token", i)
		}
	}
	// The phone saves the token of the first answer it processed...
	if _, ok, _ := st.AuthenticateDevice(row.ID, handedOut[0], at.Add(time.Minute)); !ok {
		t.Fatal("a new token the server handed out moments ago is already refused")
	}
	// ...while more requests with the old token are still arriving: they
	// keep working and still point at that same token.
	res, ok, _ := st.AuthenticateDevice(row.ID, old, at.Add(2*time.Minute))
	if !ok || res.NewToken != handedOut[0] {
		t.Fatalf("late old-token request: ok=%v new=%q", ok, res.NewToken)
	}
	if _, ok, _ := st.AuthenticateDevice(row.ID, handedOut[0], at.Add(3*time.Minute)); !ok {
		t.Fatal("new token refused after a late old-token request")
	}
	// The derived token is still a proper device token, and a guess from
	// the old token alone (without the stored nonce) is not it.
	if !looksLikeDeviceToken(handedOut[0]) || deriveRotatedToken(old, row.ID, "") == handedOut[0] {
		t.Fatal("bad derived token")
	}
}

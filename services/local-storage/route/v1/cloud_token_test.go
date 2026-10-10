package v1

import "testing"

func TestCheckOAuthToken(t *testing.T) {
	ok := []map[string]string{
		{}, // form providers carry no token
		{"user": "x"},
		{"token": `{"access_token":"ya29.a","token_type":"Bearer","refresh_token":"1//r","expiry":"2026-10-10T18:51:38+05:30"}`},
		{"token": "  {\"access_token\":\"a\"}\n"}, // pasted with whitespace
	}
	for _, p := range ok {
		if err := checkOAuthToken(p); err != nil {
			t.Errorf("%v: %v", p, err)
		}
	}
	bad := []map[string]string{
		{"token": ""},
		{"token": `{"access_token":"ya29.a","token_type":"Bea`},   // cut off
		{"token": `"access_token":"ya29.a"}`},                     // missing the {
		{"token": `Paste the following into your remote machine`}, // other output
		{"token": `{"token_type":"Bearer"}`},                      // no access token
	}
	for _, p := range bad {
		if checkOAuthToken(p) == nil {
			t.Errorf("accepted %q", p["token"])
		}
	}
}

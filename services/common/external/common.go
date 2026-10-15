package external

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	http2 "github.com/F-e-n-y-x/NivaroOS/services/common/utils/http"

	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

func getAddress(addressFile string) (string, error) {
	buf, err := os.ReadFile(addressFile)
	if err != nil {
		return "", err
	}

	return string(buf), nil
}

func ping(address string, timeout time.Duration) error {
	response, err := http2.Get(address+"/ping", timeout)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.New("failed to ping the service as address " + address)
	}

	return nil
}

// localCall sends a request to another NivaroOS service on this host as
// same-host automation (with the root-only local token).
func localCall(method, url string, body []byte, timeout time.Duration) (*http.Response, error) {
	return http2.Do(func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		localtoken.Add(r)
		return r, nil
	}, timeout)
}

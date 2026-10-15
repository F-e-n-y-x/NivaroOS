package pkg

import (
	"errors"
	"net/http"
	"time"

	http2 "github.com/F-e-n-y-x/NivaroOS/services/common/utils/http"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"go.uber.org/zap"
)

var ErrCheckURLNotOK = errors.New("check url did not return 200 OK")

// CheckURLWithRetry tries url once plus up to retry more times, a second apart.
func CheckURLWithRetry(url string, retry uint) error {
	var err error
	for i := uint(0); i <= retry; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		logger.Info("Checking if service at URL is running...", zap.Any("url", url), zap.Any("attempt", i))
		if err = CheckURL(url); err == nil {
			return nil
		}
	}
	return err
}

func CheckURL(url string) error {
	response, err := http2.Get(url, 5*time.Second)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return ErrCheckURLNotOK
	}
	return nil
}

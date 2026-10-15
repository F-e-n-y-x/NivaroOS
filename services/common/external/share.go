package external

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	APINivaroOSShare = "/v1/samba/shares"
)

type ShareService interface {
	DeleteShare(id string) error
}
type shareService struct {
	addressFile string
}

func (n *shareService) DeleteShare(id string) error {
	address, err := getAddress(n.addressFile)
	if err != nil {
		return err
	}

	url := strings.TrimSuffix(address, "/") + APINivaroOSShare + "/" + id
	fmt.Println(url)

	response, err := localCall(http.MethodDelete, url, []byte("{}"), 30*time.Second)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.New("failed to send share (status code: " + fmt.Sprint(response.StatusCode) + ")")
	}
	return nil
}

func NewShareService(runtimePath string) ShareService {
	return &shareService{
		addressFile: filepath.Join(runtimePath, NivaroOSURLFilename),
	}
}

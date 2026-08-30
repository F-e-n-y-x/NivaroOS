package v2

import (
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/codegen"
)

type LocalStorage struct{}

func NewLocalStorage() codegen.ServerInterface {
	return &LocalStorage{}
}

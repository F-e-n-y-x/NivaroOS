package v2

import (
	"github.com/F-e-n-y-x/recasa/services/local-storage/codegen"
)

type LocalStorage struct{}

func NewLocalStorage() codegen.ServerInterface {
	return &LocalStorage{}
}

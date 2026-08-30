package v2

import codegen "github.com/F-e-n-y-x/NivaroOS/services/user/codegen/user_service"

type UserService struct{}

func NewUserService() codegen.ServerInterface {
	return &UserService{}
}

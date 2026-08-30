package v1

import (
	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/internal/op"
	"github.com/gin-gonic/gin"
)

func ListDriverInfo(c *gin.Context) {
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: op.GetDriverInfoMap()})
}

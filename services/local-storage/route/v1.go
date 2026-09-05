package route

import (
	"crypto/ecdsa"
	"os"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/config"
	v1 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/route/v1"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func InitV1Router() *gin.Engine {
	// check if environment variable is set
	ginMode, success := os.LookupEnv(gin.EnvGinMode)
	if !success {
		ginMode = gin.ReleaseMode
	}
	gin.SetMode(ginMode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(ginCors())
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	if ginMode != gin.ReleaseMode {
		r.Use(ginWriteLog())
	}
	v1Group := r.Group("/v1")

	v1Group.Use(ginJWT(
		func() (*ecdsa.PublicKey, error) {
			return external.GetPublicKey(config.CommonInfo.RuntimePath)
		},
	))

	{
		v1DisksGroup := v1Group.Group("/disks")
		v1DisksGroup.Use()
		{

			v1DisksGroup.GET("", v1.GetDiskList)
			v1DisksGroup.GET("/usb", v1.GetDisksUSBList)
			v1DisksGroup.DELETE("/usb", v1.DeleteDiskUSB)
			v1DisksGroup.DELETE("", v1.DeleteDisksUmount)
			v1DisksGroup.GET("/smart", v1.GetDiskSmartInfo)
			v1DisksGroup.POST("/smart-test", v1.PostDiskSmartTest)
			v1DisksGroup.GET("/standby", v1.GetDiskStandby)
			v1DisksGroup.PUT("/standby", v1.PutDiskStandby)
		}

		v1StorageGroup := v1Group.Group("/storage")
		v1StorageGroup.Use()
		{
			v1StorageGroup.POST("", v1.PostAddStorage)

			v1StorageGroup.PUT("", v1.PutFormatStorage)

			v1StorageGroup.DELETE("", v1.DeleteStorage)
			v1StorageGroup.GET("", v1.GetStorageList)

			v1FstabGroup := v1StorageGroup.Group("/fstab")
			v1FstabGroup.Use()
			{
				v1FstabGroup.GET("", v1.GetFstabMounts)
				v1FstabGroup.GET("/candidates", v1.GetFstabCandidates)
				v1FstabGroup.POST("", v1.PostFstabMount)
				v1FstabGroup.PUT("", v1.PutFstabMount)
				v1FstabGroup.DELETE("", v1.DeleteFstabMount)
				v1FstabGroup.PUT("/enabled", v1.PutFstabMountEnabled)
				v1FstabGroup.POST("/mount", v1.PostFstabMountAction)
				v1FstabGroup.POST("/umount", v1.PostFstabUmountAction)
				v1FstabGroup.POST("/adopt", v1.PostFstabAdoptAction)
			}
		}
		v1CloudGroup := v1Group.Group("/cloud")
		v1CloudGroup.Use()
		{
			v1CloudGroup.GET("", v1.ListStorages)
			v1CloudGroup.DELETE("", v1.UmountStorage)
			v1CloudGroup.GET("/providers", v1.GetCloudProviders)
			v1CloudGroup.GET("/providers/:type/options", v1.GetCloudProviderOptions)
			v1CloudGroup.POST("/accounts", v1.PostCloudAccount)
			v1CloudGroup.POST("/accounts/icloud/start", v1.PostICloudStart)
			v1CloudGroup.POST("/accounts/icloud/verify", v1.PostICloudVerify)
			v1CloudGroup.PUT("/accounts/:name", v1.PatchCloudAccount)
			v1CloudGroup.POST("/accounts/:name/reconnect", v1.PostCloudAccountReconnect)
			v1CloudGroup.POST("/accounts/:name/speedtest", v1.PostCloudAccountSpeedTest)
		}
		v1DriverGroup := v1Group.Group("/driver")
		v1DriverGroup.Use()
		{
			v1DriverGroup.GET("", v1.ListDriverInfo)
		}
		v1USBGroup := v1Group.Group("/usb")
		v1USBGroup.Use()
		{
			v1USBGroup.PUT("/usb-auto-mount", v1.PutSystemUSBAutoMount) ///sys/usb/:status
			v1USBGroup.GET("/usb-auto-mount", v1.GetSystemUSBAutoMount) ///sys/usb/status
		}
	}

	return r
}

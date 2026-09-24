package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/docker"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	client2 "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

// 检查镜像是否存在
func (ds *dockerService) IsExistImage(imageName string) bool {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return false
	}
	defer cli.Close()
	filter := filters.NewArgs()
	filter.Add("reference", imageName)

	list, err := cli.ImageList(context.Background(), types.ImageListOptions{Filters: filter})

	if err == nil && len(list) > 0 {
		return true
	}

	return false
}

// 安装镜像
func (ds *dockerService) PullImage(ctx context.Context, imageName string) error {
	go PublishEventWrapper(ctx, common.EventTypeImagePullBegin, map[string]string{
		common.PropertyTypeImageName.Name: imageName,
	})

	defer PublishEventWrapper(ctx, common.EventTypeImagePullEnd, map[string]string{
		common.PropertyTypeImageName.Name: imageName,
	})

	if err := docker.PullImage(ctx, imageName, func(out io.ReadCloser) {
		pullImageProgress(ctx, out, "INSTALL", 1, 1)
	}); err != nil {
		go PublishEventWrapper(ctx, common.EventTypeImagePullError, map[string]string{
			common.PropertyTypeImageName.Name: imageName,
			common.PropertyTypeMessage.Name:   err.Error(),
		})

		return err
	}

	return nil
}

// Try to pull latest image.
//
// It returns `true` if the image is updated.
func (ds *dockerService) PullLatestImage(ctx context.Context, imageName string) (bool, error) {
	isImageUpdated := false

	if common.PropertiesFromContext(ctx) == nil {
		ctx = common.WithProperties(ctx, make(map[string]string))
	}

	// events published from goroutines get their own copy of the context
	// properties: the defer below writes the result into the original map,
	// which must not be written while a publisher ranges over it.
	eventCtx := common.WithProperties(ctx, lo.Assign(common.PropertiesFromContext(ctx)))

	go PublishEventWrapper(eventCtx, common.EventTypeImagePullBegin, map[string]string{
		common.PropertyTypeImageName.Name: imageName,
	})

	// published synchronously after the defer below has added the result
	defer PublishEventWrapper(ctx, common.EventTypeImagePullEnd, map[string]string{
		common.PropertyTypeImageName.Name: imageName,

		// update image update information in the defer func below, instead of here.
		// this because PublishEventWrapper will retrieve the information from context and include all properties in the event.
		//
		// common.PropertyTypeImageUpdated.Name: fmt.Sprint(isImageUpdated),  // <- no need to do it here.
	})

	defer func() {
		// write image updated information as a property back to context, so both current func and external caller can see it
		properties := common.PropertiesFromContext(ctx)
		if properties != nil {
			properties[common.PropertyTypeImageUpdated.Name] = fmt.Sprint(isImageUpdated)
		}
	}()

	if strings.HasPrefix(imageName, "sha256:") {
		message := "container uses a pinned image, and cannot be updated"
		go PublishEventWrapper(eventCtx, common.EventTypeImagePullError, map[string]string{
			common.PropertyTypeImageName.Name: imageName,
			common.PropertyTypeMessage.Name:   message,
		})

		return false, fmt.Errorf(message)
	}

	imageInfo1, err := docker.Image(ctx, imageName)
	if err != nil {
		go PublishEventWrapper(eventCtx, common.EventTypeImagePullError, map[string]string{
			common.PropertyTypeImageName.Name: imageName,
			common.PropertyTypeMessage.Name:   err.Error(),
		})
		return false, err
	}

	if match, err := docker.CompareDigest(imageName, imageInfo1.RepoDigests); err != nil {
		// do nothing
	} else if match {
		return false, nil
	}

	if err = docker.PullImage(ctx, imageName, func(out io.ReadCloser) {
		pullImageProgress(eventCtx, out, "UPDATE", 1, 1)
	}); err != nil {
		go PublishEventWrapper(eventCtx, common.EventTypeImagePullError, map[string]string{
			common.PropertyTypeImageName.Name: imageName,
			common.PropertyTypeMessage.Name:   err.Error(),
		})
		return false, err
	}

	imageInfo2, err := docker.Image(ctx, imageName)
	if err != nil {
		go PublishEventWrapper(eventCtx, common.EventTypeImagePullError, map[string]string{
			common.PropertyTypeImageName.Name: imageName,
			common.PropertyTypeMessage.Name:   err.Error(),
		})
		return false, err
	}

	isImageUpdated = imageInfo1.ID != imageInfo2.ID
	return isImageUpdated, nil
}

// 删除镜像
func (ds *dockerService) RemoveImage(name string) error {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()
	imageList, err := cli.ImageList(context.Background(), types.ImageListOptions{})
	if err != nil {
		return err
	}

	imageID := ""

Loop:
	for _, ig := range imageList {
		for _, i := range ig.RepoTags {
			if i == name {
				imageID = ig.ID
				break Loop
			}
		}
	}
	_, err = cli.ImageRemove(context.Background(), imageID, types.ImageRemoveOptions{})
	return err
}

type StatusType string

const (
	Pull         StatusType = "Pulling fs layer"
	PullComplete StatusType = "Pull complete"
)

type ProgressDetail struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

type PullOut struct {
	Status         StatusType     `json:"status"`
	ProgressDetail ProgressDetail `json:"progressDetail"`
	Id             string         `json:"id"`
}

type Throttler struct {
	InvokeInterval time.Duration
	LastInvokeTime time.Time
}

func NewThrottler(interval time.Duration) *Throttler {
	return &Throttler{
		InvokeInterval: interval,
	}
}

func (t *Throttler) ThrottleFunc(f func()) {
	if time.Since(t.LastInvokeTime) >= t.InvokeInterval {
		f()
		t.LastInvokeTime = time.Now()
	}
}

// imagePullProgress is the overall progress (0-100) while pulling image
// currentImage (1-based) of totalImageNum, with completedLayers of layers of
// the current image done. Earlier images count as complete, so the value
// only goes up across images.
func imagePullProgress(completedLayers, layers, currentImage, totalImageNum int) int {
	if totalImageNum <= 0 {
		return 0
	}

	fraction := 0.0
	if layers > 0 {
		fraction = float64(completedLayers) / float64(layers)
	}

	if fraction > 1 {
		fraction = 1
	}

	progress := int((float64(currentImage-1) + fraction) / float64(totalImageNum) * 100)

	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	return progress
}

func pullImageProgress(ctx context.Context, out io.ReadCloser, notificationType string, totalImageNum int, currentImage int) {
	layerNum := 0
	completedLayerNum := 0
	decoder := json.NewDecoder(out)
	if decoder == nil {
		logger.Error("failed to create json decoder")
		return
	}

	// the UI only listens to app:install-progress (for installs and
	// updates alike), so notificationType does not change the event name.
	_ = notificationType

	throttler := NewThrottler(500 * time.Millisecond)

	// publish synchronously, in order - progress events used to be sent from
	// one goroutine each and could arrive out of order (going backwards)
	publish := func(progress int) {
		PublishEventWrapper(ctx, common.EventTypeAppInstallProgress, map[string]string{
			common.PropertyTypeAppProgress.Name: fmt.Sprintf("%d", progress),
		})
	}

	lastProgress := -1

	for decoder.More() {
		var message jsonmessage.JSONMessage
		if err := decoder.Decode(&message); err != nil {
			logger.Error("failed to decode json message", zap.Error(err))
			break // a broken stream never recovers; More() would spin
		}

		switch message.Status {
		// pull a new layer
		case string(Pull):
			layerNum++
		// pull a layer complete
		case string(PullComplete):
			completedLayerNum++
		}

		progress := imagePullProgress(completedLayerNum, layerNum, currentImage, totalImageNum)
		if progress < lastProgress {
			progress = lastProgress
		}

		// reduce the event send frequency
		throttler.ThrottleFunc(func() {
			publish(progress)
			lastProgress = progress
		})
	}

	// always report this image as done, so the last image ends at 100%
	if final := imagePullProgress(1, 1, currentImage, totalImageNum); final != lastProgress {
		publish(final)
	}
}

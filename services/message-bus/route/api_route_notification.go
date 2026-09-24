package route

import (
	"net/http"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const (
	notificationPageDefault = 50
	notificationPageMax     = 200
	notificationIDsMax      = 1000
)

// notificationUserID is whose read state a request works on: the user of
// its access token (the JWT middleware stores the parsed claims under
// "user"). Same-host automation skips the JWT and gets user 0, a viewer of
// its own. Never a header: a skipped request could set any header.
func notificationUserID(ctx echo.Context) int {
	if claims, ok := ctx.Get("user").(*jwt.Claims); ok && claims != nil {
		return claims.ID
	}
	return 0
}

func (r *APIRoute) notificationsUnavailable(ctx echo.Context) error {
	return ctx.JSON(http.StatusServiceUnavailable, codegen.BaseResponse{Message: utils.Ptr("the notification feed is not available")})
}

func (r *APIRoute) GetNotifications(ctx echo.Context, params codegen.GetNotificationsParams) error {
	feed := r.services.NotificationService
	if feed == nil {
		return r.notificationsUnavailable(ctx)
	}
	q := model.NotificationQuery{UserID: notificationUserID(ctx), Limit: notificationPageDefault}
	if params.After != nil {
		q.After = *params.After
	}
	if params.Before != nil {
		q.Before = *params.Before
	}
	if params.Limit != nil {
		q.Limit = *params.Limit
	}
	if q.Limit < 1 || q.Limit > notificationPageMax || q.After < 0 || q.Before < 0 {
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: utils.Ptr("after/before must be >= 0 and limit 1-200")})
	}
	if params.Unread != nil {
		q.UnreadOnly = *params.Unread
	}

	page, err := feed.List(q)
	if err != nil {
		logger.Error("notification feed: listing failed", zap.Error(err))
		return ctx.JSON(http.StatusInternalServerError, codegen.ResponseInternalServerError{Message: utils.Ptr("could not read the notification feed")})
	}

	out := codegen.NotificationList{
		Data:        make([]codegen.Notification, 0, len(page.Items)),
		UnreadCount: page.UnreadCount,
		LatestId:    page.LatestID,
		HasMore:     page.HasMore,
	}
	for _, item := range page.Items {
		out.Data = append(out.Data, notificationOut(item))
	}
	return ctx.JSON(http.StatusOK, out)
}

func notificationOut(item model.NotificationView) codegen.Notification {
	n := codegen.Notification{
		Id:        item.ID,
		Time:      time.UnixMilli(item.CreatedAt).UTC(),
		SourceId:  item.SourceID,
		EventName: item.EventName,
		Category:  codegen.NotificationCategory(item.Category),
		Level:     codegen.NotificationLevel(item.Level),
		Title:     item.Title,
		Message:   item.Message,
		Key:       item.Key,
		Icon:      item.Icon,
		Args:      map[string]interface{}{},
		Read:      item.Read,
	}
	if item.Args != "" {
		var args map[string]interface{}
		if json.Unmarshal([]byte(item.Args), &args) == nil && args != nil {
			n.Args = args
		}
	}
	if item.Action != "" {
		var action map[string]interface{}
		if json.Unmarshal([]byte(item.Action), &action) == nil && action != nil {
			n.Action = &action
		}
	}
	return n
}

// notificationSelection reads a read/dismiss body: ids, or all (+ up_to).
// problem is non-empty when the body is unusable.
func notificationSelection(ctx echo.Context) (ids []int64, all bool, upTo int64, problem string) {
	var body codegen.NotificationSelection
	if err := ctx.Bind(&body); err != nil {
		return nil, false, 0, "invalid body"
	}
	if body.All != nil {
		all = *body.All
	}
	if body.UpTo != nil {
		upTo = *body.UpTo
	}
	if body.Ids != nil {
		ids = *body.Ids
	}
	if upTo < 0 {
		return nil, false, 0, "up_to must be >= 0"
	}
	if !all && len(ids) == 0 {
		return nil, false, 0, "give ids, or all: true"
	}
	if len(ids) > notificationIDsMax {
		return nil, false, 0, "at most 1000 ids"
	}
	return ids, all, upTo, ""
}

func (r *APIRoute) MarkNotificationsRead(ctx echo.Context) error {
	return r.changeNotifications(ctx, "read")
}

func (r *APIRoute) DismissNotifications(ctx echo.Context) error {
	return r.changeNotifications(ctx, "dismiss")
}

func (r *APIRoute) changeNotifications(ctx echo.Context, change string) error {
	feed := r.services.NotificationService
	if feed == nil {
		return r.notificationsUnavailable(ctx)
	}
	ids, all, upTo, problem := notificationSelection(ctx)
	if problem != "" {
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &problem})
	}
	userID := notificationUserID(ctx)
	var unread int64
	var err error
	if change == "read" {
		unread, err = feed.MarkRead(userID, ids, all, upTo)
	} else {
		unread, err = feed.Dismiss(userID, ids, all, upTo)
	}
	if err != nil {
		logger.Error("notification feed: updating state failed", zap.Error(err), zap.String("change", change))
		return ctx.JSON(http.StatusInternalServerError, codegen.ResponseInternalServerError{Message: utils.Ptr("could not update the notification feed")})
	}
	return ctx.JSON(http.StatusOK, codegen.NotificationCount{UnreadCount: unread})
}

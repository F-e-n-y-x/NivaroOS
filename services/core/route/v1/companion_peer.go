package v1

// Phone to phone directly over Tailscale (design 2026-10, phase 1).
//
// Phone B asks the server, over its normal session, for a peer ticket to
// read one path on phone A. The ticket is an HMAC with A's file-server
// secret, so A checks it locally (companion_file_server.dart) with no new
// keys and no call back to the server. It is read-only, scoped to one path
// and one operation, expires after two hours, travels only in the
// X-Peer-Ticket header and is never logged. A accepts it only on its
// Tailscale listeners (WireGuard-encrypted), never on the plain home Wi-Fi.
// When there is no direct route, B keeps using the server relay.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/labstack/echo/v5"
)

const companionPeerTicketTTL = 2 * time.Hour

// companionPeerTicket is "<base64url(payload)>.<hex hmac>". The payload,
// "v1\n<exp unix>\n<op>\n<peer>\n<path>", is what A needs to check it;
// the MAC also covers A's own id.
func companionPeerTicket(secret, deviceID, peer, op, phonePath string, exp time.Time) string {
	payload := "v1\n" + strconv.FormatInt(exp.Unix(), 10) + "\n" + op + "\n" + peer + "\n" + phonePath
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("nivaroos-peer\n" + deviceID + "\n" + payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + hex.EncodeToString(mac.Sum(nil))
}

// companionDeviceOfSession: the caller's own phone (the device its login
// session registered), "" when unknown.
func companionDeviceOfSession(sid string) string {
	if sid == "" {
		return ""
	}
	companionMu.RLock()
	defer companionMu.RUnlock()
	for id, sids := range companionSessions {
		for _, s := range sids {
			if s == sid {
				return id
			}
		}
	}
	return ""
}

// POST /v1/companion/:id/peer-ticket {"path": "...", "op": "read"|"list"}
// path is the file's server path (/DATA/Companion/<A>/...) or its path on
// the phone (/storage/...).
func PostCompanionPeerTicket(ctx *echo.Context) error {
	var in struct {
		Path string `json:"path"`
		Op   string `json:"op"`
	}
	if err := ctx.Bind(&in); err != nil || (in.Op != "read" && in.Op != "list") || in.Path == "" {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: "path and op (read or list) are required"})
	}
	dev, ok := lookupCompanionDevice(ctx, ctx.Param("id"))
	// Only the phone's owner: an unclaimed legacy device is visible to
	// everyone, but nobody gets a key to it.
	if uid := companionCaller(ctx); !ok || (uid != "" && dev.OwnerUserID != uid) {
		return companionNotFound(ctx)
	}
	phonePath := ""
	if p := filepath.Clean(in.Path); strings.HasPrefix(p, dev.RootPath+"/") || p == dev.RootPath {
		phonePath = p
	} else if owner, pp := GetCompanionDeviceByStoragePath(p); owner != nil && owner.ID == dev.ID {
		phonePath = pp
	}
	if phonePath == "" || strings.Contains(phonePath, "\n") {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: "the path isn't on this phone"})
	}
	var cands []string
	for _, c := range companionCandidates(dev) {
		if c.Kind == companionRouteTailscale {
			cands = append(cands, c.hostPort(companionPort(dev)))
		}
	}
	// A direct route proves A's file server is up right now.
	route := companionCurrentRoute(ctx.Request().Context(), dev)
	if len(cands) == 0 || dev.Secret == "" || (route != companionRouteLAN && route != companionRouteTailscale) {
		return ctx.JSON(http.StatusConflict, model.Result{Success: common_err.SERVICE_ERROR, Message: "no direct route to this phone: use the server"})
	}
	exp := time.Now().Add(companionPeerTicketTTL)
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: "ok", Data: map[string]interface{}{
		"candidates": cands,
		"ticket":     companionPeerTicket(dev.Secret, dev.ID, companionDeviceOfSession(companionSessionOf(ctx)), in.Op, phonePath, exp),
		"expires_at": exp.UTC(),
		"path":       phonePath,
		"op":         in.Op,
	}})
}

package main

// REST API, same-origin through the gateway at /v1/fans/* (the service
// gets the full path). Reads need a valid NivaroOS token; every write also
// needs an administrator (same rule as Backup & Sync and Download
// Station). /v1/fans/health is open (the UI's "is it installed?" check).
//
//	GET  /v1/fans/health
//	GET  /v1/fans/status
//	POST /v1/fans/fan       {id, label?, mode?, fixed_pct?, curve?, source?, min_pct?, hysteresis_c?, tach?}
//	POST /v1/fans/preset    {preset: auto|quiet|balanced|performance}
//	POST /v1/fans/settings  {critical_cpu_c?, critical_gpu_c?}
//	POST /v1/fans/identify  {id}
//	POST /v1/fans/auto      every fan back to Auto
//	POST /v1/fans/detect    look for the motherboard fan driver again

import (
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

const apiBase = "/v1/fans"

type api struct {
	c         *Controller
	det       detector
	publicKey func() (*ecdsa.PublicKey, error)
	// validate is jwt.Validate; tests replace it.
	validate   func(token string) (ok bool, admin bool)
	identifyMu sync.Mutex
}

func newAPI(c *Controller, det detector, runtimePath string) *api {
	a := &api{c: c, det: det}
	a.publicKey = func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(runtimePath) }
	a.validate = func(token string) (bool, bool) {
		ok, claims, err := jwt.Validate(token, a.publicKey)
		if err != nil || !ok || claims == nil {
			return false, false
		}
		return true, tokenIsAdmin(token)
	}
	return a
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (a *api) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(apiBase+"/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "service": "nivaroos-fans"})
	})
	mux.Handle(apiBase+"/", a.auth(http.HandlerFunc(a.route)))
	return mux
}

func (a *api) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.IsLocalAutomation(r) {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
		ok, admin := a.validate(token)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": 401, "message": "token is invalid"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !admin {
			writeErr(w, http.StatusForbidden, "Fan settings can only be changed by an administrator.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}
	return true
}

func (a *api) fail(w http.ResponseWriter, err error) {
	var ue userError
	if errors.As(err, &ue) {
		status := http.StatusBadRequest
		if err == errUnknownFan {
			status = http.StatusNotFound
		}
		writeErr(w, status, ue.msg)
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}

func (a *api) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, apiBase)
	if r.Method == http.MethodGet && path == "/status" {
		writeJSON(w, http.StatusOK, a.c.Status())
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	switch path {
	case "/fan":
		var u FanUpdate
		if !decode(w, r, &u) {
			return
		}
		if err := a.c.UpdateFan(u); err != nil {
			a.fail(w, err)
			return
		}
	case "/preset":
		var body struct {
			Preset string `json:"preset"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := a.c.ApplyPreset(body.Preset); err != nil {
			a.fail(w, err)
			return
		}
	case "/settings":
		var body struct {
			CPU *int `json:"critical_cpu_c"`
			GPU *int `json:"critical_gpu_c"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := a.c.SetCritical(body.CPU, body.GPU); err != nil {
			a.fail(w, err)
			return
		}
	case "/identify":
		var body struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &body) {
			return
		}
		if !a.identifyMu.TryLock() {
			writeErr(w, http.StatusConflict, "another fan is being identified - try again in a few seconds")
			return
		}
		res, err := a.c.Identify(body.ID)
		a.identifyMu.Unlock()
		if err != nil {
			a.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	case "/auto":
		if err := a.c.AllAuto(); err != nil {
			a.fail(w, err)
			return
		}
	case "/detect":
		a.c.Redetect(a.det)
	default:
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// Apply at once rather than on the next tick, then answer with the
	// new state.
	a.c.Tick()
	writeJSON(w, http.StatusOK, a.c.Status())
}

// tokenIsAdmin: today's NivaroOS tokens carry no role (every account is
// the owner), so no role claim means admin; once a role/roles claim is
// present it must say admin/administrator/owner. Same rule as
// services/backup/jobs/auth.go. Only called after jwt.Validate.
func tokenIsAdmin(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims map[string]interface{}
	if json.Unmarshal(raw, &claims) != nil {
		return false
	}
	isAdmin := func(v interface{}) bool {
		s, ok := v.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		return ok && (s == "admin" || s == "administrator" || s == "owner")
	}
	checked := false
	for _, key := range []string{"role", "roles"} {
		v, ok := claims[key]
		if !ok || v == nil {
			continue
		}
		checked = true
		switch t := v.(type) {
		case string:
			if isAdmin(t) {
				return true
			}
		case []interface{}:
			for _, x := range t {
				if isAdmin(x) {
					return true
				}
			}
		}
	}
	if b, ok := claims["is_admin"].(bool); ok {
		return b
	}
	return !checked
}

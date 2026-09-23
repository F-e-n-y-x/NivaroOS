package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"syscall"
)

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

type addBody struct {
	AddRequest
	// Set when the download was captured by the lite browser: the sidecar
	// fills in the cookies/referer it recorded, so they never have to
	// round-trip through the UI.
	CaptureSession string `json:"capture_session"`
	CaptureID      string `json:"capture_id"`
}

func RegisterRoutes(mux *http.ServeMux, m *Manager, st *SettingsStore, ab *Adblocker, br *Browser, hist *History) {
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		s := st.Get()
		out := map[string]interface{}{"version": version, "default_dir": s.DefaultDir}
		var fs syscall.Statfs_t
		if syscall.Statfs(s.DefaultDir, &fs) == nil {
			out["disk_free"] = int64(fs.Bavail) * int64(fs.Bsize)
			out["disk_total"] = int64(fs.Blocks) * int64(fs.Bsize)
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, st.Get())
	})
	mux.HandleFunc("PUT /settings", func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]json.RawMessage
		if err := readJSON(r, &patch); err != nil {
			writeErr(w, 400, err)
			return
		}
		// Merge onto the current settings, so the UI can send just the
		// fields a given control changed.
		cur := st.Get()
		raw, _ := json.Marshal(cur)
		var merged map[string]json.RawMessage
		_ = json.Unmarshal(raw, &merged)
		for k, v := range patch {
			merged[k] = v
		}
		raw, _ = json.Marshal(merged)
		var next Settings
		if err := json.Unmarshal(raw, &next); err != nil {
			writeErr(w, 400, err)
			return
		}
		s, err := st.Update(func(s *Settings) { *s = next })
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, s)
	})

	mux.HandleFunc("GET /downloads", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, m.List())
	})
	mux.HandleFunc("POST /downloads", func(w http.ResponseWriter, r *http.Request) {
		var body addBody
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		req := body.AddRequest
		if body.CaptureSession != "" && body.CaptureID != "" {
			if s := br.Session(body.CaptureSession); s != nil {
				if c, ok := s.Capture(body.CaptureID); ok {
					if req.Headers == nil {
						req.Headers = map[string]string{}
					}
					for k, v := range c.headers {
						req.Headers[k] = v
					}
					if req.Source == "" {
						req.Source = "browser"
					}
				}
			}
		}
		v, err := m.Add(req)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 201, v)
	})
	mux.HandleFunc("GET /downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, ok := m.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, 404, errNotFound)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("PATCH /downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body UpdateRequest
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		v, err := m.Update(r.PathValue("id"), body)
		if err != nil {
			code := 400
			if errors.Is(err, errNotFound) {
				code = 404
			}
			writeErr(w, code, err)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("DELETE /downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		del, _ := strconv.ParseBool(r.URL.Query().Get("delete_file"))
		if err := m.Delete(r.PathValue("id"), del); err != nil {
			writeErr(w, 404, err)
			return
		}
		w.WriteHeader(204)
	})
	action := func(fn func(string) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := fn(r.PathValue("id")); err != nil {
				code := 400
				if errors.Is(err, errNotFound) {
					code = 404
				}
				writeErr(w, code, err)
				return
			}
			v, _ := m.Get(r.PathValue("id"))
			writeJSON(w, 200, v)
		}
	}
	mux.HandleFunc("POST /downloads/{id}/pause", action(m.Pause))
	mux.HandleFunc("POST /downloads/{id}/resume", action(m.Resume))
	mux.HandleFunc("POST /downloads/{id}/redownload", action(m.Redownload))
	mux.HandleFunc("POST /downloads/pause-all", func(w http.ResponseWriter, r *http.Request) {
		for _, d := range m.List() {
			if d.State == StateDownloading || d.State == StateQueued {
				_ = m.Pause(d.ID)
			}
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /downloads/resume-all", func(w http.ResponseWriter, r *http.Request) {
		for _, d := range m.List() {
			if d.State == StatePaused || d.State == StateFailed {
				_ = m.Resume(d.ID)
			}
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /downloads/clear-completed", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]int{"removed": m.ClearCompleted()})
	})

	mux.HandleFunc("POST /probe", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL            string            `json:"url"`
			Headers        map[string]string `json:"headers"`
			CaptureSession string            `json:"capture_session"`
			CaptureID      string            `json:"capture_id"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		if body.CaptureSession != "" {
			if s := br.Session(body.CaptureSession); s != nil {
				if c, ok := s.Capture(body.CaptureID); ok {
					body.Headers = c.headers
				}
			}
		}
		res, err := m.Probe(r.Context(), body.URL, body.Headers)
		if err != nil {
			writeErr(w, 502, err)
			return
		}
		writeJSON(w, 200, res)
	})

	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		events, last := m.events.Since(after)
		writeJSON(w, 200, map[string]interface{}{"events": events, "last": last})
	})

	mux.HandleFunc("GET /adblock", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, ab.Stats())
	})
	mux.HandleFunc("POST /adblock/update", func(w http.ResponseWriter, r *http.Request) {
		go ab.UpdateLists(serverCtx, true)
		writeJSON(w, 202, ab.Stats())
	})

	mux.HandleFunc("GET /browser/history", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > maxHistory {
			limit = 500
		}
		writeJSON(w, 200, hist.List(r.URL.Query().Get("q"), limit))
	})
	mux.HandleFunc("POST /browser/history", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		e, ok := hist.Add(body.URL, body.Title)
		if !ok {
			writeErr(w, 400, errors.New("not an http(s) address"))
			return
		}
		writeJSON(w, 200, e)
	})
	mux.HandleFunc("DELETE /browser/history/{id}", func(w http.ResponseWriter, r *http.Request) {
		hist.Delete(r.PathValue("id"))
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /browser/history", func(w http.ResponseWriter, r *http.Request) {
		hist.Clear()
		w.WriteHeader(204)
	})

	mux.HandleFunc("POST /browser/sessions", func(w http.ResponseWriter, r *http.Request) {
		s := br.NewSession()
		writeJSON(w, 201, s.Stats())
	})
	mux.HandleFunc("GET /browser/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		s := br.Session(r.PathValue("sid"))
		if s == nil {
			writeErr(w, 404, errors.New("session expired"))
			return
		}
		writeJSON(w, 200, s.Stats())
	})
	mux.HandleFunc("DELETE /browser/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		br.DeleteSession(r.PathValue("sid"))
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /browser/sessions/{sid}/clear-cookies", func(w http.ResponseWriter, r *http.Request) {
		s := br.Session(r.PathValue("sid"))
		if s == nil {
			writeErr(w, 404, errors.New("session expired"))
			return
		}
		s.ClearCookies()
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /browser/sessions/{sid}/captures", func(w http.ResponseWriter, r *http.Request) {
		s := br.Session(r.PathValue("sid"))
		if s == nil {
			writeErr(w, 404, errors.New("session expired"))
			return
		}
		var body struct {
			URL     string `json:"url"`
			Referer string `json:"referer"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		c, err := s.CaptureURL(body.URL, body.Referer)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 201, c)
	})
	mux.HandleFunc("GET /browser/sessions/{sid}/captures/{cid}", func(w http.ResponseWriter, r *http.Request) {
		s := br.Session(r.PathValue("sid"))
		if s == nil {
			writeErr(w, 404, errors.New("session expired"))
			return
		}
		c, ok := s.Capture(r.PathValue("cid"))
		if !ok {
			writeErr(w, 404, errors.New("capture not found"))
			return
		}
		writeJSON(w, 200, c)
	})
}

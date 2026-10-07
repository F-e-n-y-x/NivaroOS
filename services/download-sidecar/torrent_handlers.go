package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func registerTorrentRoutes(mux *http.ServeMux, tm *Torrents) {
	fail := func(w http.ResponseWriter, err error) {
		code := 400
		if errors.Is(err, errNotFound) {
			code = 404
		}
		writeErr(w, code, err)
	}
	hashOf := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		h := strings.ToLower(r.PathValue("hash"))
		if !hashRe.MatchString(h) {
			writeErr(w, 400, errors.New("not a torrent info hash"))
			return "", false
		}
		return h, true
	}

	mux.HandleFunc("GET /torrents", func(w http.ResponseWriter, r *http.Request) {
		list, running, err := tm.List(r.Context())
		if err != nil {
			writeErr(w, 502, err)
			return
		}
		s := tm.st.Get().Torrent
		kind := engineKind(s)
		var dl, up int64
		for _, t := range list {
			dl += t.DlSpeed
			up += t.UpSpeed
		}
		unsupported := engineUnsupported[kind]
		if unsupported == nil {
			unsupported = []string{}
		}
		writeJSON(w, 200, map[string]interface{}{
			"engine":           kind,
			"running":          running,
			"qbittorrent":      qbtInstalled(),
			"unsupported":      unsupported,
			"alt_speed_active": s.altActive(time.Now()),
			"dl_speed":         dl,
			"up_speed":         up,
			"torrents":         list,
		})
	})
	mux.HandleFunc("POST /torrents", func(w http.ResponseWriter, r *http.Request) {
		var req TorrentAddRequest
		if err := readJSONLimit(r, &req, maxTorrentFile*4/3+(1<<16)); err != nil {
			writeErr(w, 400, err)
			return
		}
		meta, err := tm.Add(r.Context(), req)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 201, map[string]string{"hash": meta.Hash, "name": meta.Name})
	})
	mux.HandleFunc("GET /torrents/trackers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, tm.Trackers())
	})
	mux.HandleFunc("POST /torrents/trackers/refresh", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(serverCtx, 40*time.Second)
		defer cancel()
		tm.maybeRefreshTrackers(ctx, true)
		writeJSON(w, 200, tm.Trackers())
	})
	mux.HandleFunc("GET /torrents/{hash}", func(w http.ResponseWriter, r *http.Request) {
		h, ok := hashOf(w, r)
		if !ok {
			return
		}
		d, err := tm.Detail(r.Context(), h)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, d)
	})
	for _, action := range []string{"pause", "resume", "recheck"} {
		action := action
		mux.HandleFunc("POST /torrents/{hash}/"+action, func(w http.ResponseWriter, r *http.Request) {
			h, ok := hashOf(w, r)
			if !ok {
				return
			}
			if err := tm.do(r.Context(), func(e TorrentEngine) error { return e.Act(r.Context(), h, action) }); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(204)
		})
	}
	mux.HandleFunc("DELETE /torrents/{hash}", func(w http.ResponseWriter, r *http.Request) {
		h, ok := hashOf(w, r)
		if !ok {
			return
		}
		del, _ := strconv.ParseBool(r.URL.Query().Get("delete_files"))
		if err := tm.do(r.Context(), func(e TorrentEngine) error { return e.Remove(r.Context(), h, del) }); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("PUT /torrents/{hash}/files", func(w http.ResponseWriter, r *http.Request) {
		h, ok := hashOf(w, r)
		if !ok {
			return
		}
		var body struct {
			IDs      []int `json:"ids"`
			Priority int   `json:"priority"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		if len(body.IDs) == 0 || !validPriority(body.Priority) {
			writeErr(w, 400, errors.New("pick files and a priority of 0 (skip), 1, 6 or 7"))
			return
		}
		if err := tm.do(r.Context(), func(e TorrentEngine) error { return e.SetFilePriority(r.Context(), h, body.IDs, body.Priority) }); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("PUT /torrents/{hash}/options", func(w http.ResponseWriter, r *http.Request) {
		h, ok := hashOf(w, r)
		if !ok {
			return
		}
		var body struct {
			Sequential bool `json:"sequential"`
			FirstLast  bool `json:"first_last"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		if err := tm.do(r.Context(), func(e TorrentEngine) error { return e.SetOptions(r.Context(), h, body.Sequential, body.FirstLast) }); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
}

package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/snirye/youtube-guardian/backend/internal/store"
)

const extensionOrigin = "chrome-extension://jcpbnaipmnmgfoelllffgphnfbbngdik"

func Routes(data store.Store) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
			snapshot, err := data.Snapshot(r.Context())
			if err != nil {
				storageError(w, err)
				return
			}
			reply(w, http.StatusOK, snapshot)
		})

		mux.HandleFunc("POST /requests", func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				ChannelID   string `json:"channelId"`
				ChannelName string `json:"channelName"`
				VideoID     string `json:"videoId"`
			}
			if !read(w, r, &in) {
				return
			}
			in.ChannelName = strings.TrimSpace(in.ChannelName)
			if !store.ValidChannel(in.ChannelID) || !store.ValidVideo(in.VideoID) || len(in.ChannelName) > 320 {
				http.Error(w, "invalid channel or video", http.StatusBadRequest)
				return
			}
			req, created, err := data.RequestApproval(r.Context(), in.ChannelID, in.ChannelName, in.VideoID)
			if errors.Is(err, store.ErrDecided) {
				http.Error(w, "channel already has a decision", http.StatusConflict)
				return
			}
			if err != nil {
				storageError(w, err)
				return
			}
			if created {
				reply(w, http.StatusCreated, req)
				return
			}
			reply(w, http.StatusOK, req)
		})

		mux.HandleFunc("POST /events", func(w http.ResponseWriter, r *http.Request) {
			var in store.Playback
			if !read(w, r, &in) {
				return
			}
			if !store.ValidVideo(in.VideoID) || (in.ChannelID != "" && !store.ValidChannel(in.ChannelID)) ||
				len(in.ChannelName) > 320 || (in.Decision != "denied" && in.Decision != "approved") ||
				(in.Decision == "approved" && (in.ChannelID == "" || in.Version == nil)) ||
				(in.Version != nil && *in.Version < 1) {
				http.Error(w, "invalid playback event", http.StatusBadRequest)
				return
			}
			if err := data.Record(r.Context(), in); err != nil {
				storageError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

		mux.HandleFunc("GET /heartbeat", func(w http.ResponseWriter, r *http.Request) {
			observation, err := data.LastHeartbeat(r.Context())
			if err != nil {
				storageError(w, err)
				return
			}
			if observation == nil || !observation.Version.Valid {
				reply(w, http.StatusOK, map[string]any{"connectedAt": nil})
				return
			}
			reply(w, http.StatusOK, map[string]any{"connectedAt": observation.At})
		})

		mux.HandleFunc("POST /heartbeat", func(w http.ResponseWriter, r *http.Request) {
			var in store.Heartbeat
			if !read(w, r, &in) {
				return
			}
			if in.ExtensionVersion == "" || len(in.ExtensionVersion) > 32 ||
				(in.ConfigurationVersion != nil && *in.ConfigurationVersion < 1) || (in.ActiveTabs != nil && *in.ActiveTabs < 0) {
				http.Error(w, "invalid heartbeat", http.StatusBadRequest)
				return
			}
			if err := data.Beat(r.Context(), in); err != nil {
				storageError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

func read(w http.ResponseWriter, r *http.Request, dest any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func storageError(w http.ResponseWriter, err error) {
	log.Printf("local API database operation failed: %v", err)
	http.Error(w, "database unavailable", http.StatusServiceUnavailable)
}

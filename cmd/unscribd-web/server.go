package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/StefuDev/unscribd/internal/unscribd"
)

func newHandler(manager *Manager) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageTemplate.Execute(w, nil)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			URL    string `json:"url"`
			Format string `json:"format"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if _, err := unscribd.ExtractDocumentID(in.URL); err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		if in.Format != "text" {
			in.Format = "pdf"
		}
		job := manager.Add(in.URL, in.Format)
		_, pos, total, _ := manager.View(job.ID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{"id": job.ID, "status": job.Status, "message": job.Message, "position": pos, "queue_total": total})
	})
	mux.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			http.NotFound(w, r)
			return
		}
		job, pos, total, ok := manager.View(parts[0])
		if !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 2 && parts[1] == "file" {
			if job.Status != "complete" {
				http.NotFound(w, r)
				return
			}
			name := filepath.Base(job.Output)
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			http.ServeFile(w, r, job.Output)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": job.ID, "status": job.Status, "message": job.Message, "position": pos, "queue_total": total})
	})
	return mux
}

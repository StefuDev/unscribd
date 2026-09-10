package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/StefuDev/unscribd/internal/unscribd"
)

const ttl = time.Minute

type Job struct {
	ID      string
	URL     string
	Format  string
	Status  string
	Message string
	Dir     string
	Output  string
	Created time.Time
	key     string
}

type Manager struct {
	mu    sync.Mutex
	jobs  map[string]*Job
	byKey map[string]string
	queue []string
	wake  chan struct{}
	root  string
}

func newID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func NewManager(root string) *Manager {
	manager := &Manager{
		jobs:  map[string]*Job{},
		byKey: map[string]string{},
		wake:  make(chan struct{}, 1),
		root:  root,
	}
	go manager.worker()
	return manager
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) Add(url, format string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.TrimSpace(url) + "\x00" + format
	if id := m.byKey[key]; id != "" {
		if existing := m.jobs[id]; existing != nil && existing.Status != "failed" {
			return existing
		}
	}
	id := newID()
	job := &Job{
		ID:      id,
		URL:     url,
		Format:  format,
		Status:  "queued",
		Created: time.Now(),
		Dir:     filepath.Join(m.root, id),
		Output:  filepath.Join(m.root, id, "document.pdf"),
		key:     key,
	}
	m.jobs[id] = job
	m.byKey[key] = id
	m.queue = append(m.queue, id)
	m.updateQueuedLocked()
	m.signal()
	return job
}

func (m *Manager) updateQueuedLocked() {
	for i, id := range m.queue {
		if j := m.jobs[id]; j != nil && j.Status == "queued" {
			if i == 0 {
				j.Message = "Next in line."
			} else {
				j.Message = fmt.Sprintf("Queued. %d download%s ahead.", i, plural(i))
			}
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (m *Manager) View(id string) (*Job, int, int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, 0, 0, false
	}
	pos, total := 0, 0
	if j.Status == "queued" {
		total = len(m.queue)
		for i, q := range m.queue {
			if q == id {
				pos = i + 1
				break
			}
		}
	}
	job := *j
	return &job, pos, total, true
}

func (m *Manager) worker() {
	for {
		m.mu.Lock()
		if len(m.queue) == 0 {
			m.mu.Unlock()
			<-m.wake
			continue
		}
		id := m.queue[0]
		m.queue = m.queue[1:]
		job := m.jobs[id]
		if job != nil {
			job.Status = "running"
			job.Message = "Opening the document..."
		}
		m.updateQueuedLocked()
		m.mu.Unlock()
		if job != nil {
			m.run(job)
		}
	}
}

func (m *Manager) run(job *Job) {
	if err := os.MkdirAll(job.Dir, 0755); err != nil {
		m.mu.Lock()
		job.Status = "failed"
		job.Message = "The download could not be completed. Please try again."
		m.mu.Unlock()
		return
	}
	cacheDir := filepath.Join(job.Dir, "cache")
	defer os.RemoveAll(cacheDir)
	result, err := unscribd.Download(context.Background(), job.URL, unscribd.Options{
		Format:      job.Format,
		Output:      job.Output,
		ImagesDir:   filepath.Join(job.Dir, "images"),
		CacheDir:    cacheDir,
		CacheBytes:  128 << 20,
		Concurrency: 4,
	})
	m.mu.Lock()
	if err != nil {
		log.Printf("job %s failed: %v", job.ID, err)
		job.Status = "failed"
		job.Message = "The download could not be completed. Please try again."
	} else {
		name := unscribd.SanitizeFilename(result.Title)
		if job.Format == "text" {
			name += "_text"
		}
		job.Output = filepath.Join(job.Dir, name+".pdf")
		if result.Output != job.Output {
			if renameErr := os.Rename(result.Output, job.Output); renameErr != nil {
				job.Status = "failed"
				job.Message = "The download could not be completed. Please try again."
				m.mu.Unlock()
				return
			}
		}
		_ = os.RemoveAll(filepath.Join(job.Dir, "images"))
		job.Status = "complete"
		job.Message = "Your file is ready."
	}
	m.mu.Unlock()
	go func(id string) {
		time.Sleep(ttl)
		m.mu.Lock()
		if j := m.jobs[id]; j != nil && j.Status != "running" && j.Status != "queued" {
			_ = os.RemoveAll(j.Dir)
			if m.byKey[j.key] == id {
				delete(m.byKey, j.key)
			}
			delete(m.jobs, id)
		}
		m.mu.Unlock()
	}(job.ID)
}

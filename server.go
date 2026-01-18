package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"
)

type Task struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	MaxAge        int        `json:"maxAge"`
	AlertSchedule string     `json:"alertSchedule"`
	LastActed     *time.Time `json:"lastActed"`
	LastAlerted   *time.Time `json:"lastAlerted,omitempty"`
	Muted         bool       `json:"muted"`
	Alert         bool       `json:"alert"`
}

func runServer(ctx context.Context) {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Public routes (no auth required)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	r.Get("/mute/{id}/{token}", MuteTask)
	r.Get("/", ListTasks)

	// Protected routes (require auth token)
	r.Group(func(r chi.Router) {
		r.Use(WithToken)
		r.Post("/{id}", ActTask)
		r.Get("/{id}", GetTask)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", os.Getenv("PORT")),
		Handler: r,
	}

	go func() {
		fmt.Printf("web server listening on :%s\n", os.Getenv("PORT"))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("server error: %v\n", err)
		}
	}()

	<-ctx.Done()
	fmt.Println("stopping web server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("server shutdown error: %v\n", err)
	} else {
		fmt.Println("web server stopped gracefully")
	}
}

// smol debug
func ListTasks(w http.ResponseWriter, r *http.Request) {
	var tasks []Task
	if err := db.Find(&tasks).Error; err != nil {
		log.Printf("failed to list tasks: %v", err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func ActTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var task Task
	if err := db.First(&task, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, http.StatusText(404), 404)
			return
		}
		log.Printf("failed to check task %s: %v", id, err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	now := time.Now()
	if err := db.Model(&task).Updates(map[string]any{
		"last_acted":   now,
		"last_alerted": nil,
		"muted":        false,
		"alert":        false,
	}).Error; err != nil {
		log.Printf("failed to update task %s: %v", id, err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	log.Printf("task %s acted, cleared muted and alert state\n", id)
	w.Write([]byte(http.StatusText(200)))
}

func MuteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	token := chi.URLParam(r, "token")

	if !verifyMuteToken(id, token) {
		http.Error(w, "invalid token", 403)
		return
	}

	var task Task
	if err := db.First(&task, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "task not found", 404)
			return
		}
		log.Printf("failed to check task %s: %v", id, err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	if err := db.Model(&task).Update("muted", true).Error; err != nil {
		log.Printf("failed to mute task %s: %v\n", id, err)
		http.Error(w, "failed to mute task", 500)
		return
	}

	log.Printf("task %s has been muted\n", id)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	fmt.Fprintf(w, "Alerting for task %s is now muted.", id)
}

func GetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var task Task
	if err := db.First(&task, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, http.StatusText(404), 404)
			return
		}
		log.Printf("failed to get state for task %s: %v\n", id, err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(task); err != nil {
		log.Printf("failed to encode state for task %s: %v\n", id, err)
		http.Error(w, http.StatusText(500), 500)
		return
	}
}

func WithToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("x-api-token")
		if token != os.Getenv("TOKEN") {
			http.Error(w, http.StatusText(403), 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}

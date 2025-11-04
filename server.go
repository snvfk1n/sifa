package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type TargetState struct {
	Id            string     `json:"id"`
	LastActed     *time.Time `json:"lastActed"`
	Muted         bool       `json:"muted"`
	Alert         bool       `json:"alert"`
	MaxAge        int        `json:"maxAge"`
	AlertSchedule string     `json:"alertSchedule"`
	Email         string     `json:"email"`
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
	r.Get("/mute/{id}/{token}", MuteTarget)

	// Protected routes (require auth token)
	r.Group(func(r chi.Router) {
		r.Use(WithToken)
		r.Post("/{id}", ActTarget)
		r.Get("/{id}", GetTarget)
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

func ActTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx := slices.IndexFunc(config.Targets, func(t Target) bool { return t.Id == id })
	if idx < 0 {
		http.Error(w, http.StatusText(404), 404)
		return
	}
	target := config.Targets[idx]

	err := db.Update(func(txn *badger.Txn) error {
		now, err := time.Now().MarshalBinary()
		if err != nil {
			return err
		}
		if err = txn.Set([]byte(target.Id), now); err != nil {
			return err
		}

		// clear muted state (if exists)
		if err = txn.Delete([]byte(target.Id + ":muted")); err != nil && err != badger.ErrKeyNotFound {
			return err
		}

		// clear alert state (if exists) - target is back to normal
		if err = txn.Delete([]byte(target.Id + ":alert")); err != nil && err != badger.ErrKeyNotFound {
			return err
		}

		return nil
	})
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	log.Printf("target %s acted, cleared muted and alert state\n", target.Id)
	w.Write([]byte(http.StatusText(200)))
}

func MuteTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	token := chi.URLParam(r, "token")

	if !verifyMuteToken(id, token) {
		http.Error(w, "invalid token", 403)
		return
	}

	idx := slices.IndexFunc(config.Targets, func(t Target) bool { return t.Id == id })
	if idx < 0 {
		http.Error(w, "target not found", 404)
		return
	}

	err := db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(id+":muted"), []byte("1"))
	})
	if err != nil {
		log.Printf("failed to mute target %s: %v\n", id, err)
		http.Error(w, "failed to mute target", 500)
		return
	}

	log.Printf("target %s has been muted\n", id)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	fmt.Fprintf(w, "Alerting for target %s is now muted.", id)
}

func GetTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx := slices.IndexFunc(config.Targets, func(t Target) bool { return t.Id == id })
	if idx < 0 {
		http.Error(w, http.StatusText(404), 404)
		return
	}
	target := config.Targets[idx]

	state := TargetState{
		Id:            target.Id,
		MaxAge:        target.MaxAge,
		AlertSchedule: target.AlertSchedule,
		Email:         target.Email,
	}

	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(target.Id))
		if err == nil {
			err = item.Value(func(val []byte) error {
				var t time.Time
				if err := t.UnmarshalBinary(val); err != nil {
					return err
				}
				state.LastActed = &t
				return nil
			})
			if err != nil {
				return err
			}
		} else if err != badger.ErrKeyNotFound {
			return err
		}

		// Get muted status
		_, err = txn.Get([]byte(target.Id + ":muted"))
		if err == nil {
			state.Muted = true
		} else if err != badger.ErrKeyNotFound {
			return err
		}

		// Get alert status
		_, err = txn.Get([]byte(target.Id + ":alert"))
		if err == nil {
			state.Alert = true
		} else if err != badger.ErrKeyNotFound {
			return err
		}

		return nil
	})

	if err != nil {
		log.Printf("failed to get state for target %s: %v\n", id, err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state); err != nil {
		log.Printf("failed to encode state for target %s: %v\n", id, err)
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

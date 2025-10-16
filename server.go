package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

type Target struct {
	Id            string `json:"id"`
	MaxAge        int    `json:"maxAge"`
	AlertSchedule string `json:"alertSchedule"`
	Email         string `json:"email"`
}
type Config struct {
	Targets []Target `json:"targets"`
}

var config Config
var db *badger.DB

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	jsonFile, err := os.Open("config.json")
	if err != nil {
		fmt.Println(err)
	}
	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)
	json.Unmarshal(byteValue, &config)

	fmt.Printf("found %d targets\n", len(config.Targets))

	db, err = badger.Open(badger.DefaultOptions("./db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create a context that cancels on interrupt signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// WaitGroup to wait for both functions to finish
	var wg sync.WaitGroup

	// Start both long-running functions
	wg.Add(2)
	go func() {
		defer wg.Done()
		runScheduler(ctx)
	}()

	go func() {
		defer wg.Done()
		runServer(ctx)
	}()

	go func() {
		<-sigChan
		fmt.Println("\nreceived interrupt signal, shutting down...")
		cancel()
	}()

	wg.Wait()
	fmt.Println("all functions completed, exiting")
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
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", os.Getenv("PORT")),
		Handler: r,
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("web server listening on :%s\n", os.Getenv("PORT"))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("server error: %v\n", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	fmt.Println("stopping web server...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		// Update last acting timestamp
		now, err := time.Now().MarshalBinary()
		if err != nil {
			return err
		}
		if err = txn.Set([]byte(target.Id), now); err != nil {
			return err
		}

		// Clear muted state (if exists)
		if err = txn.Delete([]byte(target.Id + ":muted")); err != nil && err != badger.ErrKeyNotFound {
			return err
		}

		// Clear alert state (if exists) - target is back to normal
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

	// Verify the token
	if !verifyMuteToken(id, token) {
		http.Error(w, "invalid token", 403)
		return
	}

	// Check if target exists
	idx := slices.IndexFunc(config.Targets, func(t Target) bool { return t.Id == id })
	if idx < 0 {
		http.Error(w, "target not found", 404)
		return
	}

	// Set muted flag in database
	err := db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(id+":muted"), []byte("1"))
	})
	if err != nil {
		log.Printf("failed to mute target %s: %v\n", id, err)
		http.Error(w, "failed to mute target", 500)
		return
	}

	log.Printf("target %s has been muted\n", id)

	// Return HTML success page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	fmt.Fprintf(w, "Alerting for target %s is now muted.", id)
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

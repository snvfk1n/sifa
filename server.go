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

	"github.com/adhocore/gronx"
	"github.com/adhocore/gronx/pkg/tasker"
	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

type Target struct {
	Id            string `json:"id"`
	MaxAge        int    `json:"maxAge"`
	AlertSchedule string `json:"alertSchedule"`
}
type Config struct {
	Targets []Target `json:"targets"`
}

var config Config
var db *badger.DB

var gron = gronx.New()

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	jsonFile, err := os.Open("config.json")
	if err != nil {
		fmt.Println(err)
	}
	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)
	json.Unmarshal(byteValue, &config)

	fmt.Printf("Found %d targets\n", len(config.Targets))

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

func runScheduler(ctx context.Context) {
	t := tasker.New(tasker.Option{Verbose: true})

	// taskr.Task("@hourly", func(ctx context.Context) (int, error) {
	t.Task("* * * * *", func(ctx context.Context) (int, error) {
		log.Println("Running alert task")

		for _, target := range config.Targets {
			var lastActing time.Time
			err := db.View(
				func(tx *badger.Txn) error {
					item, err := tx.Get([]byte(target.Id))
					if err != nil {
						return fmt.Errorf("Getting value: %w", err)
					}
					return item.Value(func(val []byte) error {
						if err := lastActing.UnmarshalBinary(val); err != nil {
							return err
						}
						return nil
					})
				})

			if err != nil {
				log.Printf("Target %s, not acted upon\n", target.Id)
				continue
			}
			log.Printf("target %s, last acted upon %s, max age %d, alert schedule %s\n", target.Id, lastActing.Format(time.RFC3339), target.MaxAge, target.AlertSchedule)

			now := time.Now()
			diff := now.Sub(lastActing)
			if diff.Seconds() > float64(target.MaxAge) {
				log.Printf("ALERT SHOULD BE SENT NOW!!1 (%f, %d)\n", diff.Seconds(), target.MaxAge)
			}

			due, err := gron.IsDue(target.AlertSchedule)
			if err != nil {
				log.Panic(err)
			}
			if due {
				log.Printf("NOTIFICATION IS DUE!!!\n")
			} else {
				log.Println("NOTIFICATION IS NOT DUE")
			}
		}

		// then return exit code and error, for eg: if everything okay
		return 0, nil
	})

	// Run the scheduler in a goroutine
	schedulerDone := make(chan struct{})
	go func() {
		t.Run()
		close(schedulerDone)
	}()

	// Wait for context cancellation
	<-ctx.Done()
	fmt.Println("Stopping task scheduler...")
	t.Stop()

	// Wait for scheduler to fully stop
	<-schedulerDone
	fmt.Println("Task scheduler stopped")
}

func runServer(ctx context.Context) {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(WithToken)

	r.Post("/{id}", TargetActed)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", os.Getenv("PORT")),
		Handler: r,
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("Web server listening on :%s\n", os.Getenv("PORT"))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	fmt.Println("Stopping web server...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("Server shutdown error: %v\n", err)
	} else {
		fmt.Println("Web server stopped gracefully")
	}
}

func TargetActed(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx := slices.IndexFunc(config.Targets, func(t Target) bool { return t.Id == id })
	if idx < 0 {
		http.Error(w, http.StatusText(404), 404)
		return
	}
	target := config.Targets[idx]

	err := db.Update(func(txn *badger.Txn) error {
		now, err := time.Now().MarshalBinary()
		err = txn.Set([]byte(target.Id), now)
		return err
	})
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	w.Write([]byte(http.StatusText(200)))
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

// /POST webhook/:id (x-secret: <abc>)
// if id doesn't exist, reply with 404
// db.put(id, timestamp), reply with 200
//
// also, some kind of daily (?) cron job that checks the latest target call
// could use cron syntax https://github.com/adhocore/gronx
// if target is due
//   send email via postmark (https://postmarkapp.com/developer/api/email-api):
//
//   Hello, the target "$TARGET_NAME" hasn't been called in the required amount of time. Please check your systems! Last call was: $TARGET_LAST_CALL.
//

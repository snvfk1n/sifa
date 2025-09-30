package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

type Target struct {
	Id       string `json:"id"`
	Schedule string `json:"schedule"`
}
type Config struct {
	Targets []Target `json:"targets"`
}

var config Config
var db *badger.DB

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

	for _, target := range config.Targets {
		var t time.Time
		err = db.View(
			func(tx *badger.Txn) error {
				item, err := tx.Get([]byte(target.Id))
				if err != nil {
					return fmt.Errorf("getting value: %w", err)
				}
				return item.Value(func(val []byte) error {
					if err := t.UnmarshalBinary(val); err != nil {
						return err
					}
					return nil
				})
			})

		if err != nil {
			log.Printf("target %s, not acted upon\n", target.Id)
			continue
		}
		log.Printf("target %s, last acted upon %s\n", target.Id, t.Format(time.RFC3339))
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(WithToken)

	r.Post("/{id}", TargetActed)
	err = http.ListenAndServe(fmt.Sprintf(":%s", os.Getenv("PORT")), r)
	if err != nil {
		log.Fatal(err)
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/dgraph-io/badger/v4"
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
	if err != nil && !os.IsNotExist(err) {
		log.Fatal("error loading .env file: ", err)
	}

	jsonFile, err := os.Open("config.json")
	if err != nil {
		fmt.Println(err)
	}
	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)
	json.Unmarshal(byteValue, &config)

	fmt.Printf("found %d targets\n", len(config.Targets))

	opts := badger.DefaultOptions("./db")
	opts.Logger = nil
	db, err = badger.Open(opts)
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

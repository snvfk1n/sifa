package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/joho/godotenv"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func main() {
	err := godotenv.Load()
	if err != nil && !os.IsNotExist(err) {
		log.Fatal("error loading .env file: ", err)
	}

	// Check if data is being piped via stdin
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Data is being piped, run ingest mode
		if err := runIngest(); err != nil {
			log.Fatal("ingest failed: ", err)
		}
		return
	}

	if err := initAlerting(); err != nil {
		log.Fatal("error loading shoutrrr: ", err)
	}

	db, err = gorm.Open(sqlite.Open("./sifa.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatal("failed to open database: ", err)
	}

	if err := db.AutoMigrate(&Task{}); err != nil {
		log.Fatal("failed to migrate database: ", err)
	}

	var count int64
	db.Model(&Task{}).Count(&count)
	fmt.Printf("found %d tasks\n", count)

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

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/adhocore/gronx"
	"github.com/adhocore/gronx/pkg/tasker"
	"github.com/dgraph-io/badger/v4"
	"github.com/dustin/go-humanize"
)

var gron = gronx.New()

func checkTargets(ctx context.Context) (int, error) {
	log.Println("running alert task")

	for _, target := range config.Targets {
		var lastActing time.Time
		var lastAlert time.Time
		var isMuted bool
		err := db.View(
			func(tx *badger.Txn) error {
				// Get last acting timestamp
				item, err := tx.Get([]byte(target.Id))
				if err != nil {
					return fmt.Errorf("getting value: %w", err)
				}
				if err := item.Value(func(val []byte) error {
					if err := lastActing.UnmarshalBinary(val); err != nil {
						return err
					}
					return nil
				}); err != nil {
					return err
				}

				// Get last alert timestamp (may not exist)
				alertItem, err := tx.Get([]byte(target.Id + ":alert"))
				if err != nil && err != badger.ErrKeyNotFound {
					return fmt.Errorf("getting alert value: %w", err)
				}
				if err == nil {
					if err := alertItem.Value(func(val []byte) error {
						if err := lastAlert.UnmarshalBinary(val); err != nil {
							return err
						}
						return nil
					}); err != nil {
						return err
					}
				}

				// Check if target is muted
				_, mutedErr := tx.Get([]byte(target.Id + ":muted"))
				if mutedErr != nil && mutedErr != badger.ErrKeyNotFound {
					return fmt.Errorf("getting muted value: %w", mutedErr)
				}
				if mutedErr == nil {
					isMuted = true
				}

				return nil
			})

		if err != nil {
			log.Printf("target %s, not acted upon yet\n", target.Id)
			continue
		}
		log.Printf("target %s, last acted upon %s, max age %d, alert schedule %s\n", target.Id, lastActing.Format(time.RFC3339), target.MaxAge, target.AlertSchedule)

		now := time.Now()
		diff := now.Sub(lastActing)

		// Check if target is overdue (hasn't acted within maxAge)
		if diff.Seconds() > float64(target.MaxAge) {
			// Check if target is muted
			if isMuted {
				log.Printf("target %s is overdue but muted, skipping alert\n", target.Id)
				continue
			}

			// Target is overdue, check if we should send an alert based on alertSchedule
			shouldAlert := false

			if lastAlert.IsZero() {
				// Never sent an alert before, send one now
				shouldAlert = true
				log.Printf("target %s is overdue (%.0f seconds since last action), sending first alert\n",
					target.Id, diff.Seconds())
			} else {
				// Check if alertSchedule is due since last alert
				due, err := gron.IsDue(target.AlertSchedule)
				if err != nil {
					log.Printf("error checking alert schedule for %s: %v\n", target.Id, err)
					continue
				}

				if due {
					// Schedule is due right now, but check we haven't already alerted in this period
					// Minimum 1 hour between alerts to prevent spam during the same cron window
					timeSinceLastAlert := time.Since(lastAlert)
					minInterval := 1 * time.Hour

					if timeSinceLastAlert >= minInterval {
						shouldAlert = true
						log.Printf("target %s is overdue (%.0f seconds) and alert schedule is due (last alert %.0f minutes ago)\n",
							target.Id, diff.Seconds(), timeSinceLastAlert.Minutes())
					} else {
						log.Printf("target %s: alert schedule is due but already alerted recently (%.0f minutes ago), skipping\n",
							target.Id, timeSinceLastAlert.Minutes())
					}
				} else {
					log.Printf("target %s is overdue but alert schedule not due yet (last alert: %s)\n",
						target.Id, lastAlert.Format(time.RFC3339))
				}
			}

			if shouldAlert {
				// Generate mute link
				muteToken := generateMuteToken(target.Id)
				muteURL := fmt.Sprintf("%s/mute/%s/%s", os.Getenv("SIFA_URL"), target.Id, muteToken)

				// Format the duration for human readability using go-humanize
				lastActedTime := now.Add(-diff)
				formattedDuration := humanize.Time(lastActedTime)

				subject := fmt.Sprintf("Alert: Target %s is overdue", target.Id)
				message := fmt.Sprintf("Target %s has not acted since %s.\n\nTo mute these alerts until the target acts again, click here:\n%s",
					target.Id, formattedDuration, muteURL)

				if err := sendAlert(subject, message); err != nil {
					log.Printf("failed to send alert for target %s: %v\n", target.Id, err)
				} else {
					log.Printf("alert sent for target %s\n", target.Id)

					// Store the alert timestamp in BadgerDB (persists across restarts)
					if err := db.Update(func(txn *badger.Txn) error {
						alertTime, err := time.Now().MarshalBinary()
						if err != nil {
							return err
						}
						return txn.Set([]byte(target.Id+":alert"), alertTime)
					}); err != nil {
						log.Printf("failed to store alert timestamp: %v\n", err)
					}
				}
			}
		} else {
			// Target is not overdue, clear any previous alert state if it exists
			if !lastAlert.IsZero() {
				log.Printf("target %s back to normal (acted %s ago), clearing alert state\n",
					target.Id, diff.Round(time.Second))
				if err := db.Update(func(txn *badger.Txn) error {
					return txn.Delete([]byte(target.Id + ":alert"))
				}); err != nil {
					log.Printf("failed to clear alert timestamp: %v\n", err)
				}
			}
		}
	}

	// then return exit code and error, for eg: if everything okay
	return 0, nil
}

func runScheduler(ctx context.Context) {
	// Run alert check immediately on startup
	log.Println("running initial alert check on startup...")
	if _, err := checkTargets(ctx); err != nil {
		log.Printf("initial alert check failed: %v\n", err)
	}

	t := tasker.New(tasker.Option{Verbose: true})

	t.Task("@hourly", checkTargets)

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

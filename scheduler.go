package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/adhocore/gronx"
	"github.com/adhocore/gronx/pkg/tasker"
	"github.com/dustin/go-humanize"
)

var gron = gronx.New()

func checkTasks(ctx context.Context) (int, error) {
	log.Println("running alert task")

	var tasks []Task
	if err := db.Find(&tasks).Error; err != nil {
		log.Printf("failed to load tasks: %v\n", err)
		return 1, err
	}

	for _, task := range tasks {
		if task.LastActed == nil {
			log.Printf("task %s, not acted upon yet\n", task.ID)
			continue
		}

		log.Printf("task %s, last acted upon %s, max age %d, alert schedule %s\n",
			task.ID, task.LastActed.Format(time.RFC3339), task.MaxAge, task.AlertSchedule)

		now := time.Now()
		diff := now.Sub(*task.LastActed)

		// Check if task is overdue (hasn't acted within maxAge)
		if diff.Seconds() > float64(task.MaxAge) {
			// Check if task is muted
			if task.Muted {
				log.Printf("task %s is overdue but muted, skipping alert\n", task.ID)
				continue
			}

			// Task is overdue, check if we should send an alert based on alertSchedule
			shouldAlert := false

			if task.LastAlerted == nil {
				// Never sent an alert before, send one now
				shouldAlert = true
				log.Printf("task %s is overdue (%.0f seconds since last action), sending first alert\n",
					task.ID, diff.Seconds())
			} else {
				// Check if alertSchedule is due since last alert
				due, err := gron.IsDue(task.AlertSchedule)
				if err != nil {
					log.Printf("error checking alert schedule for %s: %v\n", task.ID, err)
					continue
				}

				if due {
					// Schedule is due right now, but check we haven't already alerted in this period
					// Minimum 1 hour between alerts to prevent spam during the same cron window
					timeSinceLastAlert := time.Since(*task.LastAlerted)
					minInterval := 1 * time.Hour

					if timeSinceLastAlert >= minInterval {
						shouldAlert = true
						log.Printf("task %s is overdue (%.0f seconds) and alert schedule is due (last alert %.0f minutes ago)\n",
							task.ID, diff.Seconds(), timeSinceLastAlert.Minutes())
					} else {
						log.Printf("task %s: alert schedule is due but already alerted recently (%.0f minutes ago), skipping\n",
							task.ID, timeSinceLastAlert.Minutes())
					}
				} else {
					log.Printf("task %s is overdue but alert schedule not due yet (last alert: %s)\n",
						task.ID, task.LastAlerted.Format(time.RFC3339))
				}
			}

			if shouldAlert {
				// Generate mute link
				muteToken := generateMuteToken(task.ID)
				muteURL := fmt.Sprintf("%s/mute/%s/%s", os.Getenv("SIFA_URL"), task.ID, muteToken)

				// Format the duration for human readability using go-humanize
				lastActedTime := now.Add(-diff)
				formattedDuration := humanize.Time(lastActedTime)

				subject := fmt.Sprintf("Alert: Task %s is overdue", task.ID)
				message := fmt.Sprintf("Task %s has not acted since %s.\n\nTo mute these alerts until the task acts again, click here:\n%s",
					task.ID, formattedDuration, muteURL)

				if err := sendAlert(subject, message); err != nil {
					log.Printf("failed to send alert for task %s: %v\n", task.ID, err)
				} else {
					log.Printf("alert sent for task %s\n", task.ID)

					// Store the alert timestamp
					now := time.Now()
					if err := db.Model(&task).Updates(map[string]interface{}{
						"last_alerted": now,
						"alert":        true,
					}).Error; err != nil {
						log.Printf("failed to store alert timestamp: %v\n", err)
					}
				}
			}
		} else {
			// Task is not overdue, clear any previous alert state if it exists
			if task.LastAlerted != nil {
				log.Printf("task %s back to normal (acted %s ago), clearing alert state\n",
					task.ID, diff.Round(time.Second))
				if err := db.Model(&task).Updates(map[string]interface{}{
					"last_alerted": nil,
					"alert":        false,
				}).Error; err != nil {
					log.Printf("failed to clear alert timestamp: %v\n", err)
				}
			}
		}
	}

	return 0, nil
}

func runScheduler(ctx context.Context) {
	// Run alert check immediately on startup
	log.Println("running initial alert check on startup...")
	if _, err := checkTasks(ctx); err != nil {
		log.Printf("initial alert check failed: %v\n", err)
	}

	t := tasker.New(tasker.Option{Verbose: true})

	t.Task("@hourly", checkTasks)

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

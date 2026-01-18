package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type IngestTask struct {
	ID            string `json:"id"`
	MaxAge        int    `json:"maxAge"`
	AlertSchedule string `json:"alertSchedule"`
}

func runIngest() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	var tasks []IngestTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("failed to parse config JSON: %w", err)
	}

	db, err := gorm.Open(sqlite.Open("./sifa.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.AutoMigrate(&Task{}); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	for _, t := range tasks {
		task := Task{
			ID:            t.ID,
			MaxAge:        t.MaxAge,
			AlertSchedule: t.AlertSchedule,
		}

		// Upsert: insert or update on conflict
		result := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"max_age", "alert_schedule"}),
		}).Create(&task)

		if result.Error != nil {
			log.Printf("failed to upsert task %s: %v", t.ID, result.Error)
			continue
		}

		if result.RowsAffected > 0 {
			log.Printf("upserted task %s (maxAge=%d, alertSchedule=%s)", t.ID, t.MaxAge, t.AlertSchedule)
		}
	}

	fmt.Printf("ingested %d tasks\n", len(tasks))
	return nil
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/repository"
)

func main() {
	action := flag.String("action", "stats", "stats or replay")
	workspaceID := flag.Int64("workspace-id", 0, "workspace id")
	flag.Parse()

	if *workspaceID <= 0 {
		fmt.Fprintln(os.Stderr, "-workspace-id must be a positive integer")
		os.Exit(2)
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}

	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	repo := repository.NewPostgresEventRepository(db)
	switch *action {
	case "stats":
		stats, err := repo.Stats(*workspaceID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		encoded, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(encoded))
	case "replay":
		count, err := repo.ReplayDead(*workspaceID, time.Now())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("replayed=%d\n", count)
	default:
		fmt.Fprintln(os.Stderr, "-action must be stats or replay")
		os.Exit(2)
	}
}

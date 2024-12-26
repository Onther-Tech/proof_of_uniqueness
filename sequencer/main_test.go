package main

import (
	"os"
	"testing"
	"time"
	"tokamak-sybil-resistance/database"
	"tokamak-sybil-resistance/test"

	"github.com/jmoiron/sqlx"
)

var db *sqlx.DB

func TestMain(m *testing.M) {
	var err error
	db, err = database.InitSQLDB()
	if err != nil {
		panic(err)
	}
	result := m.Run()
	if err := db.Close(); err != nil {
		panic(err)
	}
	os.Exit(result)
}

func TestRunNode(t *testing.T) {
	t.Cleanup(func() {
		test.MigrationsDownTest(db)
	})

	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Set the os.Args to simulate the command-line arguments
	os.Args = []string{"main.test", "run", "--cfg", "cfg.toml"}

	// Run the application
	errChan := make(chan error)
	go func() {
		errChan <- RunApp()
	}()

	timer := time.NewTimer(3 * time.Second)
	select {
	case err := <-errChan:
		t.Fatalf("runApp() failed: %v", err)
	case <-timer.C:
	}
}

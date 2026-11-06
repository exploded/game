// Command loadtest measures how the site copes with many players
// (docs/capacity.md). It never runs against production or the repo's
// game.db: it fills a copy with made-up players, times the background jobs
// on it, and drives a test server running on that copy.
//
//	loadtest seed -db <copy>/game.db -players 1000 -out players.json
//	loadtest jobs -db <copy>/game.db
//	loadtest run  -base http://127.0.0.1:8999 -players players.json
//
// CI deploys only cmd/server, so this ships nowhere.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seedCmd(os.Args[2:])
	case "jobs":
		err = jobsCmd(os.Args[2:])
	case "run":
		err = runCmd(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadtest seed|jobs|run [flags]  (loadtest <cmd> -h for flags)")
	os.Exit(2)
}

// checkCopy refuses a database that looks real: one with a .env beside it,
// as in the repo and on the server. A copy belongs in a folder of its own.
func checkCopy(dbPath string) error {
	if dbPath == "" {
		return errors.New("-db is required: a copy of game.db in a folder of its own")
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(abs), ".env")); err == nil {
		return fmt.Errorf("%s has a .env beside it, so it looks like a real database; copy it to a folder of its own", abs)
	}
	return nil
}

// dbmigrate initializes the current database schema using the private owner credentials.
package main

import (
	"encoding/hex"
	"fmt"
	"github.com/johankoi91/monitor/runtime/center/internal/postgres"
	"os"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "dbmigrate accepts no legacy import or migration options")
		os.Exit(1)
	}
	key, err := hex.DecodeString(os.Getenv("AVOPS_STORAGE_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid storage key")
		os.Exit(1)
	}
	db, err := postgres.Open(os.Getenv("AVOPS_DATABASE_URL"), key, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema initialization failed")
		os.Exit(1)
	}
	defer db.Close()
	if err = db.CompactOperations(); err == nil {
		err = db.RemoveKeyNodeScopes()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema initialization failed")
		os.Exit(1)
	}
	fmt.Println("V1.0 database schema ready")
}

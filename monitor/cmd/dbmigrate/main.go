package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/johankoi91/monitor/runtime/internal/postgres"
	"os"
)

func main() {
	dir := flag.String("import-dir", "", "offline legacy center data directory")
	export := flag.String("export-dir", "", "new private legacy rollback export directory")
	flag.Parse()
	key, err := hex.DecodeString(os.Getenv("AVOPS_STORAGE_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid encryption key")
		os.Exit(1)
	}
	db, err := postgres.Open(os.Getenv("AVOPS_DATABASE_URL"), key, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	if *export != "" {
		if err = db.ExportLegacy(*export); err != nil {
			fmt.Fprintln(os.Stderr, "export failed", err)
			os.Exit(1)
		}
		fmt.Println("Private compatibility export complete; accounts remain in PostgreSQL")
		return
	}
	if *dir != "" {
		counts, err := db.ImportDirectory(*dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "import failed; transaction rolled back:", err)
			os.Exit(1)
		}
		b, _ := json.Marshal(counts)
		fmt.Println(string(b))
	} else {
		fmt.Println("PostgreSQL schema ready")
	}
}

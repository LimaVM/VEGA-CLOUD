package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "vega-cloud.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var isAdmin bool
	err = db.QueryRow("SELECT is_admin FROM users WHERE username = 'vega-admin'").Scan(&isAdmin)
	if err != nil {
		log.Fatalf("Erro ao consultar user: %v", err)
	}

	fmt.Printf("User vega-admin IsAdmin: %v\n", isAdmin)
}

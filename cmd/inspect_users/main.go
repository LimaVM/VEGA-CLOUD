package main

import (
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"vega-cloud/internal/database"
)

func main() {
	if err := database.Init("./vega-cloud.db"); err != nil {
		log.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	users, err := database.GetAllUsers()
	if err != nil {
		log.Fatalf("Failed to get users: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUsername\tAdmin\tPremium\tBanned\tCtd At")
	fmt.Fprintln(w, "--\t--------\t-----\t-------\t------\t------")

	for _, u := range users {
		fmt.Fprintf(w, "%d\t%s\t%v\t%v\t%v\t%s\n",
			u.ID, u.Username, u.IsAdmin, u.IsPremium, u.IsBanned, u.CreatedAt.Format("2006-01-02 15:04"))
	}
	w.Flush()
}

package service

import (
	"log"

	"gophre/pkg/store"
)

// Reset deletes every article and vote from the database.
func Reset() {
	if err := store.Reset(); err != nil {
		log.Printf("Error resetting data: %v\n", err)
	}
}

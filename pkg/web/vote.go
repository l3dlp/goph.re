package web

import (
	"log"
	"net/http"
	"time"

	"gophre/pkg/store"

	"github.com/gin-gonic/gin"
)

func Vote(c *gin.Context) {
	id := c.Param("id")
	voteValue := c.Param("vote")

	// Récupérer l'utilisateur courant
	user, isAuthenticated := GetCurrentUser(c)
	if !isAuthenticated {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Authentication required"})
		return
	}

	value := 0
	if voteValue == "GOOD" {
		value = 1
	} else if voteValue == "BAD" {
		value = -1
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid vote value"})
		return
	}

	// Enregistre le vote et recalcule le total de l'article dans la même transaction
	if err := store.Vote(id, user.ID, value, time.Now()); err != nil {
		log.Printf("Error saving vote: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to save vote"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

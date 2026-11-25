package web

import (
	"log"
	"net/http"

	"gophre/pkg/store"

	"github.com/gin-gonic/gin"
)

// wallPage renders wall.html with the chrome data every page needs and, for a
// logged-in user, their votes so the feed shows kept/skipped state on any device.
func wallPage(c *gin.Context, extra gin.H) {
	user, _ := GetCurrentUser(c)
	h := gin.H{"user": user, "page": "feed", "path": "", "votes": userVotes(user.ID)}
	for k, v := range extra {
		h[k] = v
	}
	all, _ := h["all"].(bool)
	h["meta"] = metaFor("feed", h["path"].(string), all)
	c.HTML(http.StatusOK, "wall.html", h)
}

// userVotes maps article IDs to "GOOD" or "BAD" for one user.
func userVotes(userID string) map[string]string {
	votes := map[string]string{}
	if userID == "" {
		return votes
	}
	all, err := store.UserVotes(userID)
	if err != nil {
		log.Printf("Error reading votes: %v.", err)
		return votes
	}
	for _, vote := range all {
		if vote.Value > 0 {
			votes[vote.ArticleID] = "GOOD"
		} else {
			votes[vote.ArticleID] = "BAD"
		}
	}
	return votes
}

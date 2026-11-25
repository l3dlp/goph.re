package store

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gophre/cmd/data"
)

// ImportJSON loads the legacy rss_posts.json and votes.json into an empty database.
// Article order is kept (rowid follows the file, newest first) and the cached vote
// totals are copied as-is, since older totals predate per-user votes.
func ImportJSON(postsPath, votesPath string) (articles, votes int, err error) {
	stats, err := GetStats()
	if err != nil {
		return 0, 0, err
	}
	if stats.Count > 0 {
		return 0, 0, fmt.Errorf("Database already holds %d articles; run reset first.", stats.Count)
	}

	file, err := os.ReadFile(postsPath)
	if err != nil {
		return 0, 0, fmt.Errorf("Error reading posts file: %v.", err)
	}
	var posts []data.Article
	if err := json.Unmarshal(file, &posts); err != nil {
		return 0, 0, fmt.Errorf("Error decoding posts: %v.", err)
	}
	inserted, err := Insert(posts)
	if err != nil {
		return 0, 0, err
	}

	file, err = os.ReadFile(votesPath)
	if os.IsNotExist(err) {
		return len(inserted), 0, nil
	}
	if err != nil {
		return len(inserted), 0, fmt.Errorf("Error reading votes file: %v.", err)
	}
	var all []data.Vote
	if err := json.Unmarshal(file, &all); err != nil {
		return len(inserted), 0, fmt.Errorf("Error decoding votes: %v.", err)
	}

	db, err := DB()
	if err != nil {
		return len(inserted), 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return len(inserted), 0, fmt.Errorf("Error starting transaction: %v.", err)
	}
	defer tx.Rollback()
	for _, v := range all {
		ts := v.Timestamp
		if ts.IsZero() {
			ts = time.Unix(0, 0)
		}
		_, err := tx.Exec(`INSERT INTO votes (article_id, user_id, value, ts) VALUES (?, ?, ?, ?)
			ON CONFLICT (article_id, user_id) DO UPDATE SET value = excluded.value, ts = excluded.ts`,
			v.ArticleID, v.UserID, v.Value, ts.UnixNano())
		if err != nil {
			return len(inserted), 0, fmt.Errorf("Error importing vote: %v.", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return len(inserted), 0, fmt.Errorf("Error committing votes: %v.", err)
	}
	return len(inserted), len(all), nil
}

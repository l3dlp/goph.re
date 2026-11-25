// Package store keeps articles and votes in a single SQLite database (env.DB),
// with an FTS5 index for search. Every process (serve, update) opens it in WAL
// mode, so readers never block the hourly update and writes are transactional.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"gophre/cmd/data"
	"gophre/env"

	"github.com/microcosm-cc/bluemonday"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

// Path is the database file; tests point it at a temporary file before the first call.
var Path = env.DB

var (
	openOnce sync.Once
	db       *sql.DB
	openErr  error

	ugc    = bluemonday.UGCPolicy()
	strict = bluemonday.StrictPolicy()
)

const schema = `
CREATE TABLE IF NOT EXISTS articles (
	id          TEXT PRIMARY KEY,
	url         TEXT NOT NULL UNIQUE,
	path        TEXT NOT NULL,
	source_name TEXT NOT NULL,
	source_url  TEXT NOT NULL,
	name        TEXT NOT NULL,
	resume      TEXT NOT NULL, -- sanitized with bluemonday's UGC policy on insert
	date        INTEGER NOT NULL, -- unix seconds, zero time when the feed gave none
	vote        INTEGER NOT NULL DEFAULT 0, -- cached sum of votes.value
	added       INTEGER NOT NULL -- unix seconds, when gophre first saw the article
);
CREATE INDEX IF NOT EXISTS articles_date ON articles(date DESC);
CREATE INDEX IF NOT EXISTS articles_path_date ON articles(path, date DESC);
CREATE INDEX IF NOT EXISTS articles_vote ON articles(vote) WHERE vote > 0;
CREATE INDEX IF NOT EXISTS articles_added ON articles(added);

-- Plain-text index of name and resume; rowid matches articles.rowid.
CREATE VIRTUAL TABLE IF NOT EXISTS articles_fts USING fts5(
	name, body, content='', contentless_delete=1,
	tokenize='porter unicode61 remove_diacritics 2'
);

CREATE TABLE IF NOT EXISTS votes (
	article_id TEXT NOT NULL,
	user_id    TEXT NOT NULL,
	value      INTEGER NOT NULL,
	ts         INTEGER NOT NULL, -- unix nanoseconds
	PRIMARY KEY (article_id, user_id)
);
CREATE INDEX IF NOT EXISTS votes_user ON votes(user_id, ts);
`

// DB opens the database once per process and creates the schema if needed.
func DB() (*sql.DB, error) {
	openOnce.Do(func() {
		dsn := "file:" + url.PathEscape(Path) +
			"?_pragma=busy_timeout(10000)&_pragma=journal_mode(wal)&_pragma=synchronous(normal)&_txlock=immediate"
		db, openErr = driver.Open(dsn, fts5.Register)
		if openErr != nil {
			openErr = fmt.Errorf("Error opening database: %v.", openErr)
			return
		}
		if _, err := db.Exec(schema); err != nil {
			openErr = fmt.Errorf("Error creating schema: %v.", err)
		}
	})
	return db, openErr
}

// Close releases the database; the next DB call will not reopen it.
func Close() error {
	if db == nil {
		return nil
	}
	return db.Close()
}

const articleColumns = `a.id, a.url, a.path, a.source_name, a.source_url, a.name, a.resume, a.date, a.vote`

// Light skips the resume for callers that only need titles, sources and dates.
const lightColumns = `a.id, a.url, a.path, a.source_name, a.source_url, a.name, '', a.date, a.vote`

func scanArticles(rows *sql.Rows) ([]data.Article, error) {
	defer rows.Close()
	articles := []data.Article{}
	for rows.Next() {
		var a data.Article
		var date int64
		if err := rows.Scan(&a.ID, &a.URL, &a.Source.Path, &a.Source.Name, &a.Source.URL, &a.Name, &a.Resume, &date, &a.Vote); err != nil {
			return nil, fmt.Errorf("Error reading article: %v.", err)
		}
		a.Date = time.Unix(date, 0).UTC()
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

func query(q string, args ...any) ([]data.Article, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("Error querying articles: %v.", err)
	}
	return scanArticles(rows)
}

// Recent returns one page of articles, newest first, optionally limited to one source path.
func Recent(path string, offset, limit int) ([]data.Article, error) {
	if path == "" {
		return query(`SELECT `+articleColumns+` FROM articles a ORDER BY a.date DESC, a.rowid LIMIT ? OFFSET ?`, limit, offset)
	}
	return query(`SELECT `+articleColumns+` FROM articles a WHERE a.path = ? ORDER BY a.date DESC, a.rowid LIMIT ? OFFSET ?`, path, limit, offset)
}

// Since returns every article dated at or after t, newest first.
func Since(t time.Time) ([]data.Article, error) {
	return query(`SELECT `+articleColumns+` FROM articles a WHERE a.date >= ? ORDER BY a.date DESC, a.rowid`, t.Unix())
}

// Timeline returns every article without its resume, newest first.
func Timeline() ([]data.Article, error) {
	return query(`SELECT ` + lightColumns + ` FROM articles a ORDER BY a.date DESC, a.rowid`)
}

// Get returns one article by ID; ok is false when it does not exist.
func Get(id string) (article data.Article, ok bool, err error) {
	articles, err := query(`SELECT `+articleColumns+` FROM articles a WHERE a.id = ?`, id)
	if err != nil || len(articles) == 0 {
		return data.Article{}, false, err
	}
	return articles[0], true, nil
}

// ByID returns the articles with the given IDs, keyed by ID.
func ByID(ids []string) (map[string]data.Article, error) {
	found := make(map[string]data.Article, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	db, err := DB()
	if err != nil {
		return nil, err
	}
	// json_each keeps a single statement whatever the number of IDs.
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("Error encoding IDs: %v.", err)
	}
	rows, err := db.Query(`SELECT `+articleColumns+` FROM articles a WHERE a.id IN (SELECT value FROM json_each(?))`, string(list))
	if err != nil {
		return nil, fmt.Errorf("Error querying articles: %v.", err)
	}
	articles, err := scanArticles(rows)
	for _, a := range articles {
		found[a.ID] = a
	}
	return found, err
}

// Search runs a full-text query over titles and resumes, best matches first.
// Each word of q must match, as a prefix, so "gorout" finds "goroutines".
func Search(q string, offset, limit int) ([]data.Article, error) {
	match := matchQuery(q)
	if match == "" {
		return []data.Article{}, nil
	}
	// Title hits weigh ten times more than resume hits.
	return query(`SELECT `+articleColumns+` FROM articles_fts f JOIN articles a ON a.rowid = f.rowid
		WHERE articles_fts MATCH ? ORDER BY bm25(articles_fts, 10.0, 1.0), a.date DESC LIMIT ? OFFSET ?`,
		match, limit, offset)
}

// matchQuery turns free text into a safe FTS5 query: quoted prefix terms, ANDed.
func matchQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	terms := make([]string, 0, len(words))
	for _, w := range words {
		terms = append(terms, `"`+w+`"*`)
	}
	return strings.Join(terms, " ")
}

// Stats summarizes the archive: article count, newest article date and last insert time.
type Stats struct {
	Count       int
	LastArticle time.Time
	LastAdded   time.Time
}

// GetStats returns the archive summary; it is cheap enough to serve as a cache key.
func GetStats() (Stats, error) {
	db, err := DB()
	if err != nil {
		return Stats{}, err
	}
	var s Stats
	var last, added sql.NullInt64
	err = db.QueryRow(`SELECT (SELECT count(*) FROM articles), (SELECT max(date) FROM articles), (SELECT max(added) FROM articles)`).Scan(&s.Count, &last, &added)
	if err != nil {
		return Stats{}, fmt.Errorf("Error reading stats: %v.", err)
	}
	if last.Valid {
		s.LastArticle = time.Unix(last.Int64, 0).UTC()
	}
	if added.Valid {
		s.LastAdded = time.Unix(added.Int64, 0)
	}
	return s, nil
}

// plainText strips markup and entities, for the search index.
func plainText(s string) string {
	s = html.UnescapeString(strict.Sanitize(s))
	return strings.Join(strings.Fields(s), " ")
}

// Insert adds the articles whose URL is not known yet, sanitizing their resume,
// and returns the ones actually inserted (sanitized), in input order.
func Insert(articles []data.Article) ([]data.Article, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("Error starting transaction: %v.", err)
	}
	defer tx.Rollback()

	insert, err := tx.Prepare(`INSERT INTO articles (id, url, path, source_name, source_url, name, resume, date, vote, added)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return nil, fmt.Errorf("Error preparing insert: %v.", err)
	}
	index, err := tx.Prepare(`INSERT INTO articles_fts (rowid, name, body) VALUES (?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("Error preparing index: %v.", err)
	}

	now := time.Now().Unix()
	var inserted []data.Article
	for _, a := range articles {
		a.Resume = ugc.Sanitize(a.Resume)
		res, err := insert.Exec(a.ID, a.URL, a.Source.Path, a.Source.Name, a.Source.URL, a.Name, a.Resume, a.Date.Unix(), a.Vote, now)
		if err != nil {
			return nil, fmt.Errorf("Error inserting article %s: %v.", a.URL, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // URL (or ID) already known
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("Error reading rowid: %v.", err)
		}
		if _, err := index.Exec(rowid, plainText(a.Name), plainText(a.Resume)); err != nil {
			return nil, fmt.Errorf("Error indexing article %s: %v.", a.URL, err)
		}
		inserted = append(inserted, a)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("Error committing articles: %v.", err)
	}
	return inserted, nil
}

// Vote records one user's vote (+1 or -1) on an article, replacing any previous
// one, and refreshes the article's cached total.
func Vote(articleID, userID string, value int, at time.Time) error {
	db, err := DB()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("Error starting transaction: %v.", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO votes (article_id, user_id, value, ts) VALUES (?, ?, ?, ?)
		ON CONFLICT (article_id, user_id) DO UPDATE SET value = excluded.value, ts = excluded.ts`,
		articleID, userID, value, at.UnixNano())
	if err != nil {
		return fmt.Errorf("Error saving vote: %v.", err)
	}
	_, err = tx.Exec(`UPDATE articles SET vote = (SELECT coalesce(sum(value), 0) FROM votes WHERE article_id = ?) WHERE id = ?`,
		articleID, articleID)
	if err != nil {
		return fmt.Errorf("Error updating vote total: %v.", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("Error committing vote: %v.", err)
	}
	return nil
}

// UserVotes returns every vote of one user, oldest first.
func UserVotes(userID string) ([]data.Vote, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT article_id, user_id, value, ts FROM votes WHERE user_id = ? ORDER BY ts`, userID)
	if err != nil {
		return nil, fmt.Errorf("Error querying votes: %v.", err)
	}
	defer rows.Close()
	var votes []data.Vote
	for rows.Next() {
		var v data.Vote
		var ts int64
		if err := rows.Scan(&v.ArticleID, &v.UserID, &v.Value, &ts); err != nil {
			return nil, fmt.Errorf("Error reading vote: %v.", err)
		}
		v.Timestamp = time.Unix(0, ts)
		votes = append(votes, v)
	}
	return votes, rows.Err()
}

// Reset deletes every article and vote.
func Reset() error {
	db, err := DB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM votes; DELETE FROM articles; INSERT INTO articles_fts(articles_fts) VALUES('delete-all');`)
	if err != nil {
		return fmt.Errorf("Error resetting database: %v.", err)
	}
	return nil
}

// Backup writes a compacted, consistent copy of the database to dest.
func Backup(dest string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("Error backing up database: %v.", err)
	}
	return nil
}

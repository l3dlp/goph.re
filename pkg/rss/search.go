package rss

import (
	"gophre/cmd/data"
	"strings"
)

func Search(query string, articles []data.Article, page, size int) []data.Article {
	var results []data.Article

	// Convert the query to lowercase for case-insensitive search
	query = strings.ToLower(query)

	// First collect all matching articles
	for _, article := range articles {
		if strings.Contains(strings.ToLower(article.Name), query) || strings.Contains(strings.ToLower(article.Resume), query) {
			results = append(results, article)
		}
	}

	// Then paginate the results
	startIdx := (page - 1) * size
	endIdx := startIdx + size

	// Check bounds
	if startIdx >= len(results) {
		return []data.Article{}
	}
	if endIdx > len(results) {
		endIdx = len(results)
	}

	return results[startIdx:endIdx]
}

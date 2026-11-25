package web

import (
	"crypto/rand"
	"encoding/json"
	"gophre/env"
	"gophre/pkg/store"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/markbates/goth/gothic"
	"golang.org/x/time/rate"
)

// Gin web framework setuo
// Custom functions
// Rate limiter
// Routes

type RSSFeed struct {
	Path string `json:"path"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

func Serve(port ...int) {

	// Server Port
	goodPort := env.PORT
	if len(port) > 0 {
		goodPort = port[0]
	}

	// Define custom functions
	customFuncs := template.FuncMap{
		"safe": func(s string) template.HTML {
			return template.HTML(s)
		},
		"tojson": func(v interface{}) template.JS {
			a, _ := json.Marshal(v)
			return template.JS(a) // on injecte direct, donc pas d'échappement HTML
		},
	}

	// Rate limiter
	limiter := NewLimiter(rate.Every(1*time.Second), 500) // Adjust these values as needed

	// Gin setup
	r := gin.Default()
	// Trust the proxy
	r.SetTrustedProxies([]string{"169.155.237.69"})
	for name, fn := range pulseFuncs {
		customFuncs[name] = fn
	}
	for name, fn := range meFuncs {
		customFuncs[name] = fn
	}
	r.SetFuncMap(customFuncs)
	r.Use(RateLimiterMiddleware(limiter))

	// Set up sessions, keyed by SESSION_KEY from the environment. Without a
	// configured key, fall back to a random one: the server still works, but
	// sessions are invalidated on every restart.
	sessionKey := []byte(env.SESSION_KEY)
	if len(sessionKey) == 0 {
		sessionKey = make([]byte, 32)
		if _, err := rand.Read(sessionKey); err != nil {
			log.Printf("Error generating random session key: %v\n", err)
			return
		}
		log.Println("SESSION_KEY is not set; using a random key, sessions will not survive restarts.")
	}
	sessionStore := cookie.NewStore(sessionKey)
	r.Use(sessions.Sessions("gophre-session", sessionStore))

	// Configure Goth
	gothic.Store = sessionStore

	// Setup auth routes and middleware
	SetupAuth(r)

	// Admin routes
	adminGroup := r.Group("/admin")
	adminGroup.Use(RequireAuth())  // Apply RequireAuth middleware
	adminGroup.Use(RequireAdmin()) // Apply RequireAdmin middleware (placeholder)
	{
		adminGroup.GET("/rss", GetRssFeedsEditorHandler)
		adminGroup.POST("/rss", PostRssFeedsEditorHandler)
	}

	r.LoadHTMLGlob(env.PATH + "assets/html/*")
	SetupPWA(r)
	r.Static("/css", env.PATH+"assets/css")
	r.Static("/gfx", env.PATH+"assets/gfx")
	r.Static("/js", env.PATH+"assets/js")

	// Website
	r.GET("/", func(c *gin.Context) { wallPage(c, nil) })

	r.GET("/all", func(c *gin.Context) { wallPage(c, gin.H{"all": true}) })

	r.GET("/me", UserBookmarks)

	r.GET("/top", func(c *gin.Context) { wallPage(c, gin.H{"top": true}) })

	r.GET("/pulse", PulsePage)
	r.GET("/picks", PicksIndex)
	r.GET("/picks/feed.xml", PicksFeed)
	r.GET("/picks/:slug", PickArticle)

	// API POST
	r.POST("/vote/:id/:vote", Vote)

	// API GET
	r.GET("/article/:id", Article)
	r.GET("/feed", Feed)
	r.GET("/search", func(c *gin.Context) {
		page, size := pageParams(c)
		results, err := store.Search(c.Query("q"), (page-1)*size, size)
		if err != nil {
			log.Printf("Error searching articles: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Search failed."})
			return
		}
		c.JSON(http.StatusOK, results)
	})

	// Check all articles from one topic
	r.GET("/:path", Topic)

	// Start the server on IPv4 only.
	listener, err := net.Listen("tcp4", ":"+strconv.Itoa(goodPort))
	if err != nil {
		log.Printf("Error listening on port %d: %v\n", goodPort, err)
		return
	}
	r.RunListener(listener)

}

// pageParams reads the 1-based page and its size (1 to 100, default 30) from the query.
func pageParams(c *gin.Context) (page, size int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	size, err = strconv.Atoi(c.DefaultQuery("size", "30"))
	if err != nil || size < 1 || size > 100 {
		size = 30
	}
	return page, size
}

// It returns the article by ID
func Article(c *gin.Context) {
	article, _, err := store.Get(c.Param("id"))
	if err != nil {
		log.Printf("Error reading article: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to read article."})
		return
	}
	c.JSON(http.StatusOK, article)
}

// It takes a path, page and number of elements per page (size), and returns a JSON response with the RSS feed
func Feed(c *gin.Context) {
	page, size := pageParams(c)
	articles, err := store.Recent(c.Query("path"), (page-1)*size, size)
	if err != nil {
		log.Printf("Error reading feed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to read feed."})
		return
	}
	c.JSON(http.StatusOK, articles)
}

// It takes the path parameter from the URL, and passes it to the Topic function in the rss package.
// The Topic function returns a slice of articles, which is then passed to the search.html template
func Topic(c *gin.Context) {
	wallPage(c, gin.H{"path": c.Param("path")})
}

// GetRssFeedsEditorHandler handles GET requests to /admin/rss
func GetRssFeedsEditorHandler(c *gin.Context) {
	filePath := env.PATH + "rss/rss_feeds.json"
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error reading RSS feeds file")
		return
	}

	var feeds []RSSFeed
	json.Unmarshal(fileData, &feeds)

	user, _ := GetCurrentUser(c)
	c.HTML(http.StatusOK, "admin-rss-editor.html", gin.H{
		"FeedsContent": feeds,
		"user":         user,
	})
}

// PostRssFeedsEditorHandler handles POST requests to /admin/rss
func PostRssFeedsEditorHandler(c *gin.Context) {
	feedsContent := c.PostForm("feeds_content")
	filePath := env.PATH + "rss/rss_feeds.json"

	err := os.WriteFile(filePath, []byte(feedsContent), 0644)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error saving RSS feeds file")
		return
	}

	c.Redirect(http.StatusFound, "/admin/rss")
}

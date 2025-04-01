package db

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"

	"github.com/brettboylen/reddit-tracker/models"
)

// Database provides methods for storing and retrieving Reddit posts
type Database struct {
	db    *sql.DB
	mutex sync.RWMutex
	log   *logrus.Logger
}

// NewDatabase creates a new SQLite database connection
func NewDatabase(dbPath string, log *logrus.Logger) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &Database{
		db:  db,
		log: log,
	}

	if err := database.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	return database, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.db.Close()
}

// initTables creates the necessary tables if they don't exist
func (d *Database) initTables() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	// note: in an ideal world, this would be a migration that we could just run once per environment (ie dev, staging, prod)
	query := `
	CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		author TEXT NOT NULL,
		subreddit TEXT NOT NULL,
		url TEXT,
		created_utc REAL NOT NULL,
		created_at TIMESTAMP NOT NULL,
		upvotes INTEGER NOT NULL,
		downvotes INTEGER NOT NULL,
		score INTEGER NOT NULL,
		num_comments INTEGER NOT NULL,
		post_hint TEXT,
		is_video BOOLEAN NOT NULL,
		is_self BOOLEAN NOT NULL,
		self_text TEXT,
		permalink TEXT NOT NULL,
		processed_time TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_posts_upvotes ON posts(upvotes DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);
	`

	_, err := d.db.Exec(query)
	return err
}

// SavePost saves a post to the database
func (d *Database) SavePost(post *models.Post) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	query := `
	INSERT OR REPLACE INTO posts (
		id, title, author, subreddit, url, created_utc, created_at,
		upvotes, downvotes, score, num_comments, post_hint,
		is_video, is_self, self_text, permalink, processed_time
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(
		query,
		post.ID, post.Title, post.Author, post.Subreddit, post.URL,
		post.CreatedUTC, post.CreatedAt, post.Upvotes, post.Downvotes,
		post.Score, post.NumComments, post.PostHint, post.IsVideo,
		post.IsSelf, post.SelfText, post.Permalink, post.ProcessedTime,
	)

	if err != nil {
		return fmt.Errorf("failed to save post: %w", err)
	}

	return nil
}

// GetTopPostsByUpvotes returns the top N posts by upvotes
func (d *Database) GetTopPostsByUpvotes(limit int) ([]models.Post, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	query := `
	SELECT id, title, author, subreddit, url, created_utc, created_at,
		upvotes, downvotes, score, num_comments, post_hint,
		is_video, is_self, self_text, permalink, processed_time
	FROM posts
	ORDER BY upvotes DESC
	LIMIT ?
	`

	rows, err := d.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top posts: %w", err)
	}
	defer rows.Close()

	posts := make([]models.Post, 0, limit)
	for rows.Next() {
		var post models.Post
		var createdAt string
		var processedTime string

		err := rows.Scan(
			&post.ID, &post.Title, &post.Author, &post.Subreddit, &post.URL,
			&post.CreatedUTC, &createdAt, &post.Upvotes, &post.Downvotes,
			&post.Score, &post.NumComments, &post.PostHint, &post.IsVideo,
			&post.IsSelf, &post.SelfText, &post.Permalink, &processedTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan post: %w", err)
		}

		post.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		post.ProcessedTime, _ = time.Parse(time.RFC3339, processedTime)
		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return posts, nil
}

// GetTopUsersByPostCount returns the top N users by post count
func (d *Database) GetTopUsersByPostCount(limit int) (map[string]int, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	query := `
	SELECT author, COUNT(*) as post_count
	FROM posts
	GROUP BY author
	ORDER BY post_count DESC
	LIMIT ?
	`

	rows, err := d.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top users: %w", err)
	}
	defer rows.Close()

	users := make(map[string]int)
	for rows.Next() {
		var author string
		var count int
		
		if err := rows.Scan(&author, &count); err != nil {
			return nil, fmt.Errorf("failed to scan user post count: %w", err)
		}
		
		users[author] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return users, nil
}

// GetTotalPosts returns the total number of posts in the database
func (d *Database) GetTotalPosts() (int, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total posts: %w", err)
	}

	return count, nil
}

// GetPostsBySubreddit returns posts from a specific subreddit
func (d *Database) GetPostsBySubreddit(subreddit string) ([]models.Post, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	query := `
	SELECT id, title, author, subreddit, url, created_utc, created_at,
		upvotes, downvotes, score, num_comments, post_hint,
		is_video, is_self, self_text, permalink, processed_time
	FROM posts
	WHERE subreddit = ?
	ORDER BY upvotes DESC
	`

	rows, err := d.db.Query(query, subreddit)
	if err != nil {
		return nil, fmt.Errorf("failed to query posts for subreddit %s: %w", subreddit, err)
	}
	defer rows.Close()

	posts := make([]models.Post, 0)
	for rows.Next() {
		var post models.Post
		var createdAt string
		var processedTime string

		err := rows.Scan(
			&post.ID, &post.Title, &post.Author, &post.Subreddit, &post.URL,
			&post.CreatedUTC, &createdAt, &post.Upvotes, &post.Downvotes,
			&post.Score, &post.NumComments, &post.PostHint, &post.IsVideo,
			&post.IsSelf, &post.SelfText, &post.Permalink, &processedTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan post: %w", err)
		}

		post.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		post.ProcessedTime, _ = time.Parse(time.RFC3339, processedTime)
		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return posts, nil
} 
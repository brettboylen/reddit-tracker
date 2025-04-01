package models

import (
	"time"
)

// Post represents a Reddit post
type Post struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Author        string    `json:"author"`
	Subreddit     string    `json:"subreddit"`
	URL           string    `json:"url"`
	CreatedUTC    float64   `json:"created_utc"`
	CreatedAt     time.Time `json:"created_at"`
	Upvotes       int       `json:"upvotes"`
	Downvotes     int       `json:"downvotes"`
	Score         int       `json:"score"`
	NumComments   int       `json:"num_comments"`
	PostHint      string    `json:"post_hint"`
	IsVideo       bool      `json:"is_video"`
	IsSelf        bool      `json:"is_self"`
	SelfText      string    `json:"selftext"`
	Permalink     string    `json:"permalink"`
	ProcessedTime time.Time `json:"processed_time"`
}

// SubredditStats holds statistics for a single subreddit
type SubredditStats struct {
	PostCount         int  `json:"post_count"`
	HighestUpvotedPost Post `json:"highest_upvoted_post"`
}

// Statistics holds statistics about the Reddit posts
type Statistics struct {
	TotalPosts         int                       `json:"total_posts"`
	ProcessedPostCount int                       `json:"processed_post_count"`
	TopPostsByUpvotes  []Post                    `json:"top_posts_by_upvotes"`
	TopUsersByPostCount map[string]int           `json:"top_users_by_post_count"`
	StartTime          time.Time                 `json:"start_time"`
	LastUpdated        time.Time                 `json:"last_updated"`
	SubredditStats     map[string]SubredditStats `json:"subreddit_stats"`
} 
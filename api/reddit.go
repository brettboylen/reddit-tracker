package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/brettboylen/reddit-tracker/models"
)

const (
	baseURL      = "https://oauth.reddit.com"
	authURL      = "https://www.reddit.com/api/v1/access_token"
	defaultLimit = 100 // max number of posts per request
)

// TokenBucket implements a rate limiter using the token bucket algorithm
type TokenBucket struct {
	mutex       sync.Mutex
	capacity    int           // maximum tokens the bucket can hold
	tokens      float64       // current number of tokens
	fillRate    float64       // rate at which tokens are added (tokens per second)
	lastRefill  time.Time     // time of last token refill
	waitTimeout time.Duration // max time to wait for a token
}

// NewTokenBucket creates a new token bucket rate limiter
func NewTokenBucket(capacity int, fillRate float64, waitTimeout time.Duration) *TokenBucket {
	return &TokenBucket{
		capacity:    capacity,
		tokens:      1, // lets start with just 1 token to avoid initial burst
		fillRate:    fillRate,
		lastRefill:  time.Now(),
		waitTimeout: waitTimeout,
	}
}

// Take attempts to take a token from the bucket
// Returns true if successful, false if timed out
func (tb *TokenBucket) Take() bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now

	// Add tokens based on elapsed time and fill rate
	newTokens := elapsed * tb.fillRate
	if newTokens > 0 {
		tb.tokens = tb.tokens + newTokens
		if tb.tokens > float64(tb.capacity) {
			tb.tokens = float64(tb.capacity)
		}
	}

	// If we have at least one token, take it and return true
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}

	// No tokens available
	return false
}

// TakeWithTimeout attempts to take a token from the bucket, waiting up to waitTimeout
func (tb *TokenBucket) TakeWithTimeout() bool {
	if tb.Take() {
		return true
	}

	// calculate the time to wait for the next token
	tb.mutex.Lock()
	tokensNeeded := 1 - tb.tokens
	timeToWait := time.Duration(tokensNeeded / tb.fillRate * float64(time.Second))
	if timeToWait > tb.waitTimeout {
		timeToWait = tb.waitTimeout
	}
	tb.mutex.Unlock()

	// wait for next token and then grab it
	time.Sleep(timeToWait)
	return tb.Take()
}

// Update updates the rate limiter parameters based on Reddit API headers
func (tb *TokenBucket) Update(used int, reset int, maxRequests int) {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	// Reddit allocates 1000 requests per rolling 10-minute period (600 seconds)
	// reset_sec counts down from ~600 to 0
	// remaining is broken/bugged (always 0)
	// used counts up from 0 to 1000
	
	// use the full allocation period and total requests for calculation
	totalAllocationPeriod := 600
	totalAllocation := 1000
	
	// calculate the rate based on the entire allocation
	// lets use 95% of the full rate for safety buffer
	fullRate := float64(totalAllocation) / float64(totalAllocationPeriod)
	targetRate := fullRate * 0.95
	
	// set fill rate based on allocation
	tb.fillRate = targetRate
}

// RedditAPI represents a Reddit API client
type RedditAPI struct {
	clientID           string
	clientSecret       string
	userAgent          string
	httpClient         *http.Client
	accessToken        string
	tokenExpiry        time.Time
	mutex              sync.RWMutex
	log                *logrus.Logger
	rateLimiter        *TokenBucket
	maxRequestsPerMin  int
	rateRemainingCached int
	rateResetCached    int
	rateUsedCached     int
	rateHeadersMutex   sync.RWMutex
}

// RedditPost represents the Reddit API response structure for a post
type RedditPost struct {
	Kind string `json:"kind"`
	Data struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Title       string  `json:"title"`
		Author      string  `json:"author"`
		Subreddit   string  `json:"subreddit"`
		URL         string  `json:"url"`
		CreatedUTC  float64 `json:"created_utc"`
		Ups         int     `json:"ups"`
		Downs       int     `json:"downs"`
		Score       int     `json:"score"`
		NumComments int     `json:"num_comments"`
		PostHint    string  `json:"post_hint"`
		IsVideo     bool    `json:"is_video"`
		IsSelf      bool    `json:"is_self"`
		SelfText    string  `json:"selftext"`
		Permalink   string  `json:"permalink"`
	} `json:"data"`
}

// RedditResponse represents the Reddit API response structure
type RedditResponse struct {
	Kind string `json:"kind"`
	Data struct {
		After    string       `json:"after"`
		Before   string       `json:"before"`
		Children []RedditPost `json:"children"`
	} `json:"data"`
}

// NewRedditAPI creates a new Reddit API client
func NewRedditAPI(clientID, clientSecret, userAgent string, maxRequestsPerMinute int, log *logrus.Logger) *RedditAPI {
	// default to 100 requests per minute (real Reddit limit)
	if maxRequestsPerMinute <= 0 {
		maxRequestsPerMinute = 100
	}
	
	// our 10 minute allocation
	totalAllocation := maxRequestsPerMinute * 10
	
	standardRate := float64(totalAllocation) / 600.0 
	targetRate := standardRate * 0.95 
	
	// Create a token bucket rate limiter:
	// - capacity: 1 (no burst capacity when set to 1)
	// - fillRate: 95% of Reddit's rate (1000 requests per 600 seconds)
	// - waitTimeout: max 30 seconds wait for a token
	rateLimiter := NewTokenBucket(
		1,   // no burst
		targetRate,
		30 * time.Second,
	)
	
	return &RedditAPI{
		clientID:           clientID,
		clientSecret:       clientSecret,
		userAgent:          userAgent,
		httpClient:         &http.Client{Timeout: 30 * time.Second},
		log:                log,
		rateLimiter:        rateLimiter,
		maxRequestsPerMin:  maxRequestsPerMinute,
		rateRemainingCached: 0,
		rateResetCached:    600,
		rateUsedCached:     0,
	}
}

// GetRateLimitStatus returns the current rate limit status (remaining requests, reset time in seconds, and used requests)
func (r *RedditAPI) GetRateLimitStatus() (int, int, int) {
	r.rateHeadersMutex.RLock()
	defer r.rateHeadersMutex.RUnlock()
	return r.rateRemainingCached, r.rateResetCached, r.rateUsedCached
}

// authenticate authenticates with the Reddit API
func (r *RedditAPI) authenticate() error {
	// first check if we already have a valid token without holding the lock for long
	r.mutex.RLock()
	token := r.accessToken
	expiry := r.tokenExpiry
	r.mutex.RUnlock()
	
	if token != "" && time.Now().Before(expiry) {
		return nil
	}

	r.log.Info("Authenticating with Reddit API")

	// wait for rate limiting
	if !r.rateLimiter.TakeWithTimeout() {
		return fmt.Errorf("rate limit exceeded during authentication attempt")
	}

	data := url.Values{}
	
	r.log.Debug("Using application-only auth with client credentials")
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", authURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.SetBasicAuth(r.clientID, r.clientSecret)
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute auth request: %w", err)
	}
	defer resp.Body.Close()

	// note: see the TODO in updateRateLimits
	r.updateRateLimits(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	r.mutex.Lock()
	r.accessToken = authResp.AccessToken
	r.tokenExpiry = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	r.mutex.Unlock()

	r.log.Info("Successfully authenticated with Reddit API")
	return nil
}

// FetchPosts fetches posts from a subreddit
func (r *RedditAPI) FetchPosts(subreddit string, limit int, after string) ([]models.Post, string, error) {
	if err := r.authenticate(); err != nil {
		return nil, "", err
	}

	if limit <= 0 || limit > 100 {
		limit = defaultLimit
	}

	if !r.rateLimiter.TakeWithTimeout() {
		r.log.Warn("Rate limit exceeded, waiting before retrying")
		// wait 1 second and retry recursively!! :)
		// TODO: we could use exponential backoff here, but not going to worry about it for now
		time.Sleep(time.Second)
		return r.FetchPosts(subreddit, limit, after)
	}

	endpoint := fmt.Sprintf("%s/r/%s/new.json?limit=%d", baseURL, subreddit, limit)
	if after != "" {
		endpoint += "&after=" + after
	}

	r.log.WithFields(logrus.Fields{
		"subreddit": subreddit,
		"after": after,
		"limit": limit,
	}).Info("Fetching posts from Reddit API with pagination token")

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	r.mutex.RLock()
	token := r.accessToken
	r.mutex.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", r.userAgent)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// note: see the TODO in updateRateLimits
	r.updateRateLimits(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		r.log.WithFields(logrus.Fields{
			"subreddit":     subreddit,
			"response_body": string(body),
			"status_code":   resp.StatusCode,
		}).Error("Reddit API error response")
		return nil, "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var redditResp RedditResponse
	if err := json.NewDecoder(resp.Body).Decode(&redditResp); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	posts := make([]models.Post, 0, len(redditResp.Data.Children))
	now := time.Now()

	for _, redditPost := range redditResp.Data.Children {
		post := models.Post{
			ID:            redditPost.Data.ID,
			Title:         redditPost.Data.Title,
			Author:        redditPost.Data.Author,
			Subreddit:     redditPost.Data.Subreddit,
			URL:           redditPost.Data.URL,
			CreatedUTC:    redditPost.Data.CreatedUTC,
			CreatedAt:     time.Unix(int64(redditPost.Data.CreatedUTC), 0),
			Upvotes:       redditPost.Data.Ups,
			Downvotes:     redditPost.Data.Downs,
			Score:         redditPost.Data.Score,
			NumComments:   redditPost.Data.NumComments,
			PostHint:      redditPost.Data.PostHint,
			IsVideo:       redditPost.Data.IsVideo,
			IsSelf:        redditPost.Data.IsSelf,
			SelfText:      redditPost.Data.SelfText,
			Permalink:     redditPost.Data.Permalink,
			ProcessedTime: now,
		}
		posts = append(posts, post)
	}

	r.log.WithFields(logrus.Fields{
		"post_count": len(posts),
		"subreddit":  subreddit,
		"after":      after,
		"next_after": redditResp.Data.After,
		"pagination_changed": after != redditResp.Data.After && redditResp.Data.After != "",
	}).Info("Fetched posts from Reddit with pagination info")

	return posts, redditResp.Data.After, nil
}

// updateRateLimits updates the rate limiter based on response headers
// TODO: this isn't actually adapting based off of the header responses;  this is simply used for debuggng atm
func (r *RedditAPI) updateRateLimits(resp *http.Response) {
	// X-Ratelimit-Used: Approximate number of requests used in this period
	// X-Ratelimit-Remaining: Approximate number of requests left to use (bugged - always 0)
	// X-Ratelimit-Reset: Approximate number of seconds to end of period (counts down from ~600 seconds)
	used := getHeaderAsInt(resp.Header, "X-Ratelimit-Used")
	remaining := getHeaderAsInt(resp.Header, "X-Ratelimit-Remaining") // always 0, appears bugged
	reset := getHeaderAsInt(resp.Header, "X-Ratelimit-Reset")
	
	// skip if we didn't get valid headers for some reason
	if reset == 0 && used == 0 {
		return
	}
	
	// reddit allocates 1000 requests per 600 seconds (10 minutes); this indicates the total allocation of 1k
	totalAllocation := 1000.0

	r.rateHeadersMutex.Lock()
	r.rateRemainingCached = remaining // bugged - always 0; update anyways in case reddit fixes it
	r.rateResetCached = reset
	r.rateUsedCached = used
	r.rateHeadersMutex.Unlock()
	
	r.rateLimiter.Update(used, reset, r.maxRequestsPerMin)

	r.log.WithFields(logrus.Fields{
		"used":               used,
		"reset_sec":          reset,
		"new_fill_rate":      r.rateLimiter.fillRate,
		"usage_pct":          float64(used) / totalAllocation * 100,
	}).Debug("Updated rate limiter based on Reddit headers")
}

func getHeaderAsInt(header http.Header, name string) int {
	value := header.Get(name)
	if value == "" {
		return 0
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}

	return intValue
} 
package stats

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/brettboylen/reddit-tracker/api"
	"github.com/brettboylen/reddit-tracker/db"
	"github.com/brettboylen/reddit-tracker/models"
)

const (
	defaultTopPostsLimit = 10
	defaultTopUsersLimit = 10
)

// Collector collects and analyzes Reddit posts
type Collector struct {
	redditAPI          *api.RedditAPI
	database           *db.Database
	subreddits         []string
	paginationKeys     map[string]string
	pollingInterval    time.Duration
	topPostsLimit      int
	topUsersLimit      int
	stats              models.Statistics
	log                *logrus.Logger
	mutex              sync.RWMutex
	processedPostCount int
}

// NewCollector creates a new collector
func NewCollector(
	redditAPI *api.RedditAPI,
	database *db.Database,
	subreddits []string,
	pollingInterval int,
	log *logrus.Logger,
) *Collector {
	return &Collector{
		redditAPI:       redditAPI,
		database:        database,
		subreddits:      subreddits,
		paginationKeys:  make(map[string]string),
		pollingInterval: time.Duration(pollingInterval) * time.Second,
		topPostsLimit:   defaultTopPostsLimit,
		topUsersLimit:   defaultTopUsersLimit,
		stats: models.Statistics{
			TopPostsByUpvotes:   make([]models.Post, 0, defaultTopPostsLimit),
			TopUsersByPostCount: make(map[string]int),
			StartTime:           time.Now(),
			LastUpdated:         time.Now(),
			SubredditStats:      make(map[string]models.SubredditStats),
		},
		log: log,
	}
}

// Start func starts collecting posts from Reddit
func (c *Collector) Start(ctx context.Context) error {
	ticker := time.NewTicker(c.pollingInterval)
	defer ticker.Stop()

	if err := c.fetchAndProcessPosts(ctx); err != nil {
		c.log.WithError(err).Error("Failed to fetch and process posts")
	}

	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()
	
	resetInterval := 5 * time.Minute
	c.log.WithFields(logrus.Fields{
		"reset_interval_minutes": resetInterval.Minutes(),
	}).Info("Pagination reset interval configured")
	
	// reset paginatin key every 5 minutes to check for new posts
	resetTicker := time.NewTicker(resetInterval)
	defer resetTicker.Stop()

	// channel to receive signals to adjust the polling interval
	adjustTicker := make(chan time.Duration, 1)
	
	// adjust the polling interval based on rate limits, if needed
	go func() {
		adjustmentTicker := time.NewTicker(10 * time.Second)
		defer adjustmentTicker.Stop()
		
		// track the last reset value to detect transitions between rate limit periods
		lastResetValue := 600
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-adjustmentTicker.C:
				_, reset, used := c.redditAPI.GetRateLimitStatus()
				
				// attempt to detect if we've crossed into a new reset period
				// (when reset suddenly jumps back up from a low number)
				if reset > lastResetValue && lastResetValue < 100 {
					c.log.Info("Detected new rate limit period, values have reset")
				}
				lastResetValue = reset
				
				var newInterval time.Duration
				numSubreddits := len(c.subreddits)
				
				// Reddit allocates 1000 requests in a 600-second period
				totalAllowedRequests := 1000
				totalPeriodSeconds := 600
				
				// lets aim for 95% utilization of the rate limit
				standardRate := (float64(totalAllowedRequests) / float64(totalPeriodSeconds)) * 0.95
				
				// for multiple subreddits, divide the rate among them
				if numSubreddits > 1 {
					standardRate = standardRate / float64(numSubreddits)
				}
				
				secondsPerRequest := 1.0 / standardRate
				newInterval = time.Duration(secondsPerRequest * float64(time.Second))
				
				if c.pollingInterval == 0 || 
				   math.Abs(float64(newInterval-c.pollingInterval)) > float64(c.pollingInterval/4) {
					logFields := logrus.Fields{
						"old_interval_sec":     c.pollingInterval.Seconds(),
						"new_interval_sec":     newInterval.Seconds(),
						"reset_countdown_sec":  reset,
						"used_requests":        used,
						"total_allocation":     totalAllowedRequests,
						"period_seconds":       totalPeriodSeconds,
						"strategy":             "reddit_rate",
						"target_req_per_sec":   standardRate,
						"target_req_per_min":   standardRate * 60,
						"utilization_target":   "95%",
						"subreddit_count":      numSubreddits,
					}
					
					c.log.WithFields(logFields).Info("Using Reddit API rate limit")
					adjustTicker <- newInterval
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.fetchAndProcessPosts(ctx); err != nil {
				c.log.WithError(err).Error("Failed to fetch and process posts")
			}
		case <-statsTicker.C:
			c.updateStatistics()
			c.logStatistics()
		case newInterval := <-adjustTicker:
			ticker.Stop()
			c.pollingInterval = newInterval
			ticker = time.NewTicker(newInterval)
		case <-resetTicker.C:
			// reset pagination keys to check for new posts
			c.resetPaginationKeys()
		}
	}
}

// fetchAndProcessPosts fetches posts from Reddit and processes them
func (c *Collector) fetchAndProcessPosts(ctx context.Context) error {
	c.log.WithField("subreddits", c.subreddits).Info("Fetching posts from all subreddits")

	fetchCtx, cancel := context.WithTimeout(ctx, c.pollingInterval/2)
	defer cancel()

	var wg sync.WaitGroup
	errorsCh := make(chan error, len(c.subreddits))

	// process each subreddit concurrently; support for more than 1 subreddit
	for _, subreddit := range c.subreddits {
		wg.Add(1)
		go func(sr string) {
			defer wg.Done()
			
			c.log.WithField("subreddit", sr).Debug("Starting to fetch posts for subreddit")

			c.mutex.RLock()
			paginationKey := c.paginationKeys[sr]
			c.mutex.RUnlock()
			
			c.log.WithFields(logrus.Fields{
				"subreddit": sr,
				"pagination_key": paginationKey,
			}).Info("Using pagination key for fetch")
			
			posts, nextPaginationKey, err := c.redditAPI.FetchPosts(sr, 100, paginationKey)
			if err != nil {
				errorsCh <- fmt.Errorf("failed to fetch posts from %s: %w", sr, err)
				return
			}

			c.log.WithFields(logrus.Fields{
				"subreddit": sr,
				"count":     len(posts),
				"old_pagination_key": paginationKey,
				"new_pagination_key": nextPaginationKey,
				"pagination_changed": paginationKey != nextPaginationKey,
			}).Info("Fetched posts for subreddit with pagination update")

			c.mutex.Lock()
			c.paginationKeys[sr] = nextPaginationKey
			c.mutex.Unlock()

			if err := c.processPosts(fetchCtx, posts); err != nil {
				errorsCh <- fmt.Errorf("failed to process posts from %s: %w", sr, err)
			}
		}(subreddit)
	}

	wg.Wait()
	close(errorsCh)

	for err := range errorsCh {
		c.log.WithError(err).Error("Error while fetching and processing posts")
	}

	return nil
}

// processPosts processes a batch of posts
// TODO: ctx is not used at current;  remove it later.
func (c *Collector) processPosts(ctx context.Context, posts []models.Post) error {
	if len(posts) == 0 {
		c.log.Info("No new posts to process")
		return nil
	}

	c.log.WithField("count", len(posts)).Info("Processing posts")

	var wg sync.WaitGroup
	errCh := make(chan error, len(posts))

	// process each post in a separate goroutine
	for _, post := range posts {
		wg.Add(1)
		go func(post models.Post) {
			defer wg.Done()
			if err := c.processPost(post); err != nil {
				errCh <- err
			}
		}(post)
	}

	wg.Wait()
	close(errCh)

	// log any errors from the processing of posts
	for err := range errCh {
		c.log.WithError(err).Error("Error processing post")
	}

	c.updateStatistics()

	return nil
}

// processPost processes a single post
func (c *Collector) processPost(post models.Post) error {
	if err := c.database.SavePost(&post); err != nil {
		return fmt.Errorf("failed to save post: %w", err)
	}

	c.mutex.Lock()
	c.processedPostCount++
	c.mutex.Unlock()

	return nil
}

// updateStatistics updates the in-memory statistics
func (c *Collector) updateStatistics() {
	// top posts by upvotes from database
	topPosts, err := c.database.GetTopPostsByUpvotes(c.topPostsLimit)
	if err != nil {
		c.log.WithError(err).Error("Failed to get top posts")
		return
	}

	// top users by post count from database
	topUsers, err := c.database.GetTopUsersByPostCount(c.topUsersLimit)
	if err != nil {
		c.log.WithError(err).Error("Failed to get top users")
		return
	}

	// total posts from db
	totalPosts, err := c.database.GetTotalPosts()
	if err != nil {
		c.log.WithError(err).Error("Failed to get total posts")
		return
	}

	// get the stats per subreddit
	subredditStats := make(map[string]models.SubredditStats)
	for _, subreddit := range c.subreddits {
		posts, err := c.database.GetPostsBySubreddit(subreddit)
		if err != nil {
			c.log.WithError(err).WithField("subreddit", subreddit).Error("Failed to get posts for subreddit")
			continue
		}
		
		if len(posts) == 0 {
			continue
		}
		
		stats := models.SubredditStats{
			PostCount: len(posts),
		}
		
		if len(posts) > 0 {
			highestUpvoted := posts[0]
			for _, post := range posts {
				if post.Upvotes > highestUpvoted.Upvotes {
					highestUpvoted = post
				}
			}
			stats.HighestUpvotedPost = highestUpvoted
		}
		
		subredditStats[subreddit] = stats
	}

	c.mutex.Lock()
	c.stats.TopPostsByUpvotes = topPosts
	c.stats.TopUsersByPostCount = topUsers
	c.stats.TotalPosts = totalPosts
	c.stats.SubredditStats = subredditStats
	c.stats.LastUpdated = time.Now()
	c.mutex.Unlock()
}

// logStatistics logs the current statistics
func (c *Collector) logStatistics() {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	subredditsWithData := 0
	for _, sr := range c.subreddits {
		if _, exists := c.stats.SubredditStats[sr]; exists {
			subredditsWithData++
		}
	}

	c.log.WithFields(logrus.Fields{
		"total_posts":          c.stats.TotalPosts,
		"processed_in_run":     c.processedPostCount,
		"subreddit_count":      len(c.subreddits),
		"subreddits_with_data": subredditsWithData,
		"running_since":        time.Since(c.stats.StartTime).String(),
	}).Info("Statistics updated")
}

// GetStatistics returns a copy of the current statistics
// note: we are using a mutex to lock the stats object so it can't be modified while we are reading it
func (c *Collector) GetStatistics() models.Statistics {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	return c.stats
}

// resetPaginationKeys resets all pagination keys to empty strings to fetch newest posts
func (c *Collector) resetPaginationKeys() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	oldKeys := make(map[string]string)
	for k, v := range c.paginationKeys {
		oldKeys[k] = v
	}
	
	// Clear all pagination keys to fetch newest posts
	for subreddit := range c.paginationKeys {
		c.paginationKeys[subreddit] = ""
	}
	
	c.log.WithFields(logrus.Fields{
		"old_keys": oldKeys,
		"new_keys": c.paginationKeys,
	}).Info("Reset pagination keys to check for newest posts")
} 
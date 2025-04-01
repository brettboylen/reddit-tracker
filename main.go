package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	"github.com/brettboylen/reddit-tracker/api"
	"github.com/brettboylen/reddit-tracker/db"
	"github.com/brettboylen/reddit-tracker/stats"
	"github.com/brettboylen/reddit-tracker/utils"
)

func main() {
	envPath := flag.String("env", ".env", "Path to .env file")
	logLevel := flag.String("log-level", "debug", "Logging level (debug, info, warn, error)")
	flag.Parse()

	log := setupLogger(*logLevel)
	log.Info("Starting Reddit Tracker")

	config, err := utils.LoadConfig(*envPath, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	log.WithFields(logrus.Fields{
		"subreddits":       config.Reddit.Subreddits,
		"polling_interval": config.Reddit.PollingInterval,
		"server_port":      config.Server.Port,
	}).Info("Configuration loaded")

	database, err := db.NewDatabase(config.Database.Path, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}
	defer database.Close()

	redditAPI := api.NewRedditAPI(
		config.Reddit.ClientID,
		config.Reddit.ClientSecret,
		config.Reddit.UserAgent,
		config.Reddit.MaxRequestsPerMinute,
		log,
	)

	collector := stats.NewCollector(
		redditAPI,
		database,
		config.Reddit.Subreddits,
		config.Reddit.PollingInterval,
		log,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startEchoServer(ctx, config.Server.Port, collector, log, config.Reddit.MaxRequestsPerMinute)

	go func() {
		if err := collector.Start(ctx); err != nil && err != context.Canceled {
			log.WithError(err).Error("Stats collector stopped unexpectedly")
		}
	}()

	waitForShutdown(cancel, log)
}

// setupLogger sets up the logger with the specified log level
func setupLogger(level string) *logrus.Logger {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	return log
}

// startEchoServer starts the Echo HTTP API server
func startEchoServer(ctx context.Context, port int, collector *stats.Collector, log *logrus.Logger, maxRequestsPerMinute int) {
	e := echo.New()
	
	// middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	
	requestsPerSecond := float64(maxRequestsPerMinute) / 60.0
	
	rateLimit := rate.Limit(requestsPerSecond*0.95) // use 95% of the rate limit to be safe

	
	rateLimiterConfig := middleware.RateLimiterConfig{
		Skipper: middleware.DefaultSkipper,
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rateLimit,
				Burst:     1,      // no burst capability
				ExpiresIn: 3 * time.Minute,
			},
		),
		IdentifierExtractor: func(ctx echo.Context) (string, error) {
			return ctx.RealIP(), nil
		},
		ErrorHandler: func(ctx echo.Context, err error) error {
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{
				"error": "Rate limit exceeded, please try again later",
			})
		},
		DenyHandler: func(ctx echo.Context, identifier string, err error) error {
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{
				"error": "Rate limit exceeded, please try again later",
			})
		},
	}
	e.Use(middleware.RateLimiterWithConfig(rateLimiterConfig))
	
	e.GET("/api/stats", func(c echo.Context) error {
		stats := collector.GetStatistics()
		return c.JSON(http.StatusOK, stats)
	})
	
	e.GET("/api/stats/:subreddit", func(c echo.Context) error {
		subreddit := c.Param("subreddit")
		stats := collector.GetStatistics()
		
		// check if the subreddit exists in our stats
		subredditStats, exists := stats.SubredditStats[subreddit]
		if !exists {
			return c.JSON(http.StatusNotFound, map[string]string{
				"error": fmt.Sprintf("No statistics available for subreddit %s", subreddit),
			})
		}
		
		return c.JSON(http.StatusOK, subredditStats)
	})
	
	
	// health check endpoint; useful for k8s liveliness probes but not strictly required in this case;
	// should also add readiness probe, etc if we had a full k8s use case here
	e.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
	
	// start the server!
	go func() {
		serverAddr := fmt.Sprintf(":%d", port)
		log.WithField("port", port).Info("Starting API server")
		if err := e.Start(serverAddr); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("API server failed")
		}
	}()
	
	// wait for context cancellation to shut down server
	<-ctx.Done()
	log.Info("Shutting down API server")
	

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := e.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("API server shutdown failed")
	}
}

// waitForShutdown waits for a shutdown signal
func waitForShutdown(cancel context.CancelFunc, log *logrus.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.WithField("signal", sig.String()).Info("Shutdown signal received")

	cancel()

	time.Sleep(1 * time.Second)
	log.Info("Reddit Tracker stopped")
} 
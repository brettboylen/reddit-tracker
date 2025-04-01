# Reddit Tracker

A Go application that tracks Reddit posts in near real-time and provides statistics about the most upvoted posts and most active users. Built with the Echo web framework.

## Notes

This project demonstrates several implementation decisions worth highlighting:

- **Echo Framework**: I chose the Echo package for routing due to its performance and simplicity. Echo's built-in rate limiting middleware was beneficial here.

- **SQLite Database**: While not explicitly required, I implemented basic data persistence using SQLite. This allows the application to maintain statistics across restarts and provides a foundation for more sophisticated data analysis.

- **Multiple Subreddit Support**: The application technically supports tracking multiple subreddits via a comma-separated list in the `.env` file. This feature should be considered experimental as it hasn't been rigorously tested.

- **Kubernetes Ready**: I added a `/healthz` endpoint specifically for Kubernetes liveliness probe;  in practice, we'd likely need a readiness endpoint as well if we were pushing to k8s.

## Features

- Connects to Reddit API to fetch posts from multiple subreddits
- Respects Reddit's rate limiting using response headers
- Processes posts concurrently for better performance
- Stores posts in a SQLite database
- Provides statistics through both console output and a REST API
- Implements graceful shutdown
- Built with the Echo framework for fast and scalable API endpoints


## Requirements

- Go 1.16 or later
- Reddit API credentials (see below)

## Getting Started

### Reddit API Credentials

To use this application, you need to register an app on Reddit and obtain API credentials:

1. Go to https://www.reddit.com/prefs/apps
2. Sign in with your Reddit account
3. Scroll to the bottom and click "create app" or "create another app" button
4. Fill in the form:
   - Name: reddit-tracker (or any name you prefer)
   - App type: Select "script"
   - Description: (optional) A short description of what your app does
   - About URL: (optional) Your website or GitHub repository
   - Redirect URI: http://localhost:8080 (this isn't actually used but is required by Reddit)
5. Click "create app" button
6. After creation, note the following information:
   - Client ID: the string displayed under "personal use script" (a string like `ABC123XYZ456`)
   - Client Secret: the string labeled "secret" (a string like `ABC123xyz456DEFGhij789`)

### Configuration Setup

1. Copy the example configuration file to create your own (my `.env` not committed since it contains sensitive keys):

```bash
cp example.env .env
```

2. Edit the `.env` file with your editor of choice:

```bash
nano .env
# or
vim .env
# or
code .env
```

3. Update all the required fields in the `.env` file:

```
# Application configuration
APP_NAME=Reddit Tracker
APP_VERSION=1.0.0

# Reddit API credentials - replace with your own
REDDIT_CLIENT_ID=your_client_id_here
REDDIT_CLIENT_SECRET=your_client_secret_here

# IMPORTANT: User agent MUST be updated per Reddit's API requirements
# Format: <platform>:<app ID>:<version string> (by /u/<reddit username>)
# Example: script:reddit-tracker:v1.0.0 (by /u/your_actual_username)
REDDIT_USER_AGENT=script:reddit-tracker:v1.0.0 (by /u/your_actual_username)

# Comma-separated list of subreddits to track
REDDIT_SUBREDDITS=AskReddit

# Initial polling interval in seconds (how often to fetch posts)
# The application will adjust this based on Reddit's rate limits
REDDIT_POLLING_INTERVAL=2

# Maximum requests per minute to Reddit's API
# Reddit allows 1000 requests per 10 minutes (100 per minute)
REDDIT_MAX_REQUESTS_PER_MINUTE=100

# Database configuration
DATABASE_PATH=./reddit.db

# Server configuration
SERVER_PORT=8080

# Logging level (debug, info, warn, error)
LOG_LEVEL=info
```

#### Important Configuration Notes:

- **User Agent**: Reddit's API requires a specific format for the user agent. You **MUST** update this with your actual Reddit username or your requests may be blocked. The correct format is: `script:reddit-tracker:v1.0.0 (by /u/your_actual_username)`.

- **Subreddits**: You can track multiple subreddits by adding them as a comma-separated list, e.g., `REDDIT_SUBREDDITS=AskReddit,news,golang` but this isn't fully tested.  

- **Rate Limits**: Reddit enforces a rate limit of 100 requests per minute (or 1000 requests per 10 minutes). The application automatically manages this, but you can adjust `REDDIT_MAX_REQUESTS_PER_MINUTE` if needed.

- **Log Level**: Set to `debug` for more verbose output or `info` for standard operation. Use `warn` or `error` in production to reduce output volume.

### Building and Running

1. Clone the repository (if you haven't already):
   ```
   git clone https://github.com/brettboylen/reddit-tracker.git
   cd reddit-tracker
   ```

2. Download dependencies:
   ```
   go mod tidy
   ```

3. Build the application:
   ```
   make build
   ```

4. Run the application:
   ```
   ./reddit-tracker
   ```

   Or with the Makefile:
   ```
   make run
   ```

   Optional flags:
   - `-env`: Path to .env file (default: `.env`)
   - `-log-level`: Logging level (debug, info, warn, error) (default: `info`)

### Verifying Proper Setup

1. After starting the application, you should see log output confirming:
   - Successful loading of configuration
   - Authentication with Reddit API
   - Fetching of posts from your configured subreddits
   - Regular statistics updates

2. Visit `http://localhost:8080/api/stats` in your browser to see the collected statistics.

3. For a specific subreddit's stats, visit `http://localhost:8080/api/stats/AskReddit` (replace AskReddit with any subreddit you're tracking).

### Troubleshooting

If you encounter issues:

1. **Authentication failures**: Double-check your `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET` values.

2. **Rate limiting errors**: Ensure your `REDDIT_USER_AGENT` follows the correct format with your actual Reddit username.

3. **No data appearing**: Verify the subreddits you're tracking exist and are spelled correctly.

4. **Debug mode**: Run with `-log-level=debug` for more detailed logging information.

### API Endpoints

- **GET /api/stats**: Returns the current statistics for all tracked subreddits in JSON format
- **GET /api/stats/:subreddit**: Returns statistics for a specific subreddit
- **GET /healthz**: Health check endpoint

## How It Works

1. The application fetches posts from each configured subreddit at regular intervals.
2. It monitors and respects Reddit's rate limiting through response headers.
3. Each post is processed concurrently and stored in the SQLite database.
4. Statistics are calculated and updated after each batch of posts is processed.
5. Per-subreddit statistics are tracked and made available through the API.
6. The application provides real-time statistics through an Echo-powered REST API.

## Rate Limiting

The application respects Reddit's rate limits by:

1. Reading the rate limit headers from each response (`X-Ratelimit-Used`, `X-Ratelimit-Remaining`, `X-Ratelimit-Reset`)
2. Dynamically adjusting the request rate to ensure we stay within the allowed limits (NOTE: this is currently broken due to a bug with X-RateLimit-Remaining on Reddit's side always returning 0); 
3. Using Echo's built-in rate limiting middleware for the API endpoints

## Scaling Considerations

The application is designed with scalability in mind:

- Tracks multiple subreddits concurrently
- Each subreddit has its own pagination key for efficient updates
- Concurrent processing of posts
- Database example with proper indexing
- Rate limiting respects Reddit's API constraints
- Clean separation of concerns
- Echo framework for high-performance API endpoints

package utils

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

const testEnvPath = "./test.env"

func cleanup() {
	os.Remove(testEnvPath)
}

// TestMain handles test setup and cleanup for all tests in this package
func TestMain(m *testing.M) {
	exitCode := m.Run()

	cleanup()

	os.Exit(exitCode)
}

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_ENV_VAR", "test-value")
	defer os.Unsetenv("TEST_ENV_VAR")

	value := getEnv("TEST_ENV_VAR", "default-value")
	assert.Equal(t, "test-value", value)

	value = getEnv("NON_EXISTENT_VAR", "default-value")
	assert.Equal(t, "default-value", value)
}

func TestGetEnvAsInt(t *testing.T) {
	os.Setenv("TEST_INT_VAR", "42")
	defer os.Unsetenv("TEST_INT_VAR")

	value := getEnvAsInt("TEST_INT_VAR", 10)
	assert.Equal(t, 42, value)

	os.Setenv("TEST_INVALID_INT_VAR", "not-an-int")
	defer os.Unsetenv("TEST_INVALID_INT_VAR")

	value = getEnvAsInt("TEST_INVALID_INT_VAR", 10)
	assert.Equal(t, 10, value)

	value = getEnvAsInt("NON_EXISTENT_VAR", 10)
	assert.Equal(t, 10, value)
}

func TestValidateConfig(t *testing.T) {
	//valid
	validConfig := &Config{
		Reddit: RedditConfig{
			ClientID:       "id",
			ClientSecret:   "secret",
			UserAgent:      "agent",
			Subreddits:     []string{"golang"},
			PollingInterval: 60,
		},
		Database: DatabaseConfig{
			Path: "./test.db",
		},
	}
	assert.NoError(t, validateConfig(validConfig))

	// invalid config
	invalidConfig := &Config{
		Reddit: RedditConfig{
			ClientID:       "",
			ClientSecret:   "secret",
			UserAgent:      "agent",
			Subreddits:     []string{"golang"},
			PollingInterval: 60,
		},
	}
	err := validateConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REDDIT_CLIENT_ID")

	// invaid config
	invalidConfig = &Config{
		Reddit: RedditConfig{
			ClientID:       "id",
			ClientSecret:   "secret",
			UserAgent:      "agent",
			Subreddits:     []string{"golang"},
			PollingInterval: -1,
		},
	}
	err = validateConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REDDIT_POLLING_INTERVAL")
}

func TestParseSubreddits(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Single subreddit",
			input:    "AskReddit",
			expected: []string{"AskReddit"},
		},
		{
			name:     "Multiple subreddits",
			input:    "AskReddit,news,programming",
			expected: []string{"AskReddit", "news", "programming"},
		},
		{
			name:     "Subreddits with whitespace",
			input:    "AskReddit, news, programming",
			expected: []string{"AskReddit", "news", "programming"},
		},
		{
			name:     "Subreddits with extra commas",
			input:    "AskReddit,,news,,programming",
			expected: []string{"AskReddit", "news", "programming"},
		},
		{
			name:     "Subreddits with leading/trailing commas",
			input:    ",AskReddit,news,programming,",
			expected: []string{"AskReddit", "news", "programming"},
		},
		{
			name:     "Mixed case subreddits",
			input:    "askReddit,NEWS,Programming",
			expected: []string{"askReddit", "NEWS", "Programming"},
		},
		{
			name:     "Subreddits with extra whitespace",
			input:    "  AskReddit  ,  news  ,  programming  ",
			expected: []string{"AskReddit", "news", "programming"},
		},
		{
			name:     "Underscore in subreddit names",
			input:    "Ask_Reddit,data_science",
			expected: []string{"Ask_Reddit", "data_science"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseSubreddits(tc.input)
			
			// ignore empty string test case for now
			// TODO: probably should error here instead?
			if tc.input == "" {
				return
			}
			
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("parseSubreddits(%q) = %v; want %v", 
					tc.input, result, tc.expected)
			}
		})
	}
}

func TestParseSubredditsEdgeCases(t *testing.T) {
	// Test that multiple consecutive spaces are handled properly
	result := parseSubreddits("AskReddit,     news,programming")
	expected := []string{"AskReddit", "news", "programming"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Failed to handle multiple spaces: got %v, want %v", result, expected)
	}
	
	// Test with tab characters
	result = parseSubreddits("AskReddit,\tnews,\tprogramming")
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Failed to handle tab characters: got %v, want %v", result, expected)
	}
	
	// Test with newline characters
	result = parseSubreddits("AskReddit,\nnews,\nprogramming")
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Failed to handle newline characters: got %v, want %v", result, expected)
	}
	
	// Test with different whitespace combinations
	result = parseSubreddits(" AskReddit ,\t news\n, programming ")
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Failed to handle mixed whitespace: got %v, want %v", result, expected)
	}
} 
package api

import (
	"net/http"
	"testing"
	"time"
)

func TestGetHeaderAsInt(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string][]string
		key      string
		expected int
	}{
		{
			name: "Valid integer header",
			headers: map[string][]string{
				"X-Ratelimit-Remaining": {"42"},
			},
			key:      "X-Ratelimit-Remaining",
			expected: 42,
		},
		{
			name: "Empty header value",
			headers: map[string][]string{
				"X-Ratelimit-Remaining": {""},
			},
			key:      "X-Ratelimit-Remaining",
			expected: 0,
		},
		{
			name: "Missing header",
			headers: map[string][]string{
				"X-Ratelimit-Used": {"10"},
			},
			key:      "X-Ratelimit-Remaining",
			expected: 0,
		},
		{
			name: "Non-integer header value",
			headers: map[string][]string{
				"X-Ratelimit-Remaining": {"not-a-number"},
			},
			key:      "X-Ratelimit-Remaining",
			expected: 0,
		},
		{
			name: "Multiple values for same header (should use first)",
			headers: map[string][]string{
				"X-Ratelimit-Remaining": {"100", "200"},
			},
			key:      "X-Ratelimit-Remaining",
			expected: 100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header(tc.headers)
			result := getHeaderAsInt(header, tc.key)
			if result != tc.expected {
				t.Errorf("getHeaderAsInt(%v, %q) = %d; want %d", 
					header, tc.key, result, tc.expected)
			}
		})
	}
}

func TestTokenBucketUpdate(t *testing.T) {
	tb := NewTokenBucket(10, 1.0, time.Second)
	
	tb.Update(200, 400, 1000) // 200 used, 400 seconds left in period, 1000 requests allowed
	
	// we expect .95 of the full rate
	expectedRate := (1000.0 / 600.0) * 0.95
	
	if tb.fillRate != expectedRate {
		t.Errorf("Update() fillRate = %f; want %f", tb.fillRate, expectedRate)
	}
} 
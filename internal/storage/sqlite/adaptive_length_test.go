package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCollisionProbability(t *testing.T) {
	tests := []struct {
		numIssues int
		idLength  int
		expected  float64 // approximate
	}{
		{50, 4, 0.0007},   // ~0.07%
		{500, 4, 0.0717},  // ~7.17%
		{1000, 5, 0.0082}, // ~0.82%
		{1000, 6, 0.0002}, // ~0.02%
	}

	for _, tt := range tests {
		got := collisionProbability(tt.numIssues, tt.idLength)
		
		// Allow 20% tolerance for approximation (birthday paradox is an approximation)
		diff := got - tt.expected
		if diff < 0 {
			diff = -diff
		}
		tolerance := tt.expected * 0.2
		
		if diff > tolerance {
			t.Errorf("collisionProbability(%d, %d) = %f, want ~%f (diff: %f)",
				tt.numIssues, tt.idLength, got, tt.expected, diff)
		}
	}
}

func TestComputeAdaptiveLength(t *testing.T) {
	tests := []struct {
		name      string
		numIssues int
		config    AdaptiveIDConfig
		want      int
	}{
		{
			name:      "small database uses 4 chars",
			numIssues: 50,
			config:    DefaultAdaptiveConfig(),
			want:      4,
		},
		{
			name:      "medium database uses 4 chars",
			numIssues: 500,
			config:    DefaultAdaptiveConfig(),
			want:      4,
		},
		{
			name:      "large database uses 5 chars",
			numIssues: 1000,
			config:    DefaultAdaptiveConfig(),
			want:      5,
		},
		{
			name:      "very large database uses 6 chars",
			numIssues: 10000,
			config:    DefaultAdaptiveConfig(),
			want:      6,
		},
		{
			name:      "custom threshold - stricter",
			numIssues: 200,
			config: AdaptiveIDConfig{
				MaxCollisionProbability: 0.01, // 1% threshold
				MinLength:               4,
				MaxLength:               8,
			},
			want: 5,
		},
		{
			name:      "custom threshold - more lenient",
			numIssues: 1000,
			config: AdaptiveIDConfig{
				MaxCollisionProbability: 0.50, // 50% threshold
				MinLength:               4,
				MaxLength:               8,
			},
			want: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeAdaptiveLength(tt.numIssues, tt.config)
			if got != tt.want {
				t.Errorf("computeAdaptiveLength(%d) = %d, want %d",
					tt.numIssues, got, tt.want)
			}
		})
	}
}

func TestGenerateHashID_VariableLengths(t *testing.T) {
	prefix := "bd"
	title := "Test issue"
	description := "Test description"
	creator := "test@example.com"
	timestamp, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	
	tests := []struct {
		length       int
		expectedLen  int // length of hash portion (without prefix)
	}{
		{4, 4},
		{5, 5},
		{6, 6},
		{7, 7},
		{8, 8},
	}
	
	for _, tt := range tests {
		t.Run(fmt.Sprintf("length_%d", tt.length), func(t *testing.T) {
			id := generateHashID(prefix, title, description, creator, timestamp, tt.length, 0)
			
			// Format: "bd-xxxx" where xxxx is the hash
			if !strings.HasPrefix(id, prefix+"-") {
				t.Errorf("ID should start with %s-, got %s", prefix, id)
			}
			
			hashPart := strings.TrimPrefix(id, prefix+"-")
			if len(hashPart) != tt.expectedLen {
				t.Errorf("Hash length = %d, want %d (full ID: %s)",
					len(hashPart), tt.expectedLen, id)
			}
		})
	}
}

func TestGetAdaptiveIDLength_Integration(t *testing.T) {
	// Create in-memory database
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	ctx := context.Background()
	
	// Initialize with prefix
	if err := db.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set prefix: %v", err)
	}
	
	// Test default config (should use 4 chars for empty database)
	conn, err := db.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Failed to get connection: %v", err)
	}
	defer conn.Close()
	
	length, err := GetAdaptiveIDLength(ctx, conn, "test")
	if err != nil {
		t.Fatalf("GetAdaptiveIDLength failed: %v", err)
	}
	
	if length != 4 {
		t.Errorf("Empty database should use 4 chars, got %d", length)
	}
	
	// Test custom config
	if err := db.SetConfig(ctx, "max_collision_prob", "0.01"); err != nil {
		t.Fatalf("Failed to set max_collision_prob: %v", err)
	}
	
	if err := db.SetConfig(ctx, "min_hash_length", "5"); err != nil {
		t.Fatalf("Failed to set min_hash_length: %v", err)
	}
	
	length, err = GetAdaptiveIDLength(ctx, conn, "test")
	if err != nil {
		t.Fatalf("GetAdaptiveIDLength with custom config failed: %v", err)
	}
	
	if length < 5 {
		t.Errorf("With min_hash_length=5, got %d", length)
	}
}

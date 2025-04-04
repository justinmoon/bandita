package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"testing"
	"time"
	
	"bandita/dvm"
)

// generateTestPrivateKey creates a random private key for testing
func generateTestPrivateKey() (string, error) {
	sk := make([]byte, 32)
	if _, err := rand.Read(sk); err != nil {
		return "", err
	}
	return hex.EncodeToString(sk), nil
}

func TestTweetDvm(t *testing.T) {
	relayURL := "wss://relay.nostr.net"

	// Generate a temporary private key for testing
	sk, err := generateTestPrivateKey()
	if err != nil {
		t.Fatalf("failed to generate test private key: %v", err)
	}
	
	dvmInstance, err := dvm.NewDvm(relayURL, sk)
	if err != nil {
		t.Fatalf("failed to create dvm: %v", err)
	}

	// Run DVM in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := dvmInstance.Run(); err != nil {
			log.Printf("DVM run error: %v", err)
		}
	}()

	defer func() {
		dvmInstance.Stop()
		wg.Wait()
	}()

	client, err := dvm.NewDvmClient(relayURL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Timeout context for waiting on a response
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Request the "Running bitcoin" tweet from Hal Finney
	tweet, err := client.RequestTweet(ctx, dvmInstance.GetPublicKey(), "1110302988")
	if err != nil {
		t.Fatalf("error requesting tweet: %v", err)
	}

	// Pretty print the full tweet structure
	tweetJSON, err := json.MarshalIndent(tweet, "", "  ")
	if err != nil {
		t.Fatalf("error marshaling tweet: %v", err)
	}
	t.Logf("Full tweet structure:\n%s", tweetJSON)

	if tweet.Username != "halfin" {
		t.Errorf("expected username 'halfin', got %q", tweet.Username)
	}

	if tweet.Text != "Running bitcoin" {
		t.Errorf("expected text 'Running bitcoin', got %q", tweet.Text)
	}

	t.Logf("SUCCESS: Received tweet from @%s: %q", tweet.Username, tweet.Text)
}
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"bandita/dvm"
)

func extractTweetID(tweetURL string) (string, error) {
	// Different twitter URL patterns
	patterns := []*regexp.Regexp{
		// Standard format: https://twitter.com/username/status/1234567890
		regexp.MustCompile(`twitter\.com/[^/]+/status/(\d+)`),
		// X.com format: https://x.com/username/status/1234567890
		regexp.MustCompile(`x\.com/[^/]+/status/(\d+)`),
		// t.co format that redirects to twitter
		regexp.MustCompile(`t\.co/([a-zA-Z0-9]+)`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(tweetURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	// Check if it's just the ID
	if matched, _ := regexp.MatchString(`^\d+$`, tweetURL); matched {
		return tweetURL, nil
	}

	return "", fmt.Errorf("unable to extract tweet ID from URL: %s", tweetURL)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: cli <tweet-url>")
		os.Exit(1)
	}

	tweetURL := os.Args[1]
	tweetID, err := extractTweetID(tweetURL)
	if err != nil {
		log.Fatalf("Error extracting tweet ID: %v", err)
	}

	// Default relay if none is provided
	relayURL := "wss://relay.damus.io"
	if len(os.Args) > 2 {
		relayURL = os.Args[2]
	}

	// Default DVM pubkey - In a real app, you'd want to get this from config
	// This is a placeholder - replace with your actual DVM pubkey
	dvmPubKey := "" // Add your DVM's pubkey here
	if dvmPubKey == "" {
		// Create a temporary DVM just to get its pubkey
		tempDvm, err := dvm.NewDvm(relayURL)
		if err != nil {
			log.Fatalf("Failed to create temporary DVM: %v", err)
		}
		dvmPubKey = tempDvm.GetPublicKey()
	}

	client, err := dvm.NewDvmClient(relayURL)
	if err != nil {
		log.Fatalf("Failed to create DVM client: %v", err)
	}

	// Set a timeout for the request
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("Requesting tweet ID %s from relay %s\n", tweetID, relayURL)
	tweet, err := client.RequestTweet(ctx, dvmPubKey, tweetID)
	if err != nil {
		log.Fatalf("Error fetching tweet: %v", err)
	}

	// Pretty print the JSON response
	tweetJSON, err := json.MarshalIndent(tweet, "", "  ")
	if err != nil {
		log.Fatalf("Error formatting JSON: %v", err)
	}

	fmt.Println(string(tweetJSON))
}
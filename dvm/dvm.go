package dvm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/imperatrona/twitter-scraper"
	"github.com/nbd-wtf/go-nostr"
)

// generatePrivateKey creates a random 32-byte hex string for ephemeral usage.
func generatePrivateKey() (string, error) {
	sk := make([]byte, 32)
	if _, err := rand.Read(sk); err != nil {
		return "", err
	}
	return hex.EncodeToString(sk), nil
}

// Dvm listens for kind=42069 events containing a tweet ID, then responds with tweet data.
type Dvm struct {
	sk      string
	pk      string
	relay   *nostr.Relay
	done    chan struct{}
	scraper *twitterscraper.Scraper
	sync.Once // For ensuring done channel is closed only once
}

// GetPublicKey returns the DVM's public key
func (d *Dvm) GetPublicKey() string {
	return d.pk
}

// NewDvm creates a new DVM instance connected to the specified relay.
func NewDvm(relayURL string) (*Dvm, error) {
	sk, err := generatePrivateKey()
	if err != nil {
		return nil, err
	}
	pk, _ := nostr.GetPublicKey(sk)

	relay, err := nostr.RelayConnect(context.Background(), relayURL)
	if err != nil {
		return nil, err
	}

	// Initialize the scraper
	scraper := twitterscraper.New()

	return &Dvm{
		sk:      sk,
		pk:      pk,
		relay:   relay,
		done:    make(chan struct{}),
		scraper: scraper,
	}, nil
}

// Run subscribes to job requests and responds with tweet data.
func (d *Dvm) Run() error {
	ctx, cancel := context.WithCancel(context.Background())

	log.Printf("DVM starting subscription for tweet requests (kind=42069)")
	// Subscribe to all events of kind=42069
	since := nostr.Timestamp(time.Now().Add(-time.Second).Unix())
	sub, err := d.relay.Subscribe(ctx, nostr.Filters{
		nostr.Filter{
			Kinds: []int{42069},
			Since: &since,
		},
	})
	if err != nil {
		log.Printf("DVM subscription error: %v", err)
		return err
	}

	log.Printf("DVM subscription active - listening for events")

	defer func() {
		log.Printf("DVM shutting down subscription")
		cancel()
		sub.Unsub()
	}()

	for {
		select {
		case evt := <-sub.Events:
			if evt.Kind == 42069 {
				log.Printf("DVM received job request: id=%s from=%s tweet_id=%s", 
					evt.ID[:8], evt.PubKey[:8], evt.Content)
				
				// Get the tweet data
				log.Printf("Fetching tweet data for ID: %s", evt.Content)
				startTime := time.Now()
				tweet, err := d.scraper.GetTweet(evt.Content)
				if err != nil {
					log.Printf("Error getting tweet %s: %v", evt.Content, err)
					continue
				}
				log.Printf("Successfully fetched tweet in %v: @%s: %s", 
					time.Since(startTime), tweet.Username, tweet.Text)

				// Convert tweet to JSON
				tweetJSON, err := json.Marshal(tweet)
				if err != nil {
					log.Printf("Error marshaling tweet: %v", err)
					continue
				}

				// Build response event with tweet data
				log.Printf("Publishing response for request %s", evt.ID[:8])
				resp := nostr.Event{
					PubKey:    d.pk,
					CreatedAt: nostr.Timestamp(time.Now().Unix()),
					Kind:      1,
					Tags: nostr.Tags{
						{"e", evt.ID},     // Reference the request event
						{"p", evt.PubKey}, // Reference the requester's pubkey
					},
					Content: string(tweetJSON),
				}
				if err := resp.Sign(d.sk); err != nil {
					log.Printf("DVM sign error: %v", err)
					continue
				}
				
				publishStart := time.Now()
				log.Printf("Publishing tweet data response to relay...")
				if _, err := d.relay.Publish(context.Background(), resp); err != nil {
					log.Printf("DVM publish error: %v", err)
				} else {
					log.Printf("Successfully published response in %v", time.Since(publishStart))
				}
			}
		case <-d.done:
			log.Printf("DVM received shutdown signal")
			return nil
		}
	}
}

// Stop signals the DVM to shutdown.
func (d *Dvm) Stop() {
	d.Do(func() {
		close(d.done)
	})
}

// DvmClient publishes a tweet ID and waits for the tweet data response.
type DvmClient struct {
	sk    string
	pk    string
	relay *nostr.Relay
}

// NewDvmClient creates a new client for interacting with the DVM.
func NewDvmClient(relayURL string) (*DvmClient, error) {
	sk, err := generatePrivateKey()
	if err != nil {
		return nil, err
	}
	pk, _ := nostr.GetPublicKey(sk)

	relay, err := nostr.RelayConnect(context.Background(), relayURL)
	if err != nil {
		return nil, err
	}

	return &DvmClient{
		sk:    sk,
		pk:    pk,
		relay: relay,
	}, nil
}

// RequestTweet publishes a job event with a tweet ID and waits for the response.
func (c *DvmClient) RequestTweet(ctx context.Context, dvmPubKey string, tweetID string) (*twitterscraper.Tweet, error) {
	log.Printf("Creating tweet request for ID: %s from DVM: %s", tweetID, dvmPubKey[:8])
	
	// Create the job request event first
	evt := nostr.Event{
		PubKey:    c.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      42069,
		Tags:      nostr.Tags{},
		Content:   tweetID,
	}
	if err := evt.Sign(c.sk); err != nil {
		log.Printf("Error signing request event: %v", err)
		return nil, err
	}
	log.Printf("Created request event with ID: %s", evt.ID[:8])

	// Subscribe to potential responses that reference our request
	log.Printf("Setting up subscription for responses from DVM")
	since := nostr.Timestamp(time.Now().Add(-time.Second).Unix())
	sub, err := c.relay.Subscribe(ctx, nostr.Filters{
		nostr.Filter{
			Kinds:   []int{1},
			Authors: []string{dvmPubKey}, // Only get responses from the DVM
			Tags: nostr.TagMap{ // Look for responses that reference our request
				"e": []string{evt.ID},    // Event reference
				"p": []string{c.pk},      // Our pubkey reference
			},
			Since: &since,
		},
	})
	if err != nil {
		log.Printf("Subscription error: %v", err)
		return nil, err
	}
	defer sub.Unsub()
	log.Printf("Subscription set up successfully")

	// Now publish the request
	log.Printf("Publishing request for tweet ID: %s", tweetID)
	publishStart := time.Now()
	if _, err := c.relay.Publish(ctx, evt); err != nil {
		log.Printf("Error publishing request: %v", err)
		return nil, err
	}
	log.Printf("Request published in %v", time.Since(publishStart))

	deadline, ok := ctx.Deadline()
	if ok {
		log.Printf("Waiting for response from DVM (timeout: %v)...", 
			time.Until(deadline))
	} else {
		log.Printf("Waiting for response from DVM (no timeout set)...")
	}

	// Wait for a matching response
	for {
		select {
		case e := <-sub.Events:
			log.Printf("Received event kind=%d from=%s", e.Kind, e.PubKey[:8])
			if e.Kind == 1 {
				log.Printf("Received tweet data response from DVM")
				var tweet twitterscraper.Tweet
				if err := json.Unmarshal([]byte(e.Content), &tweet); err != nil {
					log.Printf("Error unmarshaling tweet data: %v", err)
					return nil, err
				}
				log.Printf("Successfully parsed tweet from @%s: %s", 
					tweet.Username, tweet.Text)
				return &tweet, nil
			}
		case <-ctx.Done():
			log.Printf("Request timed out after waiting for response")
			return nil, ctx.Err()
		}
	}
}
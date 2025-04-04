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

	// Subscribe to all events of kind=42069
	since := nostr.Timestamp(time.Now().Add(-time.Second).Unix())
	sub, err := d.relay.Subscribe(ctx, nostr.Filters{
		nostr.Filter{
			Kinds: []int{42069},
			Since: &since,
		},
	})
	if err != nil {
		return err
	}

	defer func() {
		cancel()
		sub.Unsub()
	}()

	for {
		select {
		case evt := <-sub.Events:
			if evt.Kind == 42069 {
				// Get the tweet data
				tweet, err := d.scraper.GetTweet(evt.Content)
				if err != nil {
					log.Printf("error getting tweet: %v", err)
					continue
				}

				// Convert tweet to JSON
				tweetJSON, err := json.Marshal(tweet)
				if err != nil {
					log.Printf("error marshaling tweet: %v", err)
					continue
				}

				// Build response event with tweet data
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
					log.Printf("dvm sign error: %v", err)
					continue
				}
				if _, err := d.relay.Publish(context.Background(), resp); err != nil {
					log.Printf("dvm publish error: %v", err)
				}
			}
		case <-d.done:
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
	// Create the job request event first
	evt := nostr.Event{
		PubKey:    c.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      42069,
		Tags:      nostr.Tags{},
		Content:   tweetID,
	}
	if err := evt.Sign(c.sk); err != nil {
		return nil, err
	}

	// Subscribe to potential responses that reference our request
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
		return nil, err
	}
	defer sub.Unsub()

	// Now publish the request
	if _, err := c.relay.Publish(ctx, evt); err != nil {
		return nil, err
	}

	// Wait for a matching response
	for {
		select {
		case e := <-sub.Events:
			if e.Kind == 1 {
				var tweet twitterscraper.Tweet
				if err := json.Unmarshal([]byte(e.Content), &tweet); err != nil {
					return nil, err
				}
				return &tweet, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
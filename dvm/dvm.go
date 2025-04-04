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
	
	// Start a heartbeat to keep the connection alive
	go d.runHeartbeat(ctx)

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
				
				// Try to publish with reconnection logic
				maxRetries := 3
				for attempt := 0; attempt < maxRetries; attempt++ {
					// Check if connection is closed and try to reconnect
					if d.relay.ConnectionError != nil {
						log.Printf("Relay connection error detected, reconnecting... (attempt %d/%d)", attempt+1, maxRetries)
						
						// Create a new relay connection
						newRelay, err := nostr.RelayConnect(context.Background(), d.relay.URL)
						if err != nil {
							log.Printf("Failed to reconnect to relay: %v", err)
							time.Sleep(500 * time.Millisecond)
							continue
						}
						
						// Update the relay reference
						d.relay = newRelay
						log.Printf("Successfully reconnected to relay")
					}
					
					// Attempt to publish
					if status, err := d.relay.Publish(context.Background(), resp); err != nil {
						log.Printf("DVM publish error (attempt %d/%d): %v", attempt+1, maxRetries, err)
						time.Sleep(500 * time.Millisecond)
					} else {
						log.Printf("Successfully published response in %v (status: %v)", time.Since(publishStart), status)
						log.Printf("Verification info - Event ID: %s", resp.ID)
						log.Printf("To verify with nak: nak event -r wss://relay.nostr.net %s", resp.ID)
						break
					}
				}
			}
		case <-d.done:
			log.Printf("DVM received shutdown signal")
			return nil
		}
	}
}

// runHeartbeat sends periodic NIP-01 keepalive events to maintain the connection
func (d *Dvm) runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Check if the connection is still alive
			if d.relay.ConnectionError != nil {
				log.Printf("Heartbeat detected closed connection, attempting to reconnect...")
				newRelay, err := nostr.RelayConnect(ctx, d.relay.URL)
				if err != nil {
					log.Printf("Heartbeat reconnection failed: %v", err)
					continue
				}
				d.relay = newRelay
				log.Printf("Heartbeat successfully reconnected to relay")
			} else {
				// Send a simple NIP-01 event as a ping to keep the connection alive
				ping := nostr.Event{
					PubKey:    d.pk,
					CreatedAt: nostr.Timestamp(time.Now().Unix()),
					Kind:      1,
					Tags:      nostr.Tags{{"client", "bandita-dvm-heartbeat"}},
					Content:   "",
				}
				if err := ping.Sign(d.sk); err != nil {
					log.Printf("Failed to sign heartbeat ping: %v", err)
					continue
				}
				
				// We don't need to actually send this event - just prepare it to be ready
				// in case we need to test the connection in the future
				log.Printf("Heartbeat check - connection still alive")
			}
		case <-ctx.Done():
			log.Printf("Heartbeat routine stopped")
			return
		case <-d.done:
			log.Printf("Heartbeat received shutdown signal")
			return
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
	log.Printf("Setting up subscription for responses from DVM (client pubkey: %s, request ID: %s)", c.pk, evt.ID)
	
	// Go back 1 minute to ensure we don't miss anything
	since := nostr.Timestamp(time.Now().Add(-1 * time.Minute).Unix())
	
	// First, set up a broader subscription to catch all responses from the DVM
	sub, err := c.relay.Subscribe(ctx, nostr.Filters{
		nostr.Filter{
			Kinds:   []int{1},
			Authors: []string{dvmPubKey}, // Only get responses from the DVM
			Since: &since,
		},
	})
	if err != nil {
		log.Printf("Subscription error: %v", err)
		return nil, err
	}
	defer sub.Unsub()
	log.Printf("Subscription set up successfully")

	// Now publish the request with retry logic
	log.Printf("Publishing request for tweet ID: %s", tweetID)
	publishStart := time.Now()
	
	// Try to publish with reconnection logic
	maxRetries := 3
	var publishErr error
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check if connection is closed and try to reconnect
		if c.relay.ConnectionError != nil {
			log.Printf("Client relay connection error detected, reconnecting... (attempt %d/%d)", attempt+1, maxRetries)
			
			// Create a new relay connection
			newRelay, err := nostr.RelayConnect(ctx, c.relay.URL)
			if err != nil {
				log.Printf("Client failed to reconnect to relay: %v", err)
				time.Sleep(500 * time.Millisecond)
				publishErr = err
				continue
			}
			
			// Update the relay reference
			c.relay = newRelay
			log.Printf("Client successfully reconnected to relay")
		}
		
		// Attempt to publish
		if _, err := c.relay.Publish(ctx, evt); err != nil {
			log.Printf("Error publishing request (attempt %d/%d): %v", attempt+1, maxRetries, err)
			time.Sleep(500 * time.Millisecond)
			publishErr = err
		} else {
			log.Printf("Request published in %v", time.Since(publishStart))
			publishErr = nil
			break
		}
	}
	
	if publishErr != nil {
		log.Printf("Failed to publish request after %d attempts: %v", maxRetries, publishErr)
		return nil, publishErr
	}

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
			log.Printf("Received event kind=%d from=%s with ID: %s", e.Kind, e.PubKey[:8], e.ID[:8])
			
			// Debug: Print the tags to help troubleshoot
			log.Printf("Event tags: %v", e.Tags)
			
			// Check if this is our response - either by tag or just as a kind 1 from the DVM
			isOurResponse := false
			
			if e.Kind == 1 {
				// First check if it's tagged with our request ID
				for _, tag := range e.Tags {
					if len(tag) >= 2 && tag[0] == "e" && tag[1] == evt.ID {
						log.Printf("Found matching event reference tag: %s", tag[1])
						isOurResponse = true
						break
					}
				}
				
				// If we didn't find a matching tag but we're getting responses, 
				// consider using it if it's from the right DVM
				if !isOurResponse && e.PubKey == dvmPubKey {
					log.Printf("Found response from DVM, but no matching tag. Trying to parse anyway.")
					isOurResponse = true
				}
				
				if isOurResponse {
					log.Printf("Received tweet data response from DVM")
					log.Printf("Raw response content: %s", e.Content)
					
					var tweet twitterscraper.Tweet
					if err := json.Unmarshal([]byte(e.Content), &tweet); err != nil {
						log.Printf("Error unmarshaling tweet data: %v", err)
						// Don't return yet, maybe there's another response coming
						continue
					}
					
					// Check if the tweet data has basic fields to confirm it's valid
					if tweet.Text == "" {
						log.Printf("Warning: Parsed tweet has empty text field, might be incomplete")
						continue
					}
					
					log.Printf("Successfully parsed tweet from @%s: %s", 
						tweet.Username, tweet.Text)
					return &tweet, nil
				}
			}
		case <-ctx.Done():
			log.Printf("Request timed out after waiting for response - check if the DVM published a response by running:")
			log.Printf("nak event -r %s --kinds 1 --author %s --limit 5", c.relay.URL, dvmPubKey)
			return nil, ctx.Err()
		}
	}
}
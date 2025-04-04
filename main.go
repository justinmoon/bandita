package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
	"time"

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

// Dvm listens for kind=42069 events, then responds with "hello world" (kind=1).
type Dvm struct {
	sk    string
	pk    string
	relay *nostr.Relay
	done  chan struct{}
	sync.Once // For ensuring done channel is closed only once
}

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

	return &Dvm{
		sk:    sk,
		pk:    pk,
		relay: relay,
		done:  make(chan struct{}),
	}, nil
}

// Run subscribes to job requests and responds with "hello world".
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
				// Build response event of kind=1 with content "hello world"
				resp := nostr.Event{
					PubKey:    d.pk,
					CreatedAt: nostr.Timestamp(time.Now().Unix()),
					Kind:      1,
					// Add tags to link this response to the request
					Tags: nostr.Tags{
						{"e", evt.ID},    // Reference the request event
						{"p", evt.PubKey}, // Reference the requester's pubkey
					},
					Content: "hello world",
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

// ---------------------------------------------------------

// DvmClient publishes a "job" event (kind=42069) and waits for the response (kind=1).
type DvmClient struct {
	sk    string
	pk    string
	relay *nostr.Relay
}

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

// RequestHelloWorld publishes a job event and waits for any "kind=1" event in response.
func (c *DvmClient) RequestHelloWorld(ctx context.Context, dvmPubKey string) (string, error) {
	// Create the job request event first so we can get its ID
	evt := nostr.Event{
		PubKey:    c.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      42069,
		Tags:      nostr.Tags{},
		Content:   "",
	}
	if err := evt.Sign(c.sk); err != nil {
		return "", err
	}

	// Subscribe to potential responses that reference our request
	since := nostr.Timestamp(time.Now().Add(-time.Second).Unix())
	sub, err := c.relay.Subscribe(ctx, nostr.Filters{
		nostr.Filter{
			Kinds:   []int{1},
			Authors: []string{dvmPubKey}, // Only get responses from the DVM
			Tags: nostr.TagMap{          // Look for responses that reference our request
				"e": []string{evt.ID},    // Event reference
				"p": []string{c.pk},      // Our pubkey reference
			},
			Since: &since,
		},
	})
	if err != nil {
		return "", err
	}
	defer sub.Unsub()

	// Now publish the request
	if _, err := c.relay.Publish(ctx, evt); err != nil {
		return "", err
	}

	// Wait for a matching response
	for {
		select {
		case e := <-sub.Events:
			if e.Kind == 1 {
				return e.Content, nil
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func main() {
	log.Println("Starting Nostr DVM...")
	
	relayURL := "wss://relay.damus.io"
	
	dvm, err := NewDvm(relayURL)
	if err != nil {
		log.Fatalf("Failed to create DVM: %v", err)
	}

	// Run the DVM - this will block until Stop() is called
	if err := dvm.Run(); err != nil {
		log.Fatalf("DVM error: %v", err)
	}
}
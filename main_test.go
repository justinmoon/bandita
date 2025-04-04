package main

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"
)

func TestHelloWorldDvm(t *testing.T) {
	relayURL := "wss://relay.damus.io"

	dvm, err := NewDvm(relayURL)
	if err != nil {
		t.Fatalf("failed to create dvm: %v", err)
	}

	// Run DVM in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := dvm.Run(); err != nil {
			log.Printf("DVM run error: %v", err)
		}
	}()

	defer func() {
		dvm.Stop()
		wg.Wait()
	}()

	client, err := NewDvmClient(relayURL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Timeout context for waiting on a response
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.RequestHelloWorld(ctx, dvm.pk)
	if err != nil {
		t.Fatalf("error requesting hello world: %v", err)
	}

	if result != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result)
	}
	t.Logf("SUCCESS: Received %q from DVM.", result)
}
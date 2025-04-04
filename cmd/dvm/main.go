package main

import (
	"log"
	"os"

	"bandita/dvm"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Starting Nostr DVM...")
	
	// Configure relay URL
	relayURL := "wss://relay.damus.io"
	
	// Get alternative relay from environment if available
	if envRelay := os.Getenv("NOSTR_RELAY"); envRelay != "" {
		relayURL = envRelay
		log.Printf("Using relay from environment: %s", relayURL)
	}
	
	log.Printf("Connecting to relay: %s", relayURL)
	dvmInstance, err := dvm.NewDvm(relayURL)
	if err != nil {
		log.Fatalf("Failed to create DVM: %v", err)
	}

	pubkey := dvmInstance.GetPublicKey()
	log.Printf("========================================")
	log.Printf("DVM Successfully initialized")
	log.Printf("Public Key: %s", pubkey)
	log.Printf("Relay: %s", relayURL)
	log.Printf("========================================")
	log.Printf("Ready to receive tweet fetch requests...")

	// Run the DVM - this will block until Stop() is called
	if err := dvmInstance.Run(); err != nil {
		log.Fatalf("DVM error: %v", err)
	}
}
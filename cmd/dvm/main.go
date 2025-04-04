package main

import (
	"log"
	"os"

	"bandita/dvm"
	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Starting Nostr DVM...")
	
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: No .env file found or error loading it: %v", err)
	}
	
	// Configure relay URL
	relayURL := "wss://relay.nostr.net"
	
	// Get alternative relay from environment if available
	if envRelay := os.Getenv("NOSTR_RELAY"); envRelay != "" {
		relayURL = envRelay
		log.Printf("Using relay from environment: %s", relayURL)
	}
	
	// Get DVM private key from environment
	privateKey := os.Getenv("DVM_PRIVATE_KEY")
	if privateKey == "" {
		log.Fatalf("DVM_PRIVATE_KEY environment variable not set. Please set it to a 64-character hex string.")
	}
	
	log.Printf("Using private key from environment (first 8 chars): %s...", privateKey[:8])
	log.Printf("Connecting to relay: %s", relayURL)
	
	dvmInstance, err := dvm.NewDvm(relayURL, privateKey)
	if err != nil {
		log.Fatalf("Failed to create DVM: %v", err)
	}

	pubkey := dvmInstance.GetPublicKey()
	log.Printf("========================================")
	log.Printf("DVM Successfully initialized")
	log.Printf("Public Key: %s", pubkey)
	log.Printf("Relay: %s", relayURL)
	log.Printf("========================================")
	log.Printf("To use this DVM in your CLI, add the following to your .env file:")
	log.Printf("DVM_PUBKEY=%s", pubkey)
	log.Printf("========================================")
	log.Printf("To restart this exact DVM instance later, ensure your .env contains:")
	log.Printf("DVM_PRIVATE_KEY=%s", privateKey)
	log.Printf("========================================")
	log.Printf("Ready to receive tweet fetch requests...")

	// Run the DVM - this will block until Stop() is called
	if err := dvmInstance.Run(); err != nil {
		log.Fatalf("DVM error: %v", err)
	}
}
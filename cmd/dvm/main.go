package main

import (
	"log"

	"bandita/dvm"
)

func main() {
	log.Println("Starting Nostr DVM...")
	
	relayURL := "wss://relay.damus.io"
	
	dvmInstance, err := dvm.NewDvm(relayURL)
	if err != nil {
		log.Fatalf("Failed to create DVM: %v", err)
	}

	log.Printf("DVM Public Key: %s", dvmInstance.GetPublicKey())
	log.Printf("Listening on relay: %s", relayURL)

	// Run the DVM - this will block until Stop() is called
	if err := dvmInstance.Run(); err != nil {
		log.Fatalf("DVM error: %v", err)
	}
}
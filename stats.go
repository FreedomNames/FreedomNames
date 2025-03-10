package main

import (
	"log"
	"time"
)

// TODO: Merged with existing GetNetworkPeers()?
func (freedomName *FreedomNameNode) connectionStats() int {
	peers := freedomName.kadDHT.Host().Network().Peers()
	numConnected := len(peers)
	return numConnected
}

func (freedomName *FreedomNameNode) statsLoop() {
	// Collect stats every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			// Collect peer stats
			peersConnected := freedomName.connectionStats()
			log.Printf("Stats: Number of peers connected: %d", peersConnected)

			// Collect bandwidth stats
			bandwidth := freedomName.bandwidthCounter.GetBandwidthTotals()
			log.Printf("Stats: Total bandwidth In: %v bytes/s", float64(bandwidth.RateIn))
			log.Printf("Stats: Total bandwidth Out: %v bytes/s", float64(bandwidth.RateOut))

			// freedomNameBw := freedomName.bandwidthCounter.GetBandwidthForProtocol("/freedomnames/1.0.0")
			// log.Printf("Stats: Bandwidth In: %v bytes/s", float64(freedomNameBw.RateIn))
			// log.Printf("Stats: Bandwidth Out: %v bytes/s", float64(freedomNameBw.RateOut))

			// TODO: add bandwidth for other protocols if needed (eg. for pub-sub, relay, etc.)
		case <-freedomName.ctx.Done():
			log.Println("Stopping stats service.")
			return
		}
	}
}

package main

import (
	"log"
	"time"
)

func (freedomName *FreedomNameNode) statsLoop() {
	// Collect stats every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			// Collect peer stats
			hosts := freedomName.GetNetworkPeers()
			peersConnected := len(hosts)
			log.Printf("Stats: Number of host peers connected: %d", peersConnected)

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

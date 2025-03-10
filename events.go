package main

import (
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
)

// eventLoop listens for events from the libp2p event bus and handles them accordingly.
// TODO: Move the output to a separate log file instead of stdout.
func (freedomName *FreedomNameNode) eventLoop() {
	// Subscribe to events we want to listen for
	sub, err := freedomName.kadDHT.Host().EventBus().Subscribe([]interface{}{
		new(event.EvtLocalProtocolsUpdated),
		new(event.EvtLocalAddressesUpdated),
		new(event.EvtLocalReachabilityChanged),
		new(event.EvtNATDeviceTypeChanged),
		new(event.EvtPeerProtocolsUpdated),
		new(event.EvtPeerIdentificationCompleted),
		new(event.EvtPeerIdentificationFailed),
		new(event.EvtPeerConnectednessChanged),
	})
	if err != nil {
		log.Printf("failed to subscribe to peer connectedness events: %s", err)
	}
	defer sub.Close()

	log.Println("Event listener started")

	for {
		select {
		case evt := <-sub.Out():
			go func(evt interface{}) {
				switch e := evt.(type) {
				case event.EvtLocalProtocolsUpdated:
					log.Printf("Event: 'Local protocols updated' - added: %+v, removed: %+v", e.Added, e.Removed)
				case event.EvtLocalAddressesUpdated:
					p2pAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", freedomName.kadDHT.Host().ID()))
					if err != nil {
						log.Printf("error computing p2p address: %s", err)
					} else {
						// Added
						for _, addr := range e.Current {
							addr := addr.Address.Encapsulate(p2pAddr)
							log.Printf("Event: 'Local address updated': %s", addr)
						}
						// Removed
						for _, addr := range e.Removed {
							addr := addr.Address.Encapsulate(p2pAddr)
							log.Printf("Event: 'Local address removed': %s", addr)
						}
					}
				case event.EvtLocalReachabilityChanged:
					log.Printf("Event: 'Local reachability changed': %+v", e.Reachability)
				case event.EvtNATDeviceTypeChanged:
					log.Printf("Event: 'NAT device type changed' - DeviceType %v, transport: %v", e.NatDeviceType.String(), e.TransportProtocol.String())
				case event.EvtPeerProtocolsUpdated:
					log.Printf("Event: 'Peer protocols updated' - added: %+v, removed: %+v, peer: %+v", e.Added, e.Removed, e.Peer)
				case event.EvtPeerIdentificationCompleted:
					log.Printf("Event: 'Peer identification completed' - %v", e.Peer)
				case event.EvtPeerIdentificationFailed:
					log.Printf("Event 'Peer identification failed' - peer: %v, reason: %v", e.Peer, e.Reason.Error())
				case event.EvtPeerConnectednessChanged:
					// Get the peer info
					peerInfo := freedomName.kadDHT.Host().Network().Peerstore().PeerInfo(e.Peer)
					// Get the peer ID
					peerID := peerInfo.ID
					// Get the peer protocols
					peerProtocols, err := freedomName.kadDHT.Host().Network().Peerstore().GetProtocols(peerID)
					if err != nil {
						log.Printf("Error getting peer protocols: %s", err)
					}
					// Get the peer addresses
					peerAddresses := freedomName.kadDHT.Host().Network().Peerstore().Addrs(peerID)
					log.Printf("Event: 'Peer connectedness change' - Peer %s (peerInfo: %+v) is now %s, protocols: %v, addresses: %v", peerID.String(), peerInfo, e.Connectedness, peerProtocols, peerAddresses)

					// Q: Do we really need to manage the peersstore ourselves?
					if e.Connectedness == network.NotConnected {
						freedomName.kadDHT.Host().Network().Peerstore().RemovePeer(peerID)
					}
				case *event.EvtNATDeviceTypeChanged:
					log.Printf("Event `NAT device type changed` - DeviceType %v, transport: %v", e.NatDeviceType.String(), e.TransportProtocol.String())
				default:
					log.Printf("Received unknown event (type: %T): %+v", e, e)
				}
			}(evt)
		case <-freedomName.ctx.Done():
			log.Println("Stopping event listener")
			return
		}
	}
}

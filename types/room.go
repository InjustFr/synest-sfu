package types

import (
	"encoding/json"
	"fmt"
	"time"

	"sync"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

type Room struct {
	id     string
	peers  map[string]*Peer
	tracks map[string]*webrtc.TrackLocalStaticRTP
	lock   sync.RWMutex
}

type RoomInterface interface {
	AddPeer(peer *Peer)
	RemovePeer(id string)
	AddTrack(track *webrtc.TrackRemote) *webrtc.TrackLocalStaticRTP
	RemoveTrack(track *webrtc.TrackLocalStaticRTP)
	SendOffer(offer webrtc.SessionDescription, peer *Peer)
	Signal()
}

func newRoom(id string) *Room {
	room := &Room{
		id:     id,
		peers:  map[string]*Peer{},
		tracks: map[string]*webrtc.TrackLocalStaticRTP{},
		lock:   sync.RWMutex{},
	}

	go func() {
		for range time.NewTicker(time.Second * 3).C {
			room.dispatchKeyFrame()
		}
	}()

	return room
}

func (room *Room) AddPeer(peer *Peer) {
	room.lock.Lock()
	defer room.lock.Unlock()

	room.peers[peer.id] = peer
}

func (room *Room) RemovePeer(id string) {
	room.lock.Lock()
	defer func() {
		room.lock.Unlock()
		room.Signal()
	}()

	if peer, ok := room.peers[id]; ok {
		peer.peerConnection.Close()

		delete(room.peers, id)
	}
}

func (room *Room) AddTrack(t *webrtc.TrackRemote) *webrtc.TrackLocalStaticRTP {
	room.lock.Lock()
	defer func() {
		room.lock.Unlock()
		room.Signal()
	}()

	trackLocal, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
	if err != nil {
		panic(err)
	}

	room.tracks[t.ID()] = trackLocal
	return trackLocal
}

func (room *Room) RemoveTrack(track *webrtc.TrackLocalStaticRTP) {
	room.lock.Lock()
	defer func() {
		room.lock.Unlock()
		room.Signal()
	}()

	delete(room.tracks, track.ID())
}

func (room *Room) Signal() {
	room.lock.Lock()
	defer func() {
		room.lock.Unlock()
		room.dispatchKeyFrame()
	}()

	attemptSync := func() (tryAgain bool) {
		for _, peer := range room.peers {
			if peer.peerConnection.ConnectionState() == webrtc.PeerConnectionStateClosed {
				room.RemovePeer(peer.id)
				return true // We modified the slice, start from the beginning
			}

			// map of sender we already are seanding, so we don't double send
			existingSenders := map[string]bool{}

			for _, sender := range peer.peerConnection.GetSenders() {
				if sender.Track() == nil {
					continue
				}

				existingSenders[sender.Track().ID()] = true

				// If we have a RTPSender that doesn't map to a existing track remove and signal
				if _, ok := room.tracks[sender.Track().ID()]; !ok {
					if err := peer.peerConnection.RemoveTrack(sender); err != nil {
						return true
					}
				}
			}

			// Don't receive videos we are sending, make sure we don't have loopback
			for _, receiver := range peer.peerConnection.GetReceivers() {
				if receiver.Track() == nil {
					continue
				}

				existingSenders[receiver.Track().ID()] = true
			}

			// Add all track we aren't sending yet to the PeerConnection
			for trackID := range room.tracks {
				if _, ok := existingSenders[trackID]; !ok {
					if _, err := peer.peerConnection.AddTrack(room.tracks[trackID]); err != nil {
						return true
					}
				}
			}

			offer, err := peer.CreateOffer()
			if err != nil {
				return true
			}

			err = room.SendOffer(offer, peer)
			if err != nil {
				return true
			}

		}

		return
	}

	for syncAttempt := 0; ; syncAttempt++ {
		if syncAttempt == 25 {
			// Release the lock and attempt a sync in 3 seconds. We might be blocking a RemoveTrack or AddTrack
			go func() {
				time.Sleep(time.Second * 3)
				room.Signal()
			}()
			return
		}

		if !attemptSync() {
			break
		}
	}
}

func (room *Room) dispatchKeyFrame() {
	room.lock.Lock()
	defer room.lock.Unlock()

	for _, peer := range room.peers {
		for _, receiver := range peer.peerConnection.GetReceivers() {
			if receiver.Track() == nil {
				continue
			}

			_ = peer.peerConnection.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{
					MediaSSRC: uint32(receiver.Track().SSRC()),
				},
			})
		}
	}
}

func (room *Room) SendOffer(offer webrtc.SessionDescription, peer *Peer) (err error) {
	offerString, err := json.Marshal(offer)
	if err != nil {
		fmt.Printf("Failed to marshal offer to json: %v", err)
		return err
	}

	fmt.Println("User:", peer.id, "; Room:", room.id, "; Offer sent")

	if err = peer.websocket.WriteJSON(&WsMessage{
		Type: "offer",
		Data: string(offerString),
	}); err != nil {
		return err
	}

	return nil
}

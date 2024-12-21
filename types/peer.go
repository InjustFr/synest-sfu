package types

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type Peer struct {
	id             string
	websocket      *ThreadSafeWriter
	peerConnection *webrtc.PeerConnection
	mutex          *sync.RWMutex
}

type PeerInterface interface {
	CreateOffer() (offer webrtc.SessionDescription)
	ReactOnAnswer(answer webrtc.SessionDescription)
	ReactOnCandidate(candidate webrtc.ICECandidateInit)
}

func newPeer(id string) *Peer {
	return &Peer{
		id:    id,
		mutex: &sync.RWMutex{},
	}
}

func (peer *Peer) CreateOffer() (offer webrtc.SessionDescription, err error) {
	peer.mutex.Lock()
	defer peer.mutex.Unlock()

	offer, err = peer.peerConnection.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	if err = peer.peerConnection.SetLocalDescription(offer); err != nil {
		return webrtc.SessionDescription{}, err
	}

	return offer, nil
}

func (peer *Peer) ReactOnAnswer(answer webrtc.SessionDescription) error {
	peer.mutex.Lock()
	defer peer.mutex.Unlock()

	if err := peer.peerConnection.SetRemoteDescription(answer); err != nil {
		return err
	}

	return nil
}

func (peer *Peer) ReactOnCandidate(candidate webrtc.ICECandidateInit) error {
	peer.mutex.Lock()
	defer peer.mutex.Unlock()

	if err := peer.peerConnection.AddICECandidate(candidate); err != nil {
		return err
	}

	return nil
}

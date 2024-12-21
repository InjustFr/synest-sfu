package types

import (
	"encoding/json"
	"fmt"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type Coordinator struct {
	rooms map[string]*Room
}

type CoordinatorInterface interface {
	CreateRoom(id string)
	RemoveRoom(id string)
	AddUserToRoom(id string, peerId string, conn *ThreadSafeWriter)
	RemoveUserFromRoom(id string, peerId string, conn *ThreadSafeWriter)
	HandleEvent(message WsMessage, conn *ThreadSafeWriter)
}

func NewCoordinator() *Coordinator {
	return &Coordinator{rooms: map[string]*Room{}}
}

func (coordinator *Coordinator) CreateRoom(id string) {
	coordinator.rooms[id] = newRoom(id)
}

func (coordinator *Coordinator) RemoveRoom(id string) {
	delete(coordinator.rooms, id)
}

func (coordinator *Coordinator) AddUserToRoom(id string, peerId string, conn *ThreadSafeWriter) {
	if _, ok := coordinator.rooms[id]; !ok {
		coordinator.CreateRoom(id)
		fmt.Println("Created room ", id)
	}

	if room, ok := coordinator.rooms[id]; ok {
		room.AddPeer(newPeer(peerId))
		if peer, ok := room.peers[peerId]; ok {
			peer.websocket = conn

			peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				fmt.Println("Failed to creates a PeerConnection: ", err)
				return
			}

			for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
				if _, err := peerConnection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
					Direction: webrtc.RTPTransceiverDirectionRecvonly,
				}); err != nil {
					fmt.Println("Failed to add transceiver: ", err)
					return
				}
			}

			peer.peerConnection = peerConnection

			peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
				if i == nil {
					return
				}
				// If you are serializing a candidate make sure to use ToJSON
				// Using Marshal will result in errors around `sdpMid`
				candidateString, err := json.Marshal(i.ToJSON())
				if err != nil {
					fmt.Println("Failed to marshal candidate to json: ", err)
					return
				}

				fmt.Println("Send candidate to client: ", candidateString)

				if writeErr := peer.websocket.WriteJSON(&WsMessage{
					Type: "candidate",
					Data: string(candidateString),
				}); writeErr != nil {
					fmt.Println("Failed to write JSON: ", writeErr)
				}
			})

			// If PeerConnection is closed remove it from global list
			peerConnection.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
				fmt.Println("Connection state change: ", p)

				switch p {
				case webrtc.PeerConnectionStateFailed:
					if err := peerConnection.Close(); err != nil {
						fmt.Println("Failed to close PeerConnection: ", err)
					}
				case webrtc.PeerConnectionStateClosed:
					room.Signal()
				default:
				}
			})

			peerConnection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
				fmt.Println("Got remote track: Kind=", t.Kind(), ", ID=", t.ID(), ", PayloadType=", t.PayloadType())

				// Create a track to fan out our incoming video to all peers
				trackLocal := room.AddTrack(t)
				defer room.RemoveTrack(trackLocal)

				buf := make([]byte, 1500)
				rtpPkt := &rtp.Packet{}

				for {
					i, _, err := t.Read(buf)
					if err != nil {
						return
					}

					if err = rtpPkt.Unmarshal(buf[:i]); err != nil {
						fmt.Println("Failed to unmarshal incoming RTP packet: ", err)
						return
					}

					rtpPkt.Extension = false
					rtpPkt.Extensions = nil

					if err = trackLocal.WriteRTP(rtpPkt); err != nil {
						return
					}
				}
			})

			peerConnection.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
				fmt.Println("ICE connection state changed: ", is)
			})

			room.Signal()
		}
	}

}

func (coordinator *Coordinator) RemoveUserFromRoom(id string, peerId string, conn *ThreadSafeWriter) {
	if room, ok := coordinator.rooms[id]; ok {
		if _, ok := room.peers[peerId]; ok {
			delete(room.peers, peerId)
		}

		if len(room.peers) <= 0 {
			coordinator.RemoveRoom(id)
		}
	}
}

func (coordinator *Coordinator) HandleEvent(message WsMessage, conn *ThreadSafeWriter) {
	data, ok := message.Data.(map[string]any)
	if !ok {
		fmt.Println("Could not parse message ", message)
		return
	}

	peerId := data["peerId"].(string)
	roomId := data["roomId"].(string)

	switch message.Type {
	case "join":
		fmt.Println("Got join")

		coordinator.AddUserToRoom(roomId, peerId, conn)
	case "leave":
		fmt.Println("Got leave")

		coordinator.RemoveUserFromRoom(roomId, peerId, conn)
	case "candidate":
		candidate := webrtc.ICECandidateInit{}
		if err := json.Unmarshal([]byte(data["candidate"].(string)), &candidate); err != nil {
			fmt.Println("Failed to unmarshal json to candidate: ", err)
			return
		}

		fmt.Println("Got candidate: ", candidate)

		if room, ok := coordinator.rooms[roomId]; ok {
			if peer, ok := room.peers[peerId]; ok {
				peer.ReactOnCandidate(candidate)
			}
		}
	case "answer":
		answer := webrtc.SessionDescription{}
		if err := json.Unmarshal([]byte(data["answer"].(string)), &answer); err != nil {
			fmt.Println("Failed to unmarshal json to answer: ", err)
			return
		}

		fmt.Println("Got answer: ", answer)

		if room, ok := coordinator.rooms[roomId]; ok {
			if peer, ok := room.peers[peerId]; ok {
				err := peer.ReactOnAnswer(answer)
				if err != nil {
					fmt.Println("Failed to react on anwser: ", err)
					return
				}
			}
		}
	default:
		fmt.Println("unknown message: ", message)
	}
}

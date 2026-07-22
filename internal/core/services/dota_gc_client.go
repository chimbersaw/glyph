package services

import (
	"context"
	"errors"
	"log"
	"sync"

	dotaproto "github.com/dotabuff/manta/dota"
	"github.com/sicdex/go-steam-ws"
	gcproto "github.com/sicdex/go-steam-ws/protocol/gamecoordinator"
	"google.golang.org/protobuf/proto"
)

const dotaAppID uint32 = 570

var errDotaNotReady = errors.New("dota GC client is not ready")

// dotaGCClient contains only the Game Coordinator messages this service uses.
// Keeping it local lets the Steam transport use WebSockets without coupling it
// to go-dota2's legacy TCP-only Steam client.
type dotaGCClient struct {
	steamClient    *steam.Client
	ready          chan struct{}
	readyOnce      sync.Once
	matchResponses chan *dotaproto.CMsgGCMatchDetailsResponse
}

func newDotaGCClient(client *steam.Client) *dotaGCClient {
	dc := &dotaGCClient{
		steamClient:    client,
		ready:          make(chan struct{}),
		matchResponses: make(chan *dotaproto.CMsgGCMatchDetailsResponse, 1),
	}
	client.GC.RegisterPacketHandler(dc)
	return dc
}

func (d *dotaGCClient) Ready() <-chan struct{} {
	return d.ready
}

func (d *dotaGCClient) SetPlaying(playing bool) {
	if playing {
		d.steamClient.GC.SetGamesPlayed(uint64(dotaAppID))
		return
	}
	d.steamClient.GC.SetGamesPlayed()
}

func (d *dotaGCClient) SayHello() {
	d.write(uint32(dotaproto.EGCBaseClientMsg_k_EMsgGCClientHello), &dotaproto.CMsgClientHello{
		ClientSessionNeed: proto.Uint32(104),
		ClientLauncher:    dotaproto.PartnerAccountType_PARTNER_NONE.Enum(),
		Engine:            dotaproto.ESourceEngine_k_ESE_Source2.Enum(),
	})
}

func (d *dotaGCClient) RequestMatchDetails(ctx context.Context, matchID uint64) (*dotaproto.CMsgGCMatchDetailsResponse, error) {
	select {
	case <-d.ready:
	default:
		return nil, errDotaNotReady
	}

	d.write(uint32(dotaproto.EDOTAGCMsg_k_EMsgGCMatchDetailsRequest), &dotaproto.CMsgGCMatchDetailsRequest{
		MatchId: proto.Uint64(matchID),
	})

	for {
		select {
		case response := <-d.matchResponses:
			if response.GetMatch() == nil || response.GetMatch().GetMatchId() == matchID {
				return response, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (d *dotaGCClient) HandleGCPacket(packet *gcproto.GCPacket) {
	if packet.AppId != dotaAppID {
		return
	}

	switch packet.MsgType {
	case uint32(dotaproto.EGCBaseClientMsg_k_EMsgGCClientWelcome):
		d.markReady()

	case uint32(dotaproto.EGCBaseClientMsg_k_EMsgGCClientConnectionStatus):
		status := new(dotaproto.CMsgConnectionStatus)
		if err := proto.Unmarshal(packet.Body, status); err != nil {
			log.Printf("Could not decode Dota GC connection status: %v", err)
			return
		}
		log.Println("New GC connection status:", status.GetStatus())
		if status.GetStatus() == dotaproto.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION {
			d.markReady()
		} else {
			d.SayHello()
		}

	case uint32(dotaproto.EDOTAGCMsg_k_EMsgGCMatchDetailsResponse):
		response := new(dotaproto.CMsgGCMatchDetailsResponse)
		if err := proto.Unmarshal(packet.Body, response); err != nil {
			log.Printf("Could not decode Dota match details: %v", err)
			return
		}
		select {
		case d.matchResponses <- response:
		default:
			log.Println("Dropping unexpected Dota match details response")
		}

	case uint32(dotaproto.EGCBaseClientMsg_k_EMsgGCPingRequest):
		d.write(uint32(dotaproto.EGCBaseClientMsg_k_EMsgGCPingResponse), new(dotaproto.CMsgGCClientPing))
	}
}

func (d *dotaGCClient) markReady() {
	d.readyOnce.Do(func() {
		close(d.ready)
	})
}

func (d *dotaGCClient) write(messageType uint32, body proto.Message) {
	d.steamClient.GC.Write(gcproto.NewGCMsgProtobuf(dotaAppID, messageType, body))
}

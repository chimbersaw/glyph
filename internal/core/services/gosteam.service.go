package services

import (
	"context"
	"errors"
	"fmt"
	"go-glyph/internal/core/dtos"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sicdex/go-steam-ws"
	steamproto "github.com/sicdex/go-steam-ws/protocol"
	steampb "github.com/sicdex/go-steam-ws/protocol/protobuf"
	"github.com/sicdex/go-steam-ws/protocol/steamlang"
)

type GoSteamService struct {
	steamClient       *steam.Client
	dotaClient        *dotaGCClient
	steamLoginInfos   []*steam.LogOnDetails
	counter           uint
	lock              sync.Mutex
	keepAliveTicker   *time.Ticker
	keepAliveTickerMu sync.Mutex
	keepAliveRequests chan struct{}
}

func NewGoSteamService(usernames, passwords string) *GoSteamService {
	var steamLoginInfos []*steam.LogOnDetails
	u := strings.Split(usernames, " ")
	p := strings.Split(passwords, " ")
	for i := 0; i < len(u); i++ {
		steamLoginInfos = append(steamLoginInfos, &steam.LogOnDetails{
			Username: u[i],
			Password: p[i],
		})
	}
	steamLoginInfo := steamLoginInfos[0]

	service := &GoSteamService{
		steamLoginInfos: steamLoginInfos,
		counter:         0,
		lock:            sync.Mutex{},
	}

	sc, dc, err := initDotaClient(steamLoginInfo, service.onDisconnected)
	if err != nil {
		log.Fatal(err)
	}

	service.steamClient = sc
	service.dotaClient = dc
	service.startKeepAlive()

	return service
}

func (s *GoSteamService) GetMatchDetails(matchID int) (dtos.Match, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		match, err := s.getMatchFromSteam(matchID)
		if err == nil {
			return match, nil
		}

		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errDotaNotReady) {
			return dtos.Match{}, err
		}

		log.Printf("Error connecting to dota: %v, changing client...", err)
		err = s.changeClient()
		time.Sleep(1 * time.Second)
		if err != nil {
			log.Println("Error changing client:", err)
		}
	}

	log.Printf("Could not get match details after %d attempts", maxRetries)
	return dtos.Match{}, UserFacingError{Code: fiber.StatusServiceUnavailable, Message: "Error connecting to dota servers :( Please try again later"}
}

func (s *GoSteamService) getMatchFromSteam(matchID int) (dtos.Match, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	matchDetails, err := s.dotaClient.RequestMatchDetails(ctx, uint64(matchID))
	if err != nil {
		return dtos.Match{}, err
	}

	return dtos.Match{
		ID:         matchID,
		Cluster:    int(matchDetails.Match.GetCluster()),
		ReplaySalt: int(matchDetails.Match.GetReplaySalt()),
	}, nil
}

func (s *GoSteamService) changeClient() error {
	s.gracefulDisconnect()

	s.counter++
	if s.counter >= uint(len(s.steamLoginInfos)) {
		s.counter = 0
	}
	loginInfo := s.steamLoginInfos[s.counter]

	log.Printf("Switching to client `%s`", loginInfo.Username)
	sc, dc, err := initDotaClient(loginInfo, s.onDisconnected)
	if err != nil {
		return err
	}

	s.steamClient = sc
	s.dotaClient = dc

	return nil
}

func (s *GoSteamService) startKeepAlive() {
	// Keep-alive every 1 hour to reinitialize the client if it's not ready
	ticker := time.NewTicker(1 * time.Hour)
	s.keepAliveTickerMu.Lock()
	s.keepAliveTicker = ticker
	s.keepAliveTickerMu.Unlock()

	s.keepAliveRequests = make(chan struct{}, 1)

	go func() {
		for range s.keepAliveRequests {
			s.runKeepAlive()
		}
	}()

	go func() {
		defer ticker.Stop()
		for range ticker.C {
			s.requestKeepAlive()
		}
	}()
}

func (s *GoSteamService) requestKeepAlive() {
	if s.keepAliveRequests == nil {
		return
	}

	// Non-blocking enqueue
	select {
	case s.keepAliveRequests <- struct{}{}:
	default:
	}
}

func (s *GoSteamService) runKeepAlive() {
	if !s.steamClient.Connected() {
		log.Println("Steam client not connected, running keep-alive...")
	}

	if _, err := s.GetMatchDetails(239); err != nil {
		log.Printf("Keep-alive error: %v", err)
	} else {
		log.Println("Keep-alive success")
	}
}

func (s *GoSteamService) onDisconnected() {
	go func() {
		log.Println("Requesting keep-alive after disconnect in 5s")
		time.Sleep(5 * time.Second)
		if s.steamClient == nil {
			return
		}

		s.keepAliveTickerMu.Lock()
		if s.keepAliveTicker != nil {
			s.keepAliveTicker.Reset(1 * time.Hour)
		}
		s.keepAliveTickerMu.Unlock()

		s.requestKeepAlive()
	}()
}

func initDotaClient(steamLoginInfo *steam.LogOnDetails, onDisconnected func()) (*steam.Client, *dotaGCClient, error) {
	sc := steam.NewClient()
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	sc.Dialer = dialer.Dial
	dc := newDotaGCClient(sc)
	connectionErrors := make(chan error, 1)
	helloRetryCtx, stopHelloRetry := context.WithCancel(context.Background())
	defer stopHelloRetry()
	reportConnectionError := func(err error) {
		select {
		case connectionErrors <- err:
		default:
		}
	}

	server, err := connectSteamWebSocket(sc)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("Steam client connected to websocket server %s\n", server)

	go func() {
		for event := range sc.Events() {
			switch e := event.(type) {

			case *steam.ConnectedEvent:
				log.Println("Connected, attempting to log in...")
				go func() {
					authResult, err := sc.Authentication.LogOnWithCredentials(steamLoginInfo.Username, steamLoginInfo.Password)
					if err != nil {
						reportConnectionError(fmt.Errorf("steam credential authentication failed: %w", err))
						return
					}
					sc.Auth.LogOn(&steam.LogOnDetails{
						Username:    authResult.AccountName,
						AccessToken: authResult.RefreshToken,
					})
				}()

			case *steam.LoggedOnEvent:
				if e.Result != steamlang.EResult_OK {
					reportConnectionError(fmt.Errorf("steam logon failed: %v", e.Result))
					continue
				}
				log.Println("Logged in to Steam")
				sc.Social.SetPersonaState(steamlang.EPersonaState_Online)
				dc.SetPlaying(true)
				dc.SayHello()
				go retryDotaHelloUntilReady(helloRetryCtx, dc)

			case *steam.LogOnFailedEvent:
				reportConnectionError(fmt.Errorf("steam logon failed: %v", e.Result))

			case *steam.AccountInfoEvent:
				log.Println(e.AccountFlags)

			case *steam.DisconnectedEvent:
				stopHelloRetry()
				log.Printf("Disconnected from Steam :(")
				if onDisconnected != nil {
					onDisconnected()
				}

			case steam.FatalErrorEvent:
				reportConnectionError(fmt.Errorf("steam fatal error: %w", error(e)))

			case error:
				reportConnectionError(e)
			}
		}
	}()

	// Credential auth is an HTTP polling flow, so allow it more time than the
	// old password-in-CM handshake needed.
	select {
	case <-dc.Ready():
		log.Println("Dota client is ready with a GC session.")
		return sc, dc, nil
	case err := <-connectionErrors:
		stopHelloRetry()
		sc.Disconnect()
		return nil, nil, err
	case <-time.After(30 * time.Second):
		stopHelloRetry()
		sc.Disconnect()
		return nil, nil, errors.New("timeout waiting for Dota client to connect")
	}
}

func retryDotaHelloUntilReady(ctx context.Context, dc *dotaGCClient) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-dc.Ready():
			return
		case <-ticker.C:
			dc.SayHello()
		}
	}
}

func connectSteamWebSocket(sc *steam.Client) (string, error) {
	servers, err := steam.FetchCMListForConnect(0)
	if err != nil {
		return "", fmt.Errorf("fetch Steam CM list: %w", err)
	}

	websocketServers := steam.FilterByType(servers, "websockets")
	secureServers := make([]steam.CMServer, 0, len(websocketServers))
	for _, server := range websocketServers {
		if strings.HasSuffix(server.Endpoint, ":443") {
			secureServers = append(secureServers, server)
		}
	}
	if len(secureServers) == 0 {
		return "", errors.New("steam directory returned no secure websocket servers")
	}
	sort.Slice(secureServers, func(i, j int) bool {
		return secureServers[i].WeightedLoad < secureServers[j].WeightedLoad
	})

	const maxAttempts = 5
	var lastErr error
	for i, server := range secureServers {
		if i >= maxAttempts {
			break
		}
		if err := sc.ConnectToWebSocket(server.Endpoint); err == nil {
			return server.Endpoint, nil
		} else {
			lastErr = err
			log.Printf("Steam websocket %s is unavailable: %v", server.Endpoint, err)
		}

		// ConnectToWebSocket reports the same dial failure as a fatal event.
		// Consume it here so a later successful attempt is not mistaken for a
		// dropped established connection by the main event loop.
		select {
		case <-sc.Events():
		default:
		}
	}

	return "", fmt.Errorf("connect to Steam websocket after %d attempts: %w", maxAttempts, lastErr)
}

func (s *GoSteamService) gracefulDisconnect() {
	if s.steamClient == nil {
		return
	}

	if s.dotaClient != nil {
		s.dotaClient.SetPlaying(false)
	}

	if s.steamClient.Connected() {
		s.steamClient.Social.SetPersonaState(steamlang.EPersonaState_Offline)
		s.steamClient.Write(steamproto.NewClientMsgProtobuf(steamlang.EMsg_ClientLogOff, new(steampb.CMsgClientLogOff)))
		time.Sleep(250 * time.Millisecond)
	}
	s.steamClient.Disconnect()
}

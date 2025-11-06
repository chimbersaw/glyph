package services

import (
	"context"
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	"github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/steamlang"
	"github.com/sirupsen/logrus"
	"go-glyph/internal/core/dtos"
	"log"
	"strings"
	"sync"
	"time"
)

type GoSteamService struct {
	steamClient       *steam.Client
	dotaClient        *dota2.Dota2
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

		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, dota2.ErrNotReady) {
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
	s.steamClient.Disconnect()
	time.Sleep(3 * time.Second)

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

func initDotaClient(steamLoginInfo *steam.LogOnDetails, onDisconnected func()) (*steam.Client, *dota2.Dota2, error) {
	sc := steam.NewClient()
	if err := steam.InitializeSteamDirectory(); err != nil {
		log.Println("Failed to initialize Steam directory:", err)
		return nil, nil, err
	}

	dc := dota2.New(sc, logrus.New())

	// Channel to signal that the Dota client has a valid GC session
	ready := make(chan struct{}, 1)

	go func() {
		for event := range sc.Events() {
			switch e := event.(type) {

			case *steam.ConnectedEvent:
				log.Println("Connected, attempting to log in...")
				sc.Auth.LogOn(steamLoginInfo)

			case *steam.LoggedOnEvent:
				log.Println("Logging in...")
				sc.Social.SetPersonaState(steamlang.EPersonaState_Online)
				time.Sleep(3 * time.Second)
				dc.SetPlaying(true)
				time.Sleep(3 * time.Second)
				dc.SayHello()

			case *steam.LogOnFailedEvent:
				log.Printf("LogOn failed. Reason: %v\n", e.Result)

			case *events.GCConnectionStatusChanged:
				log.Println("New GC connection status:", e.NewState)
				if e.NewState == protocol.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION {
					// Non-blocking send
					select {
					case ready <- struct{}{}:
					default:
					}
				} else {
					dc.SayHello()
				}

			case *steam.AccountInfoEvent:
				log.Println(e.AccountFlags)

			case *steam.DisconnectedEvent:
				log.Printf("Disconnected from Steam :(")
				if onDisconnected != nil {
					onDisconnected()
				}

			case steam.FatalErrorEvent:
				log.Print(e)

			case error:
				log.Print(e)
			}
		}
	}()

	server := sc.Connect()
	log.Printf("Steam client connected to server %s\n", server.String())

	// Wait until the Dota client has a GC session or until 15 seconds pass.
	select {
	case <-ready:
		log.Println("Dota client is ready with a GC session.")
		return sc, dc, nil
	case <-time.After(15 * time.Second):
		return nil, nil, errors.New("timeout waiting for Dota client to connect")
	}
}

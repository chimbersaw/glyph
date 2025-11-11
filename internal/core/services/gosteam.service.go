package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"go-glyph/internal/core/dtos"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	"github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/steamlang"
	"github.com/sirupsen/logrus"
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
	//service.startKeepAlive()

	return service
}

func (s *GoSteamService) GetMatchDetails(matchID int) (dtos.Match, error) {
	err := s.PrintAccountPartyStats(uint32(matchID))
	if err != nil {
		fmt.Println(err)
		return dtos.Match{}, err
	}
	return dtos.Match{}, UserFacingError{Code: fiber.StatusServiceUnavailable, Message: "LOL stfu"}

	//s.lock.Lock()
	//defer s.lock.Unlock()
	//
	//maxRetries := 3
	//for i := 0; i < maxRetries; i++ {
	//	match, err := s.getMatchFromSteam(matchID)
	//	if err == nil {
	//		return match, nil
	//	}
	//
	//	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, dota2.ErrNotReady) {
	//		return dtos.Match{}, err
	//	}
	//
	//	log.Printf("Error connecting to dota: %v, changing client...", err)
	//	err = s.changeClient()
	//	time.Sleep(1 * time.Second)
	//	if err != nil {
	//		log.Println("Error changing client:", err)
	//	}
	//}
	//
	//log.Printf("Could not get match details after %d attempts", maxRetries)
	//return dtos.Match{}, UserFacingError{Code: fiber.StatusServiceUnavailable, Message: "Error connecting to dota servers :( Please try again later"}
}

func (s *GoSteamService) PrintAccountPartyStats(accountID uint32) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	fileName := fmt.Sprintf("match_ids_%d.txt", accountID)
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open match id file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("Error closing match id file: %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	matchIDs := make([]uint64, 0)
	seenMatches := make(map[uint64]struct{})

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		id, parseErr := strconv.ParseUint(line, 10, 64)
		if parseErr != nil {
			log.Printf("Skipping invalid match id %q: %v", line, parseErr)
			continue
		}

		if _, exists := seenMatches[id]; exists {
			continue
		}
		seenMatches[id] = struct{}{}
		matchIDs = append(matchIDs, id)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read match id file: %w", err)
	}

	if len(matchIDs) == 0 {
		return fmt.Errorf("no match ids found for account %d in %s", accountID, fileName)
	}

	type partyStat struct {
		count int
		name  string
	}

	progressFile := fmt.Sprintf("saved_progress_%d.txt", accountID)
	stats := make(map[uint32]*partyStat)
	processedCount := 0

	if progressHandle, progressErr := os.Open(progressFile); progressErr == nil {
		func() {
			defer func() {
				if closeErr := progressHandle.Close(); closeErr != nil {
					log.Printf("Error closing progress file: %v", closeErr)
				}
			}()

			progressScanner := bufio.NewScanner(progressHandle)
			if progressScanner.Scan() {
				text := strings.TrimSpace(progressScanner.Text())
				if text != "" {
					if val, convErr := strconv.Atoi(text); convErr == nil {
						processedCount = val
					} else {
						log.Printf("Invalid processed count in %s: %v", progressFile, convErr)
					}
				}
			}

			for progressScanner.Scan() {
				line := strings.TrimSpace(progressScanner.Text())
				if line == "" {
					continue
				}

				if !strings.HasPrefix(line, "#") {
					log.Printf("Skipping malformed progress line: %s", line)
					continue
				}

				content := strings.TrimPrefix(line, "#")
				firstComma := strings.Index(content, ",")
				if firstComma == -1 {
					log.Printf("Skipping malformed progress line: %s", line)
					continue
				}

				countStr := strings.TrimSpace(content[:firstComma])
				rest := content[firstComma+1:]
				secondComma := strings.Index(rest, ",")
				if secondComma == -1 {
					log.Printf("Skipping malformed progress line: %s", line)
					continue
				}

				accountStr := strings.TrimSpace(rest[:secondComma])
				name := strings.TrimSpace(rest[secondComma+1:])

				countVal, countErr := strconv.Atoi(countStr)
				if countErr != nil {
					log.Printf("Skipping progress entry with invalid count %q: %v", countStr, countErr)
					continue
				}

				accountVal, accountErr := strconv.ParseUint(accountStr, 10, 32)
				if accountErr != nil {
					log.Printf("Skipping progress entry with invalid account id %q: %v", accountStr, accountErr)
					continue
				}

				stats[uint32(accountVal)] = &partyStat{
					count: countVal,
					name:  name,
				}
			}

			if progressErr := progressScanner.Err(); progressErr != nil {
				log.Printf("Error reading progress file %s: %v", progressFile, progressErr)
			}
		}()

		if processedCount > len(matchIDs) {
			processedCount = len(matchIDs)
		}
		if processedCount > 0 {
			matchIDs = matchIDs[processedCount:]
			log.Printf("Resuming from %d processed matches; %d remain", processedCount, len(matchIDs))
		}
	} else if !os.IsNotExist(progressErr) {
		return fmt.Errorf("failed to open saved progress file: %w", progressErr)
	}

	parsedMatches := 0
	consecutiveFailures := 0

	for x, id := range matchIDs {
		var (
			matchDetails *protocol.CMsgGCMatchDetailsResponse
			err          error
		)

		for attempt := 0; attempt < 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			matchDetails, err = s.dotaClient.RequestMatchDetails(ctx, id)
			cancel()

			if err == nil {
				break
			}

			log.Printf("Error retrieving match %d (attempt %d/3): %v", id, attempt+1, err)
			if changeErr := s.changeClient(); changeErr != nil {
				log.Printf("Error changing client: %v", changeErr)
			}
			time.Sleep(1 * time.Second)
		}

		match := matchDetails.GetMatch()
		if err != nil || matchDetails == nil || match == nil {
			consecutiveFailures++
			if consecutiveFailures >= 5 {
				log.Printf("Stopping match parsing after %d consecutive failures", consecutiveFailures)
				break
			}
			continue
		}

		consecutiveFailures = 0
		parsedMatches++

		var (
			targetPartyID uint64
			found         bool
		)
		for _, player := range match.GetPlayers() {
			if player.GetAccountId() == accountID {
				targetPartyID = player.GetPartyId()
				found = true
				break
			}
		}

		if !found || targetPartyID == 0 {
			continue
		}

		for _, player := range match.GetPlayers() {
			if player.GetAccountId() == accountID {
				continue
			}

			if player.GetPartyId() != targetPartyID {
				continue
			}

			companionID := player.GetAccountId()
			if companionID == 0 {
				continue
			}

			stat := stats[companionID]
			if stat == nil {
				stat = &partyStat{}
				stats[companionID] = stat
			}
			stat.count++
			if name := player.GetPlayerName(); name != "" && stat.name == "" {
				stat.name = name
			}
		}

		totalProcessed := processedCount + x + 1
		if totalProcessed%50 == 0 {
			log.Printf("Processed %d matches...", totalProcessed)
		}

		//time.Sleep(7 * time.Second)
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("Matches parsed successfully: %d\n", parsedMatches)
	fmt.Printf("Matches parsed overall: %d\n", processedCount+parsedMatches)

	results := make([]struct {
		id    uint32
		name  string
		count int
	}, 0, len(stats))
	for id, stat := range stats {
		results = append(results, struct {
			id    uint32
			name  string
			count int
		}{id: id, name: stat.name, count: stat.count})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].count == results[j].count {
			return results[i].id < results[j].id
		}
		return results[i].count > results[j].count
	})

	for _, result := range results {
		name := result.name
		if name == "" {
			name = "(unknown)"
		}
		fmt.Printf("#%d, %d, %s\n", result.count, result.id, name)
	}

	return nil
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
		//log.Println("Requesting keep-alive after disconnect in 5s")
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

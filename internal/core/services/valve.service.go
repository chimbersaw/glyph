package services

import (
	"bufio"
	"compress/bzip2"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"go-glyph/internal/core/dtos"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	baseReplayURL = "http://replay%d.valve.net/570/%d_%d.dem.bz2"
	demosPath     = "demos"
)

type ValveService struct {
	client http.Client
}

func NewValveService() *ValveService {
	return &ValveService{client: http.Client{}}
}

func (s ValveService) RetrieveFile(match dtos.Match) error {
	if match.Cluster == 0 {
		return UserFacingError{Code: fiber.StatusNotFound, Message: "Match id is invalid"}
	}

	startTime := time.Now()

	url := fmt.Sprintf(baseReplayURL, match.Cluster, match.ID, match.ReplaySalt)
	response, err := s.client.Get(url)
	if err != nil {
		return GETError{url: url, error: err}
	}
	if response.StatusCode != 200 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			log.Printf("HTTP body read error to %s with status code %d", url, response.StatusCode)
			return ReadResponseBodyError{err}
		}

		bodyStr := string(body)
		if strings.Contains(bodyStr, "Error: 2010") {
			log.Printf("HTTP error to %s with status code %d and body: %s", url, response.StatusCode, bodyStr)
			return UserFacingError{Code: fiber.StatusNotFound, Message: "Match is too new or too old :("}
		}

		return HTTPError{url: url, statusCode: response.StatusCode, response: bodyStr}
	}
	defer response.Body.Close()

	if _, err := os.Stat(demosPath); os.IsNotExist(err) {
		err := os.Mkdir(demosPath, os.ModePerm)
		if err != nil {
			return FolderCreationError{foldername: demosPath, error: err}
		}
	}
	filename := fmt.Sprintf("%s/%d.dem", demosPath, match.ID)

	// Check if file exists, and if it does, remove it to ensure a fresh download.
	if _, err := os.Stat(filename); err == nil {
		if err := os.Remove(filename); err != nil {
			return RemoveFileError{filename: filename, error: err}
		}
	}

	// Create a new file to save the decompressed content
	file, err := os.Create(filename)
	if err != nil {
		return FileCreationError{filename: filename, error: err}
	}
	defer file.Close()

	bufferedWriter := bufio.NewWriter(file)
	defer bufferedWriter.Flush()

	// Create a bzip2 reader to decompress the content
	reader := bzip2.NewReader(response.Body)

	// Copy the decompressed content to the file
	_, err = io.Copy(bufferedWriter, reader)
	if err != nil {
		_ = bufferedWriter.Flush()
		_ = file.Close()
		_ = os.Remove(filename)
		return CopyError{err}
	}

	// Log the time it took to download and decompress
	duration := time.Since(startTime)
	log.Printf("Downloaded and decompressed file %s in %v", filename, duration)

	// Decompression completed
	return nil
}

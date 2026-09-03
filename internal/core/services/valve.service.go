package services

import (
	"bufio"
	"compress/bzip2"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/klauspost/compress/zstd"
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

	reader, closeReader, err := newReplayReader(response.Body)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(filename)
		return CopyError{err}
	}
	defer closeReader()

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

func newReplayReader(source io.Reader) (io.Reader, func(), error) {
	bufferedReader := bufio.NewReader(source)
	header, err := bufferedReader.Peek(4)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read replay compression header: %w", err)
	}

	if header[0] == 'B' && header[1] == 'Z' && header[2] == 'h' && header[3] >= '1' && header[3] <= '9' {
		return bzip2.NewReader(bufferedReader), func() {}, nil
	}

	if header[0] == 0x28 && header[1] == 0xb5 && header[2] == 0x2f && header[3] == 0xfd {
		reader, err := zstd.NewReader(bufferedReader)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot create zstd replay reader: %w", err)
		}
		return reader, reader.Close, nil
	}

	return nil, nil, fmt.Errorf("unsupported replay compression header: % x", header)
}

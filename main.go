package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	nurl "net/url"
	"os"
	"strings"
	"time"

	"github.com/markusmobius/go-trafilatura"
)

type scrapeRequest struct {
	URL string `json:"url"`
}

type scrapeResponse struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/scrape", scrapeHandler)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on :%s", port)
	log.Fatal(server.ListenAndServe())
}

func scrapeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	var req scrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
		return
	}

	parsedURL, err := validateURL(req.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	resp, err := httpClient.Get(parsedURL.String())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponse{Error: fmt.Sprintf("failed to fetch URL: %v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, errorResponse{Error: fmt.Sprintf("upstream returned status %d", resp.StatusCode)})
		return
	}

	result, err := trafilatura.Extract(resp.Body, trafilatura.Options{OriginalURL: parsedURL})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponse{Error: fmt.Sprintf("failed to extract content: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, scrapeResponse{
		URL:     parsedURL.String(),
		Title:   result.Metadata.Title,
		Content: strings.TrimSpace(result.ContentText),
	})
}

func validateURL(raw string) (*nurl.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("url is required")
	}

	parsedURL, err := nurl.ParseRequestURI(raw)
	if err != nil {
		return nil, errors.New("url must be a valid absolute URL")
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("url must use http or https")
	}

	return parsedURL, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

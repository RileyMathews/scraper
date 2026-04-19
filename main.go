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

	"github.com/go-shiori/dom"
	"github.com/markusmobius/go-trafilatura"
)

type scrapeRequest struct {
	URL string `json:"url"`
}

type scrapeResponse struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	ContentHTML string `json:"content_html,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type openAPIDocument struct {
	OpenAPI    string                     `json:"openapi"`
	Info       openAPIInfo                `json:"info"`
	Servers    []openAPIServer            `json:"servers,omitempty"`
	Paths      map[string]openAPIPathItem `json:"paths"`
	Components openAPIComponents          `json:"components"`
}

type openAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type openAPIServer struct {
	URL string `json:"url"`
}

type openAPIPathItem struct {
	Post *openAPIOperation `json:"post,omitempty"`
}

type openAPIOperation struct {
	OperationID string                          `json:"operationId"`
	Summary     string                          `json:"summary,omitempty"`
	Description string                          `json:"description,omitempty"`
	RequestBody *openAPIRequestBody             `json:"requestBody,omitempty"`
	Responses   map[string]openAPIResponseEntry `json:"responses"`
}

type openAPIRequestBody struct {
	Required bool                           `json:"required"`
	Content  map[string]openAPIMediaTypeRef `json:"content"`
}

type openAPIResponseEntry struct {
	Description string                         `json:"description"`
	Content     map[string]openAPIMediaTypeRef `json:"content,omitempty"`
}

type openAPIMediaTypeRef struct {
	Schema   openAPISchemaRef        `json:"schema"`
	Examples map[string]openAPIValue `json:"examples,omitempty"`
}

type openAPIValue struct {
	Value any `json:"value"`
}

type openAPIComponents struct {
	Schemas map[string]openAPISchema `json:"schemas"`
}

type openAPISchemaRef struct {
	Ref string `json:"$ref,omitempty"`
}

type openAPISchema struct {
	Type                 string                   `json:"type,omitempty"`
	Description          string                   `json:"description,omitempty"`
	Properties           map[string]openAPISchema `json:"properties,omitempty"`
	Required             []string                 `json:"required,omitempty"`
	AdditionalProperties any                      `json:"additionalProperties,omitempty"`
	Example              any                      `json:"example,omitempty"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/scrape", scrapeHandler)
	mux.HandleFunc("/openapi.json", openAPIHandler)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on :%s", port)
	log.Fatal(server.ListenAndServe())
}

func openAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, buildOpenAPIDocument(r))
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
		URL:         parsedURL.String(),
		Title:       result.Metadata.Title,
		Content:     strings.TrimSpace(result.ContentText),
		ContentHTML: strings.TrimSpace(dom.OuterHTML(result.ContentNode)),
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
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

func buildOpenAPIDocument(r *http.Request) openAPIDocument {
	return openAPIDocument{
		OpenAPI: "3.1.0",
		Info: openAPIInfo{
			Title:       "Scraper API",
			Version:     "1.0.0",
			Description: "Extracts article-like content from a URL and returns both plain text and HTML.",
		},
		Servers: []openAPIServer{{URL: serverURL(r)}},
		Paths: map[string]openAPIPathItem{
			"/scrape": {
				Post: &openAPIOperation{
					OperationID: "scrapeUrl",
					Summary:     "Scrape a URL",
					Description: "Fetch a remote HTTP or HTTPS URL, extract the main content, and return plain text plus HTML.",
					RequestBody: &openAPIRequestBody{
						Required: true,
						Content: map[string]openAPIMediaTypeRef{
							"application/json": {
								Schema: openAPISchemaRef{Ref: "#/components/schemas/ScrapeRequest"},
								Examples: map[string]openAPIValue{
									"article": {
										Value: scrapeRequest{URL: "https://example.com/article"},
									},
								},
							},
						},
					},
					Responses: map[string]openAPIResponseEntry{
						"200": {
							Description: "Content extracted successfully.",
							Content: map[string]openAPIMediaTypeRef{
								"application/json": {
									Schema: openAPISchemaRef{Ref: "#/components/schemas/ScrapeResponse"},
									Examples: map[string]openAPIValue{
										"success": {
											Value: scrapeResponse{
												URL:         "https://example.com/article",
												Title:       "Example Article",
												Content:     "Plain text content",
												ContentHTML: "<article><p>Plain text content</p></article>",
											},
										},
									},
								},
							},
						},
						"400": errorResponseEntry("The request body or URL was invalid.", "invalid_request", errorResponse{Error: "url must be a valid absolute URL"}),
						"405": errorResponseEntry("The HTTP method is not supported for this endpoint.", "method_not_allowed", errorResponse{Error: "method not allowed"}),
						"502": errorResponseEntry("Fetching or extracting content from the upstream URL failed.", "upstream_error", errorResponse{Error: "upstream returned status 502"}),
					},
				},
			},
		},
		Components: openAPIComponents{
			Schemas: map[string]openAPISchema{
				"ScrapeRequest": {
					Type:        "object",
					Description: "Request body for scraping a remote page.",
					Required:    []string{"url"},
					Properties: map[string]openAPISchema{
						"url": {
							Type:        "string",
							Description: "Absolute HTTP or HTTPS URL to fetch and extract content from.",
							Example:     "https://example.com/article",
						},
					},
					AdditionalProperties: false,
				},
				"ScrapeResponse": {
					Type:        "object",
					Description: "Extracted content for the requested URL.",
					Required:    []string{"url", "content"},
					Properties: map[string]openAPISchema{
						"url": {
							Type:        "string",
							Description: "Normalized URL that was fetched.",
							Example:     "https://example.com/article",
						},
						"title": {
							Type:        "string",
							Description: "Extracted document title when available.",
							Example:     "Example Article",
						},
						"content": {
							Type:        "string",
							Description: "Plain text representation of the extracted main content.",
							Example:     "Plain text content",
						},
						"content_html": {
							Type:        "string",
							Description: "HTML representation of the extracted main content when available.",
							Example:     "<article><p>Plain text content</p></article>",
						},
					},
					AdditionalProperties: false,
				},
				"ErrorResponse": {
					Type:        "object",
					Description: "Error payload returned when the request cannot be completed.",
					Required:    []string{"error"},
					Properties: map[string]openAPISchema{
						"error": {
							Type:        "string",
							Description: "Human-readable error message.",
							Example:     "url is required",
						},
					},
					AdditionalProperties: false,
				},
			},
		},
	}
}

func errorResponseEntry(description, exampleName string, example errorResponse) openAPIResponseEntry {
	return openAPIResponseEntry{
		Description: description,
		Content: map[string]openAPIMediaTypeRef{
			"application/json": {
				Schema: openAPISchemaRef{Ref: "#/components/schemas/ErrorResponse"},
				Examples: map[string]openAPIValue{
					exampleName: {
						Value: example,
					},
				},
			},
		},
	}
}

func serverURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost"
		if port := os.Getenv("PORT"); port != "" {
			host += ":" + port
		}
	}

	if !strings.Contains(host, ":") {
		defaultPort := "80"
		if scheme == "https" {
			defaultPort = "443"
		}
		if serverPort := strings.TrimSpace(r.Header.Get("X-Forwarded-Port")); serverPort != "" && serverPort != defaultPort {
			host += ":" + serverPort
		}
	}

	return scheme + "://" + host
}

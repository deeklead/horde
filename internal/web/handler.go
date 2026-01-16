package web

import (
	"html/template"
	"net/http"
)

// RaidFetcher defines the interface for fetching raid data.
type RaidFetcher interface {
	FetchRaids() ([]RaidRow, error)
	FetchMergeQueue() ([]MergeQueueRow, error)
	FetchRaiders() ([]RaiderRow, error)
}

// RaidHandler handles HTTP requests for the raid warmap.
type RaidHandler struct {
	fetcher  RaidFetcher
	template *template.Template
}

// NewRaidHandler creates a new raid handler with the given fetcher.
func NewRaidHandler(fetcher RaidFetcher) (*RaidHandler, error) {
	tmpl, err := LoadTemplates()
	if err != nil {
		return nil, err
	}

	return &RaidHandler{
		fetcher:  fetcher,
		template: tmpl,
	}, nil
}

// ServeHTTP handles GET / requests and renders the raid warmap.
func (h *RaidHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raids, err := h.fetcher.FetchRaids()
	if err != nil {
		http.Error(w, "Failed to fetch raids", http.StatusInternalServerError)
		return
	}

	mergeQueue, err := h.fetcher.FetchMergeQueue()
	if err != nil {
		// Non-fatal: show raids even if merge queue fails
		mergeQueue = nil
	}

	raiders, err := h.fetcher.FetchRaiders()
	if err != nil {
		// Non-fatal: show raids even if raiders fail
		raiders = nil
	}

	data := RaidData{
		Raids:    raids,
		MergeQueue: mergeQueue,
		Raiders:   raiders,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := h.template.ExecuteTemplate(w, "raid.html", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

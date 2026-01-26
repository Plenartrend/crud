package main

import (
	"encoding/json"
	"log"
	"net/http"
	api "plenartrend/crud/src/openAPI"
	"strings"

	"github.com/jmoiron/sqlx"
)

type Server struct {
	db *sqlx.DB
}

func NewServer(db *sqlx.DB) *Server {
	return &Server{db: db}
}

func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Server healthy!"))
}

func (s *Server) GetAlerts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Alert{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetBundestagStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.SessionStatus{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetCampaigns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Campaign{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) PostCampaigns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	resp := api.Campaign{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetCampaignsId(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.Campaign{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.DashboardData{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetPoliticians(w http.ResponseWriter, r *http.Request, params api.GetPoliticiansParams) {
	politicians := []api.Politician{}

	var args []any
	var err error

	// TODO we don't have a lot of this data
	// TODO use actual full titles of persons
	// TODO don't hardcode election period -> should probably come from params
	// TODO only return one record per person (currently one per role) -> Which role to pick?
	query := `
		SELECT
			1 as age,
			0.6 as contributionFactor,
			'd' as gender,
			r.id,
			'null' as image,
			(r.first_name || ' ' || r.last_name) as name,
			pg.name as party,
			'Deutschland' as region,
			r.name as role,
			null as similar,
			null as topTopics,
			'neutral' as volatility
		FROM roles r, parliamentary_groups pg
		WHERE r.election_period = 21
			AND r.group_id = pg.id
	`

	if params.Ids != nil && *params.Ids != "" {
		ids := strings.Split(*params.Ids, ",")
		log.Printf("Filtering politicians by IDs: %v", ids)

		query, args, err = sqlx.In(query+" AND r.person_id IN (?)", ids)

		if err != nil {
			log.Printf("Failed to build query: %v", err)
			http.Error(w, "Failed to build query", http.StatusInternalServerError)
			return
		}

		query = s.db.Rebind(query)
	}

	err = s.db.Select(&politicians, query, args...)
	if err != nil {
		log.Printf("Failed to query politicians: %v", err)
		http.Error(w, "Failed to query politicians", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(politicians)
}

func (s *Server) GetPoliticiansId(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.PoliticianDetail{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Report{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetSearch(w http.ResponseWriter, r *http.Request, params api.GetSearchParams) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.SearchResults{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetSpeeches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.FullSpeech{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetSpeechesId(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.SpeechDetail{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetTopics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Topic{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetTopicsId(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.TopicDetail{}
	_ = json.NewEncoder(w).Encode(resp)
}

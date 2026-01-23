package main

import (
	"encoding/json"
	"net/http"
	api "plenartrend/crud/src/openAPI"
)

type Server struct{}

func NewServer() *Server {
	return &Server{}
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Politician{}
	_ = json.NewEncoder(w).Encode(resp)
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

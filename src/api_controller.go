package main

import (
	"encoding/json"
	"log"
	"net/http"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/service"
	"plenartrend/crud/src/types"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

type Server struct {
	db               *sqlx.DB
	analyticsService *service.AnalyticsService
}

func NewServer(db *sqlx.DB) *Server {
	return &Server{
		db:               db,
		analyticsService: service.NewAnalyticsService(db),
	}
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

func (s *Server) GetAnalysisTimeSeries(w http.ResponseWriter, r *http.Request, params api.GetAnalysisTimeSeriesParams) {
	log.Printf("GetAnalysisTimeSeries called")

	if params.TimeRange == nil {
		http.Error(w, "time_range parameter is required", http.StatusBadRequest)
		return
	}

	dataPoints, err := s.analyticsService.GetAnalysisTimeSeries(*params.TimeRange, params.TopicId, params.PersonId, params.GroupId)
	if err != nil {
		log.Printf("Failed to get analysis time series: %v", err)
		http.Error(w, "Failed to get analysis time series", http.StatusInternalServerError)
		return
	}

	resp := api.AnalysisOverTimeResponse{
		Series: &dataPoints,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := api.DashboardData{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) getPoliticians(ids []string) ([]api.Politician, error) {
	politicians := []api.Politician{}

	var args []any
	var err error

	// TODO we don't have a lot of this data
	// TODO don't hardcode election period -> should probably come from params
	// TODO only return one record per person (currently one per role)? -> Which role to pick?
	query := `
		SELECT
			1 as age,
			0.6 as contributionFactor,
			'd' as gender,
			r.id,
			'null' as image,
			COALESCE(r.title, r.first_name || ' ' || COALESCE(r.name_suffix || ' ', '') || r.last_name || ', ' || r.name) as name,
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

	if len(ids) > 0 {
		log.Printf("Filtering politicians by IDs: %v", ids)

		query, args, err = sqlx.In(query+" AND r.person_id IN (?)", ids)

		if err != nil {
			log.Printf("Failed to build query: %v", err)
			return nil, err
		}

		query = s.db.Rebind(query)
	}

	err = s.db.Select(&politicians, query, args...)
	return politicians, err
}

func (s *Server) GetPoliticians(w http.ResponseWriter, r *http.Request, params api.GetPoliticiansParams) {
	politicians := []api.Politician{}
	ids := []string{}

	if params.Ids != nil && *params.Ids != "" {
		ids = strings.Split(*params.Ids, ",")
	}

	politicians, err := s.getPoliticians(ids)
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
	politicians, err := s.getPoliticians([]string{id})

	if err != nil {
		log.Printf("Failed to query politician: %v", err)
		http.Error(w, "Failed to query politician", http.StatusInternalServerError)
		return
	}

	if len(politicians) == 0 {
		log.Printf("Politician not found: %v", id)
		http.Error(w, "Politician not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(politicians[0])
}

func (s *Server) GetReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Report{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetSearch(w http.ResponseWriter, r *http.Request, params api.GetSearchParams) {
	politicians, err := s.getPoliticians(nil)
	if err != nil {
		log.Printf("Failed to query politicians: %v", err)
		http.Error(w, "Failed to query politicians", http.StatusInternalServerError)
		return
	}

	var filteredPoliticians []api.Politician
	if params.Q == nil || *params.Q == "" {
		filteredPoliticians = politicians
	} else {
		for _, p := range politicians {
			if strings.Contains(strings.ToLower(*p.Name), strings.ToLower(*params.Q)) {
				filteredPoliticians = append(filteredPoliticians, p)
			}
		}
	}

	topics, err := s.getTopics(nil)
	if err != nil {
		log.Printf("Failed to query topics: %v", err)
		http.Error(w, "Failed to query topics", http.StatusInternalServerError)
		return
	}

	var filteredTopics []api.Topic
	if params.Q == nil || *params.Q == "" {
		filteredTopics = topics
	} else {
		for _, t := range topics {
			if strings.Contains(strings.ToLower(*t.Title), strings.ToLower(*params.Q)) {
				filteredTopics = append(filteredTopics, t)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	searchResults := api.SearchResults{
		Politicians: &filteredPoliticians,
		Topics:      &filteredTopics,
	}
	_ = json.NewEncoder(w).Encode(searchResults)
}

func (s *Server) getSpeeches(id *int) ([]api.FullSpeech, error) {
	speeches := []api.FullSpeech{}

	idSelector := ""

	if id != nil {
		idSelector = " AND a.id = " + strconv.Itoa(*id)
	}

	// TODO we don't have a lot of this data
	err := s.db.Select(&speeches, `
		SELECT
			a.text as content,
			p.date as date,
			null as duration,
			a.id as id,
			null as relatedTopics,
			null as sentiment,
			null as session,
			p.url as sourceUrl,
			a.role_id as speakerId,
			null as title,
			null as topicId,
			a.type as type
		FROM activities a, protocols p
		WHERE a.protocol_id = p.id
			AND a.type LIKE 'Rede%'
			`+idSelector+`
	`)

	return speeches, err
}

func (s *Server) GetSpeeches(w http.ResponseWriter, r *http.Request) {
	speeches := []api.FullSpeech{}

	speeches, err := s.getSpeeches(nil)

	if err != nil {
		log.Printf("Failed to query speeches: %v", err)
		http.Error(w, "Failed to query speeches", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(speeches)
}

func (s *Server) GetSpeechesId(w http.ResponseWriter, r *http.Request, id string) {
	speechID, err := strconv.Atoi(id)

	if err != nil {
		log.Printf("Invalid speech ID: %v", err)
		http.Error(w, "Invalid speech ID", http.StatusBadRequest)
		return
	}

	speeches, err := s.getSpeeches(&speechID)

	if err != nil {
		log.Printf("Failed to query speech: %v", err)
		http.Error(w, "Failed to query speech", http.StatusInternalServerError)
		return
	}

	if len(speeches) == 0 {
		log.Printf("Speech not found: %v", speechID)
		http.Error(w, "Speech not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(speeches[0])
}

func (s *Server) getTopics(id *int) ([]api.Topic, error) {
	topics := []api.Topic{}
	idSelector := ""
	if id != nil {
		idSelector = " WHERE id = " + strconv.Itoa(*id)
	}
	err := s.db.Select(&topics, `
		SELECT
			null as category,
			id as id,
			null as relevance,
			name as title,
			null as sentiment,
			null as trend
		FROM topics
		`+idSelector+`
	`)
	return topics, err
}

func (s *Server) GetTopics(w http.ResponseWriter, r *http.Request, params api.GetTopicsParams) {
	log.Printf("GetTopics called")
	pageSize, offset := PaginationFromRequest(r)

	// Total count
	var totalItems int
	err := s.db.Get(&totalItems, `
		SELECT COUNT(*) FROM get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta
		JOIN topics t ON t.id = ta.topic_id
	`)
	if err != nil {
		log.Printf("Failed to count topics: %v", err)
		http.Error(w, "Failed to query topics", http.StatusInternalServerError)
		return
	}

	meta := PaginationMeta{
		Page:       PageFromOffset(offset, pageSize),
		PageSize:   pageSize,
		TotalItems: totalItems,
	}

	dataQuery := `
		SELECT t.id, t.name, t.updated, t.created, ta.topic_relevance, ta.avg_sentiment
		FROM get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta
		JOIN topics t ON t.id = ta.topic_id
		ORDER BY ta.topic_relevance DESC
		LIMIT $1 OFFSET $2
	`
	var rows []types.TopicWithAnalytics
	err = s.db.Select(&rows, dataQuery, pageSize, offset)
	if err != nil {
		log.Printf("Failed to query topics: %v", err)
		http.Error(w, "Failed to query topics", http.StatusInternalServerError)
		return
	}
	for _, row := range rows {
		log.Printf("Topic: %v", row.Name)
		log.Printf("TopicRelevance: %v", row.TopicRelevance)
		log.Printf("AvgSentiment: %v", row.AvgSentiment)
	}

	data := make([]api.Topic, 0, len(rows))
	for _, row := range rows {
		idStr := strconv.Itoa(row.ID)
		rel := float32(row.TopicRelevance)
		sent := float32(row.AvgSentiment)
		data = append(data, api.Topic{
			Id:        &idStr,
			Title:     &row.Name,
			Relevance: &rel,
			Sentiment: &sent,
		})
	}

	paginatedTopics := api.PaginatedTopics{
		Data:       data,
		Page:       meta.Page,
		PageSize:   meta.PageSize,
		TotalItems: meta.TotalItems,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(paginatedTopics)
}

func (s *Server) GetTopicsId(w http.ResponseWriter, r *http.Request, id string) {
	log.Printf("GetTopicsId called with id: %s", id)
	topicID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Invalid topic ID: %v", err)
		http.Error(w, "Invalid topic ID", http.StatusBadRequest)
		return
	}
	log.Printf("Fetching topic with ID: %d", topicID)

	topicDetail, err := s.analyticsService.GetTopicDetail(topicID)
	if err != nil {
		log.Printf("Failed to get topic detail: %v", err)
		http.Error(w, "Failed to query topic", http.StatusInternalServerError)
		return
	}

	log.Printf("Sending response for topic %d", topicID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(topicDetail)
}

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

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/jmoiron/sqlx"
)

type Server struct {
	db                 *sqlx.DB
	politiciansService *service.PoliticiansService
	topicsService      *service.TopicsService
	helpersService     *service.HelpersService
}

func NewServer(db *sqlx.DB) *Server {
	helpersService := service.NewHelpersService(db)
	topicsService := service.NewTopicsService(db, helpersService)
	politiciansService := service.NewPoliticiansService(db, topicsService, helpersService)
	return &Server{
		db:                 db,
		politiciansService: politiciansService,
		topicsService:      topicsService,
		helpersService:     helpersService,
	}
}

// Helper function to convert contribution factor string to API enum

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

	dataPoints, err := s.topicsService.GetAnalysisTimeSeries(*params.TimeRange, params.TopicId, params.PersonId, params.GroupId)
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

func (s *Server) GetPoliticians(w http.ResponseWriter, r *http.Request, params api.GetPoliticiansParams) {
	log.Printf("GetPoliticians called with params: %+v", params)
	log.Printf("Raw query string: %s", r.URL.RawQuery)

	// Get pagination parameters
	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
	}

	var electionPeriod *int
	if params.ElectionPeriod != nil {
		electionPeriod = params.ElectionPeriod
	}

	var groupID *int
	if params.GroupId != nil {
		groupID = params.GroupId
		log.Printf("Group ID filter received: %d", *groupID)
	} else {
		log.Printf("No group ID filter received")
	}

	politicians, totalCount, err := s.politiciansService.GetPoliticians(electionPeriod, groupID, params.PageSize, offset)
	if err != nil {
		log.Printf("Failed to query politicians: %v", err)
		http.Error(w, "Failed to query politicians", http.StatusInternalServerError)
		return
	}

	var page int
	var pageSize int
	if params.PageSize == nil {
		page = 1
		pageSize = totalCount
	} else {
		pageSize = *params.PageSize
		page = PageFromOffset(offset, pageSize)
	}

	response := api.PaginatedPoliticians{
		Data:       politicians,
		TotalItems: totalCount,
		Page:       page,
		PageSize:   pageSize,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) GetPoliticiansId(w http.ResponseWriter, r *http.Request, id string, params api.GetPoliticiansIdParams) {
	log.Printf("GetPoliticiansId called with id=%s, params=%+v", id, params)

	personID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Failed to convert ID to int: %v", err)
		http.Error(w, "Failed to convert ID to int", http.StatusBadRequest)
		return
	}

	var electionPeriod int
	if params.ElectionPeriod == nil {
		log.Printf("No election period provided, fetching max for person %d", personID)
		electionPeriod, err = s.helpersService.GetMaxElectionPeriod(personID)
		if err != nil {
			log.Printf("Failed to get max election period: %v", err)
			http.Error(w, "Failed to get max election period", http.StatusInternalServerError)
			return
		}
		log.Printf("Using max election period: %d", electionPeriod)
	} else {
		electionPeriod = int(*params.ElectionPeriod)
		log.Printf("Using provided election period: %d", electionPeriod)
	}

	politicianDetail, err := s.politiciansService.GetPoliticianDetail(personID, electionPeriod)
	if err != nil {
		log.Printf("Failed to get politician details: %v", err)
		http.Error(w, "Politician not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(politicianDetail)
}

func (s *Server) GetPoliticiansIdSimilar(w http.ResponseWriter, r *http.Request, id string, params api.GetPoliticiansIdSimilarParams) {
	log.Printf("GetPoliticiansIdSimilar called with id=%s, params=%+v", id, params)

	personID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Failed to convert ID to int: %v", err)
		http.Error(w, "Failed to convert ID to int", http.StatusBadRequest)
		return
	}

	var electionPeriod int
	if params.ElectionPeriod == nil {
		log.Printf("No election period provided, fetching max for person %d", personID)
		electionPeriod, err = s.helpersService.GetMaxElectionPeriod(personID)
		if err != nil {
			log.Printf("Failed to get max election period: %v", err)
			http.Error(w, "Failed to get max election period", http.StatusInternalServerError)
			return
		}
		log.Printf("Using max election period: %d", electionPeriod)
	} else {
		electionPeriod = int(*params.ElectionPeriod)
		log.Printf("Using provided election period: %d", electionPeriod)
	}

	similar, err := s.politiciansService.GetSimilarPoliticians(personID, electionPeriod)
	if err != nil {
		log.Printf("Failed to get similar politicians: %v", err)
		http.Error(w, "Failed to get similar politicians", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(similar)
}

func (s *Server) GetPoliticiansIdActivity(w http.ResponseWriter, r *http.Request, id string, params api.GetPoliticiansIdActivityParams) {
	log.Printf("GetPoliticiansIdActivity called with id=%s, params=%+v", id, params)

	personID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Failed to convert ID to int: %v", err)
		http.Error(w, "Failed to convert ID to int", http.StatusBadRequest)
		return
	}

	var electionPeriod int
	if params.ElectionPeriod == nil {
		log.Printf("No election period provided, fetching max for person %d", personID)
		electionPeriod, err = s.helpersService.GetMaxElectionPeriod(personID)
		if err != nil {
			log.Printf("Failed to get max election period: %v", err)
			http.Error(w, "Failed to get max election period", http.StatusInternalServerError)
			return
		}
		log.Printf("Using max election period: %d", electionPeriod)
	} else {
		electionPeriod = int(*params.ElectionPeriod)
		log.Printf("Using provided election period: %d", electionPeriod)
	}

	timeRange := api.TimeRangeFilter("last_year")
	if params.TimeRange != nil {
		timeRange = *params.TimeRange
	}
	activityData, err := s.politiciansService.GetActivityTimeSeries(timeRange, &personID, nil)
	if err != nil {
		log.Printf("Failed to get activity time series: %v", err)
		http.Error(w, "Failed to get activity time series", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(activityData)
}

func (s *Server) GetElectionPeriods(w http.ResponseWriter, r *http.Request) {
	log.Printf("GetElectionPeriods called")

	var periods []types.ElectionPeriod
	err := s.db.Select(&periods, "SELECT DISTINCT ep.* FROM analysed_protocols ap JOIN election_periods ep ON ap.election_period = ep.number ORDER BY ep.number DESC")
	if err != nil {
		log.Printf("Failed to query election periods: %v", err)
		http.Error(w, "Failed to query election periods", http.StatusInternalServerError)
		return
	}
	log.Printf("Found %d election periods", len(periods))

	// Convert to API response format
	response := make([]api.ElectionPeriod, len(periods))
	for i, p := range periods {
		id := p.Number
		number := p.Number
		var startDate *openapi_types.Date
		if p.StartDate.Valid {
			startDate = &openapi_types.Date{Time: p.StartDate.Time}
		}
		var endDate *openapi_types.Date
		if p.EndDate.Valid {
			endDate = &openapi_types.Date{Time: p.EndDate.Time}
		}

		response[i] = api.ElectionPeriod{
			Id:        &id,
			Number:    &number,
			StartDate: startDate,
			EndDate:   endDate,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) GetReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := []api.Report{}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) GetSearch(w http.ResponseWriter, r *http.Request, params api.GetSearchParams) {
	// Use nil page size to get all politicians for search
	politicians, _, err := s.politiciansService.GetPoliticians(nil, nil, nil, 0)
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

	topics, totalItems, err := s.topicsService.GetTopics(pageSize, offset)
	if err != nil {
		log.Printf("Failed to query topics: %v", err)
		http.Error(w, "Failed to query topics", http.StatusInternalServerError)
		return
	}

	paginatedTopics := api.PaginatedTopics{
		Data:       topics,
		Page:       PageFromOffset(offset, pageSize),
		PageSize:   pageSize,
		TotalItems: totalItems,
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

	topicDetail, err := s.topicsService.GetTopicDetail(topicID, nil, nil)
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

func (s *Server) GetParliamentaryGroups(w http.ResponseWriter, r *http.Request, params api.GetParliamentaryGroupsParams) {
	period := params.ElectionPeriod

	log.Printf("Fetching parliamentary groups for election period: %d", period)

	// Query groups that have at least one role in the specified election period
	query := `
		SELECT DISTINCT pg.id, pg.name
		FROM parliamentary_groups pg
		JOIN roles r ON r.group_id = pg.id
		WHERE r.election_period = ?
		ORDER BY pg.name
	`

	type GroupResult struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}

	groups := []GroupResult{}
	err := s.db.Select(&groups, s.db.Rebind(query), period)
	if err != nil {
		log.Printf("Failed to query parliamentary groups: %v", err)
		http.Error(w, "Failed to query parliamentary groups", http.StatusInternalServerError)
		return
	}

	// Convert to API types
	apiGroups := []api.ParliamentaryGroup{}
	for _, g := range groups {
		id := g.ID
		name := g.Name
		apiGroups = append(apiGroups, api.ParliamentaryGroup{
			Id:   &id,
			Name: &name,
		})
	}

	log.Printf("Found %d parliamentary groups", len(apiGroups))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiGroups)
}

func (s *Server) GetPoliticiansMostActive(w http.ResponseWriter, r *http.Request, params api.GetPoliticiansMostActiveParams) {
	limit := 10
	if params.Limit != nil {
		limit = *params.Limit
		if limit < 1 || limit > 100 {
			limit = 10
		}
	}

	period := 21
	if params.ElectionPeriod != nil {
		period = *params.ElectionPeriod
	}

	log.Printf("GetPoliticiansMostActive called with limit=%d, election_period=%d", limit, period)

	activePoliticians, err := s.politiciansService.GetActivePoliticians(period, limit, true)
	if err != nil {
		log.Printf("Failed to get most active politicians: %v", err)
		http.Error(w, "Failed to get most active politicians", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(activePoliticians)
}

func (s *Server) GetPoliticiansLeastActive(w http.ResponseWriter, r *http.Request, params api.GetPoliticiansLeastActiveParams) {
	limit := 10
	if params.Limit != nil {
		limit = *params.Limit
		if limit < 1 || limit > 100 {
			limit = 10
		}
	}

	period := 21
	if params.ElectionPeriod != nil {
		period = *params.ElectionPeriod
	}

	log.Printf("GetPoliticiansLeastActive called with limit=%d, election_period=%d", limit, period)

	activePoliticians, err := s.politiciansService.GetActivePoliticians(period, limit, false)
	if err != nil {
		log.Printf("Failed to get least active politicians: %v", err)
		http.Error(w, "Failed to get least active politicians", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(activePoliticians)
}

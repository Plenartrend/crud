package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

func (s *Server) getPoliticians(electionPeriod *int, groupID *int, pageSize *int, offset int) ([]api.Politician, int, error) {
	politicians := []api.Politician{}
	log.Printf("Getting politicians for election period: %d, groupID: %v, pageSize: %d, offset: %d", electionPeriod, groupID, pageSize, offset)

	// Default to latest election period if not specified
	period := 21 //TODO: Fetch
	if electionPeriod != nil {
		period = *electionPeriod
	}

	// Query with pagination
	query := (`
		SELECT r.*, pg.name as faction_name
		FROM roles r
		LEFT JOIN parliamentary_groups pg ON r.group_id = pg.id
		JOIN (
			SELECT person_id, MIN(id) as min_id
			FROM roles
			WHERE election_period = $1
			GROUP BY person_id
		) r2
		ON r.id = r2.min_id
		WHERE r.election_period = $1
		AND CASE WHEN $4::integer IS NOT NULL THEN r.group_id = $4::integer ELSE TRUE END
		ORDER BY r.last_name, r.first_name
		LIMIT CASE WHEN $2::integer IS NULL THEN NULL ELSE $2::integer END OFFSET CASE WHEN $2::integer IS NULL THEN 0 ELSE $3 END
	`)

	countQuery := (`
		SELECT COUNT(r.*)
		FROM roles r
		LEFT JOIN parliamentary_groups pg ON r.group_id = pg.id
		JOIN (
			SELECT person_id, MIN(id) as min_id
			FROM roles
			WHERE election_period = $1
			GROUP BY person_id
		) r2
		ON r.id = r2.min_id
		WHERE r.election_period = $1
		AND CASE WHEN $2::integer IS NOT NULL THEN r.group_id = $2::integer ELSE TRUE END
	`)

	var totalCount int
	err := s.db.Get(&totalCount, s.db.Rebind(countQuery), period, groupID)
	if err != nil {
		log.Printf("Failed to count politicians: %v", err)
		return nil, 0, err
	}

	log.Printf("Fetching politicians for election period: %d, groupID: %v, limit: %d, offset: %d", period, groupID, pageSize, offset)

	type RoleWithFaction struct {
		types.Role
		FactionName sql.NullString `db:"faction_name"`
	}

	rolesWithFaction := []RoleWithFaction{}
	err = s.db.Select(&rolesWithFaction, s.db.Rebind(query), period, pageSize, offset, groupID)
	if err != nil {
		log.Printf("Failed to query roles: %v", err)
		return nil, 0, err
	}

	personIDs := make([]int, len(rolesWithFaction))
	for i, roleWithFaction := range rolesWithFaction {
		personIDs[i] = roleWithFaction.Role.PersonID
	}

	cfactors, err := s.analyticsService.GetContributionFactor(period, personIDs)
	if err != nil {
		log.Printf("Failed to get contribution factors: %v", err)
		cfactors = make(map[int]service.ContributionFactor)
	}

	volatilities, err := s.analyticsService.GetVolatility(period, personIDs)
	if err != nil {
		log.Printf("Failed to get volatilities: %v", err)
		volatilities = make(map[int]float64)
	}

	topTopicsMap, err := s.analyticsService.GetTopTopics(period, personIDs, 3)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		topTopicsMap = make(map[int][]types.Topic)
	}

	speechCounts, err := s.analyticsService.GetNumberOfSpeeches(period, personIDs)
	if err != nil {
		log.Printf("Failed to get speech counts: %v", err)
		speechCounts = make(map[int]int)
	}

	// Convert roles to politicians with analytics data
	for _, roleWithFaction := range rolesWithFaction {
		role := roleWithFaction.Role
		personID := role.PersonID

		// Get analytics data from maps with defaults
		cfactor := cfactors[personID]
		if cfactor == "" {
			cfactor = "low"
		}

		volatility := volatilities[personID]
		topTopics := topTopicsMap[personID]
		if topTopics == nil {
			topTopics = []types.Topic{}
		}

		numSpeeches := speechCounts[personID]

		// Convert to API types (pointers)
		idStr := strconv.Itoa(role.PersonID)
		nameSuffix := ""
		if role.NameSuffix.Valid && role.NameSuffix.String != "" {
			nameSuffix = role.NameSuffix.String + " "
		}
		name := role.FirstName + " " + nameSuffix + role.LastName
		party := ""
		if roleWithFaction.FactionName.Valid {
			party = roleWithFaction.FactionName.String
		}
		roleStr := ""
		if role.RoleName.Valid {
			roleStr = role.RoleName.String
		}
		volatilityStr := fmt.Sprintf("%.2f", volatility)

		// Convert contribution factor to API enum type
		cfactorStr := strings.ToLower(string(cfactor))
		var apiContributionFactor api.PoliticianContributionFactor
		switch cfactorStr {
		case "high":
			apiContributionFactor = api.PoliticianContributionFactorHigh
		case "medium":
			apiContributionFactor = api.PoliticianContributionFactorMedium
		default:
			apiContributionFactor = api.PoliticianContributionFactorLow
		}

		// Convert top topics to API format
		apiTopTopics := make([]api.TopTopic, 0, len(topTopics))
		for _, topic := range topTopics {
			topicName := topic.Name
			stance := "neutral" // We don't have stance data yet
			apiTopTopics = append(apiTopTopics, api.TopTopic{
				Topic:  &topicName,
				Stance: &stance,
			})
		}

		politician := api.Politician{
			Id:                 &idStr,
			Name:               &name,
			Party:              &party,
			Role:               &roleStr,
			Volatility:         &volatilityStr,
			ContributionFactor: &apiContributionFactor,
			TopTopics:          &apiTopTopics,
			NumSpeeches:        &numSpeeches,
		}

		politicians = append(politicians, politician)
	}

	return politicians, totalCount, nil
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

	politicians, totalCount, err := s.getPoliticians(electionPeriod, groupID, params.PageSize, offset)
	if err != nil {
		log.Printf("Failed to query politicians: %v", err)
		http.Error(w, "Failed to query politicians", http.StatusInternalServerError)
		return
	}

	var page int
	if params.PageSize == nil {
		page = 1
	} else {
		page = (offset / *params.PageSize) + 1
	}

	var pageSize int
	if params.PageSize == nil {
		pageSize = totalCount
	} else {
		pageSize = *params.PageSize
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

func (s *Server) GetPoliticiansId(w http.ResponseWriter, r *http.Request, id string) {
	// Query directly for single politician by ID
	period := 21 // Default to current period

	query := `
		SELECT r.*, pg.name as faction_name
		FROM roles r
		LEFT JOIN parliamentary_groups pg ON r.group_id = pg.id
		WHERE r.person_id = ? AND r.election_period = ?
		LIMIT 1
	`

	type RoleWithFaction struct {
		types.Role
		FactionName sql.NullString `db:"faction_name"`
	}

	var roleWithFaction RoleWithFaction
	err := s.db.Get(&roleWithFaction, s.db.Rebind(query), id, period)
	if err != nil {
		log.Printf("Failed to query politician: %v", err)
		http.Error(w, "Politician not found", http.StatusNotFound)
		return
	}

	role := roleWithFaction.Role

	// Use batch methods with single ID
	personIDs := []int{role.PersonID}

	cfactors, err := s.analyticsService.GetContributionFactor(period, personIDs)
	if err != nil {
		log.Printf("Failed to get contribution factor: %v", err)
		cfactors = make(map[int]service.ContributionFactor)
	}
	contributionFactor := cfactors[role.PersonID]
	if contributionFactor == "" {
		contributionFactor = "low"
	}

	volatilities, err := s.analyticsService.GetVolatility(period, personIDs)
	if err != nil {
		log.Printf("Failed to get volatility: %v", err)
		volatilities = make(map[int]float64)
	}
	volatility := volatilities[role.PersonID]

	// Convert to API types (pointers)
	idStr := strconv.Itoa(role.PersonID)
	name := role.FirstName + " " + role.LastName
	party := ""
	if roleWithFaction.FactionName.Valid {
		party = roleWithFaction.FactionName.String
	}
	roleStr := ""
	if role.RoleName.Valid {
		roleStr = role.RoleName.String
	}
	volatilityStr := fmt.Sprintf("%.2f", volatility)

	// Convert contribution factor to API enum type
	contributionFactorStr := strings.ToLower(string(contributionFactor))
	var apiContributionFactor api.PoliticianContributionFactor
	switch contributionFactorStr {
	case "high":
		apiContributionFactor = api.PoliticianContributionFactorHigh
	case "medium":
		apiContributionFactor = api.PoliticianContributionFactorMedium
	default:
		apiContributionFactor = api.PoliticianContributionFactorLow
	}

	politician := api.Politician{
		Id:                 &idStr,
		Name:               &name,
		Party:              &party,
		Role:               &roleStr,
		Volatility:         &volatilityStr,
		ContributionFactor: &apiContributionFactor,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(politician)
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
	politicians, _, err := s.getPoliticians(nil, nil, nil, 0)
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

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
	partiesService     *service.PartyService
}

func NewServer(db *sqlx.DB) *Server {
	helpersService := service.NewHelpersService(db)
	topicsService := service.NewTopicsService(db, helpersService)
	partiesService := service.NewPartyService(db, topicsService)
	politiciansService := service.NewPoliticiansService(db, topicsService, helpersService)
	return &Server{
		db:                 db,
		politiciansService: politiciansService,
		topicsService:      topicsService,
		helpersService:     helpersService,
		partiesService:     partiesService,
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

	dataPoints, err := s.topicsService.GetAnalysisTimeSeries(params.TimeRange, params.TopicId, params.PersonId, params.GroupId)
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

	var groupID *int
	if params.GroupId != nil {
		groupID = params.GroupId
		log.Printf("Group ID filter received: %d", *groupID)
	} else {
		log.Printf("No group ID filter received")
	}

	politicians, totalCount, err := s.politiciansService.GetPoliticians(params.ElectionPeriod, groupID, params.PageSize, offset)
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

	politicianDetail, err := s.politiciansService.GetPoliticianDetail(personID, params.ElectionPeriod)
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

	similar, err := s.politiciansService.GetSimilarPoliticians(personID, params.ElectionPeriod)
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

	activityData, err := s.politiciansService.GetActivityTimeSeries(params.TimeRange, &personID, nil)
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
	politicians, _, err := s.politiciansService.GetPoliticians(21, nil, nil, 0) // TODO: election period
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

func (s *Server) GetParties(w http.ResponseWriter, r *http.Request, params api.GetPartiesParams) {
	parties, err := s.partiesService.GetParties(params.ElectionPeriod)

	if err != nil {
		log.Printf("Failed to query parties: %v", err)
		http.Error(w, "Failed to query parties", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(parties)
}

func (s *Server) GetPartiesId(w http.ResponseWriter, r *http.Request, id string, params api.GetPartiesIdParams) {
	partyID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Invalid party ID: %v", err)
		http.Error(w, "Invalid party ID", http.StatusBadRequest)
		return
	}

	partyDetail, err := s.partiesService.GetPartiesId(partyID, params.ElectionPeriod, params.TimeRange)
	if err != nil {
		log.Printf("Failed to get party detail: %v", err)
		http.Error(w, "Failed to query party", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(partyDetail)
}

/*
func (s *Server) GetParties_gen(w http.ResponseWriter, r *http.Request) {
	log.Printf("GetParties called")

	period := 21 // Default to current period

	// Query all parliamentary groups
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

	// Get all person IDs for each group
	groupPersonIDs := make(map[int][]int)
	for _, g := range groups {
		var personIDs []int
		err := s.db.Select(&personIDs, s.db.Rebind(`
			SELECT DISTINCT person_id
			FROM roles
			WHERE group_id = ? AND election_period = ?
		`), g.ID, period)
		if err != nil {
			log.Printf("Failed to get person IDs for group %d: %v", g.ID, err)
			continue
		}
		groupPersonIDs[g.ID] = personIDs
	}

	// Convert to API types with analytics
	apiParties := []api.Party{}
	for _, g := range groups {
		personIDs := groupPersonIDs[g.ID]
		if len(personIDs) == 0 {
			continue
		}

		// Get analytics for all members
		cfactors, _ := s.analyticsService.GetContributionFactor(period, personIDs)
		volatilities, _ := s.analyticsService.GetVolatility(period, personIDs)
		topTopicsMap, _ := s.analyticsService.GetTopTopics(period, personIDs, 3)
		speechCounts, _ := s.analyticsService.GetNumberOfSpeeches(period, personIDs)

		// Aggregate analytics
		totalSpeeches := 0
		for _, count := range speechCounts {
			totalSpeeches += count
		}

		// Calculate average volatility
		avgVolatility := 0.0
		if len(volatilities) > 0 {
			for _, v := range volatilities {
				avgVolatility += v
			}
			avgVolatility /= float64(len(volatilities))
		}

		// Determine predominant contribution factor
		cfactorCounts := map[service.ContributionFactor]int{}
		for _, cf := range cfactors {
			cfactorCounts[cf]++
		}
		var predominantCF service.ContributionFactor = "low"
		maxCount := 0
		for cf, count := range cfactorCounts {
			if count > maxCount {
				maxCount = count
				predominantCF = cf
			}
		}

		// Aggregate top topics across all members
		topicFrequency := make(map[int]int)
		for _, topics := range topTopicsMap {
			for _, topic := range topics {
				topicFrequency[topic.ID]++
			}
		}

		// Get top 3 topics by frequency
		type topicCount struct {
			topic types.Topic
			count int
		}
		var topicCounts []topicCount
		for topicID, count := range topicFrequency {
			for _, topics := range topTopicsMap {
				for _, topic := range topics {
					if topic.ID == topicID {
						topicCounts = append(topicCounts, topicCount{topic, count})
						break
					}
				}
			}
		}

		// Sort by count and take top 3
		apiTopTopics := []api.TopTopic{}
		for i := 0; i < len(topicCounts) && i < 3; i++ {
			maxIdx := i
			for j := i + 1; j < len(topicCounts); j++ {
				if topicCounts[j].count > topicCounts[maxIdx].count {
					maxIdx = j
				}
			}
			if maxIdx != i {
				topicCounts[i], topicCounts[maxIdx] = topicCounts[maxIdx], topicCounts[i]
			}
			topicName := topicCounts[i].topic.Name
			stance := "neutral"
			apiTopTopics = append(apiTopTopics, api.TopTopic{
				Topic:  &topicName,
				Stance: &stance,
			})
		}

		id := strconv.Itoa(g.ID)
		name := g.Name
		volatilityStr := fmt.Sprintf("%.2f", avgVolatility)

		var apiCF api.PartyContributionFactor
		switch strings.ToLower(string(predominantCF)) {
		case "high":
			apiCF = api.PartyContributionFactorHigh
		case "medium":
			apiCF = api.PartyContributionFactorMedium
		default:
			apiCF = api.PartyContributionFactorLow
		}

		apiParties = append(apiParties, api.Party{
			Id:                 &id,
			Name:               &name,
			NumSpeeches:        &totalSpeeches,
			Volatility:         &volatilityStr,
			ContributionFactor: &apiCF,
			TopTopics:          &apiTopTopics,
		})
	}

	log.Printf("Returning %d parties", len(apiParties))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiParties)
}

func (s *Server) GetPartiesId_gen(w http.ResponseWriter, r *http.Request, id string) {
	log.Printf("GetPartiesId called with id: %s", id)

	groupID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Invalid party ID: %v", err)
		http.Error(w, "Invalid party ID", http.StatusBadRequest)
		return
	}

	period := 21 // Default to current period

	// Query the parliamentary group
	type GroupResult struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}

	var group GroupResult
	err = s.db.Get(&group, s.db.Rebind(`
		SELECT pg.id, pg.name
		FROM parliamentary_groups pg
		WHERE pg.id = ?
	`), groupID)
	if err != nil {
		log.Printf("Failed to query party: %v", err)
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	// Get all person IDs for this group
	var personIDs []int
	err = s.db.Select(&personIDs, s.db.Rebind(`
		SELECT DISTINCT person_id
		FROM roles
		WHERE group_id = ? AND election_period = ?
	`), groupID, period)
	if err != nil {
		log.Printf("Failed to get person IDs: %v", err)
		http.Error(w, "Failed to get party members", http.StatusInternalServerError)
		return
	}

	// Get members (politicians)
	members, _, err := s.getPoliticians(&period, &groupID, nil, 0)
	if err != nil {
		log.Printf("Failed to get members: %v", err)
		members = []api.Politician{}
	}

	// Get analytics for all members
	cfactors, _ := s.analyticsService.GetContributionFactor(period, personIDs)
	volatilities, _ := s.analyticsService.GetVolatility(period, personIDs)
	topTopicsMap, _ := s.analyticsService.GetTopTopics(period, personIDs, 3)
	speechCounts, _ := s.analyticsService.GetNumberOfSpeeches(period, personIDs)

	// Aggregate analytics
	totalSpeeches := 0
	for _, count := range speechCounts {
		totalSpeeches += count
	}

	// Calculate average volatility
	avgVolatility := 0.0
	if len(volatilities) > 0 {
		for _, v := range volatilities {
			avgVolatility += v
		}
		avgVolatility /= float64(len(volatilities))
	}

	// Determine predominant contribution factor
	cfactorCounts := map[service.ContributionFactor]int{}
	for _, cf := range cfactors {
		cfactorCounts[cf]++
	}
	var predominantCF service.ContributionFactor = "low"
	maxCount := 0
	for cf, count := range cfactorCounts {
		if count > maxCount {
			maxCount = count
			predominantCF = cf
		}
	}

	// Aggregate top topics
	topicFrequency := make(map[int]int)
	for _, topics := range topTopicsMap {
		for _, topic := range topics {
			topicFrequency[topic.ID]++
		}
	}

	type topicCount struct {
		topic types.Topic
		count int
	}
	var topicCounts []topicCount
	for topicID, count := range topicFrequency {
		for _, topics := range topTopicsMap {
			for _, topic := range topics {
				if topic.ID == topicID {
					topicCounts = append(topicCounts, topicCount{topic, count})
					break
				}
			}
		}
	}

	apiTopTopics := []api.TopTopic{}
	for i := 0; i < len(topicCounts) && i < 3; i++ {
		maxIdx := i
		for j := i + 1; j < len(topicCounts); j++ {
			if topicCounts[j].count > topicCounts[maxIdx].count {
				maxIdx = j
			}
		}
		if maxIdx != i {
			topicCounts[i], topicCounts[maxIdx] = topicCounts[maxIdx], topicCounts[i]
		}
		topicName := topicCounts[i].topic.Name
		stance := "neutral"
		apiTopTopics = append(apiTopTopics, api.TopTopic{
			Topic:  &topicName,
			Stance: &stance,
		})
	}

	// Get recent speeches
	var recentSpeeches []api.SpeechSnippet
	if len(personIDs) > 0 {
		// Build query with IN clause
		query := `
			SELECT
				a.id,
				a.text,
				p.date,
				r.first_name || ' ' || r.last_name as speaker,
				pg.name as party
			FROM activities a
			JOIN protocols p ON a.protocol_id = p.id
			JOIN roles r ON a.role_id = r.id
			LEFT JOIN parliamentary_groups pg ON r.group_id = pg.id
			WHERE r.person_id IN (?) AND a.type LIKE 'Rede%'
			ORDER BY p.date DESC
			LIMIT 5
		`
		query, args, err := sqlx.In(query, personIDs)
		if err == nil {
			query = s.db.Rebind(query)
			type SpeechRow struct {
				ID      int            `db:"id"`
				Text    string         `db:"text"`
				Date    sql.NullTime   `db:"date"`
				Speaker string         `db:"speaker"`
				Party   sql.NullString `db:"party"`
			}
			var rows []SpeechRow
			err = s.db.Select(&rows, query, args...)
			if err == nil {
				for _, row := range rows {
					speechID := strconv.Itoa(row.ID)
					text := row.Text
					if len(text) > 200 {
						text = text[:200] + "..."
					}
					speaker := row.Speaker
					party := ""
					if row.Party.Valid {
						party = row.Party.String
					}
					var date *time.Time
					if row.Date.Valid {
						date = &row.Date.Time
					}
					sentiment := api.Neutral
					recentSpeeches = append(recentSpeeches, api.SpeechSnippet{
						Id:           &speechID,
						FullSpeechId: &speechID,
						Text:         &text,
						Speaker:      &speaker,
						Party:        &party,
						Date:         date,
						Sentiment:    &sentiment,
					})
				}
			}
		}
	}

	idStr := strconv.Itoa(group.ID)
	name := group.Name
	volatilityStr := fmt.Sprintf("%.2f", avgVolatility)

	var apiCF api.PartyDetailContributionFactor
	switch strings.ToLower(string(predominantCF)) {
	case "high":
		apiCF = api.PartyDetailContributionFactorHigh
	case "medium":
		apiCF = api.PartyDetailContributionFactorMedium
	default:
		apiCF = api.PartyDetailContributionFactorLow
	}

	partyDetail := api.PartyDetail{
		Id:                 &idStr,
		Name:               &name,
		NumSpeeches:        &totalSpeeches,
		Volatility:         &volatilityStr,
		ContributionFactor: &apiCF,
		TopTopics:          &apiTopTopics,
		Members:            &members,
		RecentSpeeches:     &recentSpeeches,
	}

	log.Printf("Returning party detail for ID %d", groupID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(partyDetail)
}
*/

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

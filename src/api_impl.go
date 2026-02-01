package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	api "plenartrend/crud/src/openAPI"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	openapi_types "github.com/oapi-codegen/runtime/types"
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

func (s *Server) GetAnalysisTimeSeries(w http.ResponseWriter, r *http.Request, params api.GetAnalysisTimeSeriesParams) {
	log.Printf("GetAnalysisTimeSeries called")

	// Validate required parameter
	if params.TimeRange == nil {
		http.Error(w, "time_range parameter is required", http.StatusBadRequest)
		return
	}

	// Generate week dates based on time range
	var weekDates []string
	now := time.Now()
	isoYear, isoWeek := now.ISOWeek()

	// Helper to find Monday of a given ISO week
	weekStart := func(year, week int) time.Time {
		jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
		jan4Weekday := int(jan4.Weekday())
		if jan4Weekday == 0 {
			jan4Weekday = 7
		}
		isoWeek1Monday := jan4.AddDate(0, 0, -jan4Weekday+1)
		return isoWeek1Monday.AddDate(0, 0, (week-1)*7)
	}

	getWeeks := func(numWeeks int) []string {
		res := make([]string, numWeeks)
		year, week := isoYear, isoWeek
		for i := 0; i < numWeeks; i++ {
			currWeek := week - i
			currYear := year
			for currWeek <= 0 {
				currYear--
				weeksLastYear := 52
				lastYearDate := time.Date(currYear, 12, 28, 0, 0, 0, 0, time.UTC)
				if _, lastWeek := lastYearDate.ISOWeek(); lastWeek == 53 {
					weeksLastYear = 53
				}
				currWeek += weeksLastYear
			}
			ws := weekStart(currYear, currWeek)
			res[i] = ws.Format("2006-01-02")
		}
		return res
	}

	switch *params.TimeRange {
	case "last_month":
		weekDates = getWeeks(4)
	case "last_6_months":
		weekDates = getWeeks(26)
	case "ytd":
		weekDates = getWeeks(isoWeek) // getWeeks returns newest-to-oldest, will be reversed at end
		log.Printf("YTD: Generated %d weeks with getWeeks(%d)", len(weekDates), isoWeek)
		if len(weekDates) > 0 {
			log.Printf("YTD weekDates[0] (newest): %s", weekDates[0])
			log.Printf("YTD weekDates[last] (oldest): %s", weekDates[len(weekDates)-1])
		}
	case "last_year":
		weekDates = getWeeks(52)
	case "last_2_years":
		weekDates = getWeeks(104)
	case "last_5_years":
		weekDates = getWeeks(260)
	case "max":
		first_protocol_date, last_protocol_date, err := s.getFirstAndLastAnalyzedDate()
		if err != nil {
			log.Printf("Failed to get first and last analyzed date: %v", err)
			http.Error(w, "Failed to get first and last analyzed date", http.StatusInternalServerError)
			return
		}
		numWeeks := int(last_protocol_date.Sub(first_protocol_date).Hours() / 24 / 7)
		weekDates = getWeeks(numWeeks)
	default:
		http.Error(w, "Invalid time_range parameter", http.StatusBadRequest)
		return
	}

	dataPoints := []api.AnalysisOverTimePoint{}

	for _, weekDate := range weekDates {
		// Build dynamic query based on filter parameters
		dataQuery := `
			SELECT COALESCE(AVG(ta.topic_share), 0) as topic_share, 
			       COALESCE(AVG(ta.avg_sentiment), 0) as avg_sentiment
			FROM get_topic_analytics($1::date, 20, NULL, NULL) AS ta
			WHERE 1=1`

		args := []interface{}{weekDate}
		argPos := 2

		if params.TopicId != nil {
			dataQuery += " AND ta.topic_id = $" + strconv.Itoa(argPos)
			args = append(args, *params.TopicId)
			argPos++
		}

		if params.PersonId != nil {
			// Note: This assumes get_topic_analytics can filter by person_id
			// You may need to adjust based on your actual schema
			dataQuery += " AND ta.person_id = $" + strconv.Itoa(argPos)
			args = append(args, *params.PersonId)
			argPos++
		}

		if params.GroupId != nil {
			dataQuery += " AND ta.group_id = $" + strconv.Itoa(argPos)
			args = append(args, *params.GroupId)
			argPos++
		}

		var topicShare, avgSentiment float64
		err := s.db.QueryRow(dataQuery, args...).Scan(&topicShare, &avgSentiment)
		if err != nil {
			log.Printf("Failed to query data points for week %s: %v", weekDate, err)
			// Continue with next week instead of failing entirely
			continue
		}

		periodDate, err := time.Parse("2006-01-02", weekDate)
		if err != nil {
			log.Printf("Failed to parse week date %s: %v", weekDate, err)
			continue
		}

		period := openapi_types.Date{Time: periodDate}
		relevance := float32(topicShare)
		sentiment := float32(avgSentiment)

		dataPoints = append(dataPoints, api.AnalysisOverTimePoint{
			Period:    &period,
			Relevance: &relevance,
			Sentiment: &sentiment,
		})
	}

	log.Printf("Generated %d data points BEFORE reversal", len(dataPoints))
	if len(dataPoints) > 0 {
		log.Printf("First point (before reverse): %s", dataPoints[0].Period.Time.Format("2006-01-02"))
		log.Printf("Last point (before reverse): %s", dataPoints[len(dataPoints)-1].Period.Time.Format("2006-01-02"))
	}

	// Reverse the dataPoints so oldest is first (left), newest is last (right)
	for i, j := 0, len(dataPoints)-1; i < j; i, j = i+1, j-1 {
		dataPoints[i], dataPoints[j] = dataPoints[j], dataPoints[i]
	}

	log.Printf("Generated %d data points AFTER reversal", len(dataPoints))
	if len(dataPoints) > 0 {
		log.Printf("First point (after reverse): %s", dataPoints[0].Period.Time.Format("2006-01-02"))
		log.Printf("Last point (after reverse): %s", dataPoints[len(dataPoints)-1].Period.Time.Format("2006-01-02"))
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
		SELECT t.id, t.name, t.updated, t.created, ta.topic_share, ta.avg_sentiment
		FROM get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta
		JOIN topics t ON t.id = ta.topic_id
		ORDER BY ta.topic_share DESC
		LIMIT $1 OFFSET $2
	`
	var rows []TopicWithAnalytics
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

func (s *Server) getFirstAndLastAnalyzedDate() (time.Time, time.Time, error) {
	first_protocol_date, last_protocol_date := time.Time{}, time.Time{}
	err := s.db.Get(&first_protocol_date, "SELECT MIN(date) FROM analyzed_protocols")
	if err != nil {
		log.Printf("Failed to get first protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	err = s.db.Get(&last_protocol_date, "SELECT MAX(date) FROM analyzed_protocols")
	if err != nil {
		log.Printf("Failed to get last protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	return first_protocol_date, last_protocol_date, nil
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

	// Get topic with analytics (relevance and sentiment)
	dataQuery := `
		SELECT t.id, t.name, t.updated, t.created, ta.topic_share, ta.avg_sentiment
		FROM topics t
		JOIN get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta ON ta.topic_id = t.id
		WHERE t.id = $1
	`
	var topicWithAnalytics TopicWithAnalytics
	err = s.db.Get(&topicWithAnalytics, dataQuery, topicID)
	if err != nil {
		log.Printf("Failed to query topic with analytics: %v", err)
		http.Error(w, "Failed to query topic with analytics", http.StatusInternalServerError)
		return
	}
	log.Printf("Found topic: %s (ID: %d, Relevance: %.4f, Sentiment: %.4f)", 
		topicWithAnalytics.Name, topicWithAnalytics.ID, topicWithAnalytics.TopicRelevance, topicWithAnalytics.AvgSentiment)

	// Convert to API Topic type
	idStr := strconv.Itoa(topicWithAnalytics.ID)
	relevance := float32(topicWithAnalytics.TopicRelevance)
	sentiment := float32(topicWithAnalytics.AvgSentiment)
	t := api.Topic{
		Id:        &idStr,
		Title:     &topicWithAnalytics.Name,
		Relevance: &relevance,
		Sentiment: &sentiment,
	}

	// Get party positions
	var analyticsPerParty = []PartyAnalytics{}
	log.Printf("Fetching party analytics for topic %d", topicID)
	err = s.db.Select(&analyticsPerParty, `SELECT * FROM get_topic_analytics_per_party($1, 30, $2)`, time.Now(), topicID)
	if err != nil {
		log.Printf("Failed to get analytics per party: %v", err)
	} else {
		log.Printf("Found %d party analytics rows", len(analyticsPerParty))
	}

	partyPositions := []api.PartyPosition{}
	for _, analytics := range analyticsPerParty {
		// Get party name from parliamentary_groups table
		var partyName string
		err := s.db.Get(&partyName, "SELECT name FROM parliamentary_groups WHERE id = $1", analytics.GroupID)
		if err != nil {
			log.Printf("Failed to get party name for group_id %d: %v", analytics.GroupID, err)
			continue
		}

		sentiment := float32(analytics.AvgSentiment * 100)
		relevance := float32(analytics.TopicRelevance) // Keep as 0-1, frontend will scale to 0-100
		log.Printf("Party: %s (group_id: %d), Sentiment: %.2f, Relevance: %.4f", partyName, analytics.GroupID, sentiment, relevance)
		partyPositions = append(partyPositions, api.PartyPosition{
			Party:     &partyName,
			Sentiment: &sentiment,
			Relevance: &relevance,
		})
	}
	log.Printf("Built %d party positions", len(partyPositions))

	// Get most active politicians (stakeholders)
	// The function now returns politicians per side based on ranking_type (pro/contra)
	var mostActivePoliticiansByRanking []PersonRanking
	numPoliticiansPerSide := 2
	err = s.db.Select(&mostActivePoliticiansByRanking,
		`SELECT * FROM get_most_active($1, $2, $3, $4)`,
		time.Now(), 30, numPoliticiansPerSide, topicID)
	if err != nil {
		log.Printf("Failed to get most active politicians: %v", err)
	}
	log.Printf("Found %d most active politicians for topic %d", len(mostActivePoliticiansByRanking), topicID)

	proPoliticians := []api.Politician{}
	contraPoliticians := []api.Politician{}

	for _, ranking := range mostActivePoliticiansByRanking {
		log.Printf("Processing politician ID %d with ranking_type: %s, score: %.2f",
			ranking.PersonID, ranking.RankingType, ranking.Score)

		// Get the role information for this person
		var role Role
		err := s.db.Get(&role,
			"SELECT * FROM roles WHERE person_id = $1 ORDER BY election_period DESC LIMIT 1",
			ranking.PersonID)
		if err != nil {
			log.Printf("Failed to get role for person_id %d: %v", ranking.PersonID, err)
			continue
		}

		// Get parliamentary group name
		var groupName string
		if role.GroupID.Valid {
			err = s.db.Get(&groupName,
				"SELECT name FROM parliamentary_groups WHERE id = $1",
				role.GroupID.Int64)
			if err != nil {
				log.Printf("Failed to get group name for group_id %d: %v", role.GroupID.Int64, err)
				groupName = "Unbekannt"
			}
		} else {
			groupName = "Fraktionslos"
		}

		// Build the full name
		fullName := role.FirstName + " " + role.NameSuffix.String + " " + role.LastName
		if role.Title.Valid && role.Title.String != "" {
			fullName = role.Title.String
		}

		// Convert to API Politician type
		personIDStr := strconv.Itoa(ranking.PersonID)
		contributionFactor := float32(ranking.Score)
		gender := "d"           // TODO: Get actual gender from database
		image := "null"         // TODO: Get actual image URL when available
		region := "Deutschland" // TODO: Get actual region from database
		roleStr := "mock"

		politician := api.Politician{
			ContributionFactor: &contributionFactor,
			Gender:             &gender,
			Id:                 &personIDStr,
			Image:              &image,
			Name:               &fullName,
			Party:              &groupName,
			Region:             &region,
			Role:               &roleStr,
			Similar:            nil, // TODO: Implement similar politicians
			TopTopics:          nil, // TODO: Implement top topics
			Volatility:         nil, // TODO: Implement volatility calculation
		}

		log.Printf("Added politician: %s (%s, ranking_type: %s)", fullName, groupName, ranking.RankingType)

		// Map ranking types: HIGHEST -> pro (most positive), LOWEST -> contra (most negative)
		if ranking.RankingType == "pro" || ranking.RankingType == "HIGHEST" {
			log.Printf("Adding %s to pro politicians", fullName)
			proPoliticians = append(proPoliticians, politician)
		} else if ranking.RankingType == "contra" || ranking.RankingType == "LOWEST" {
			log.Printf("Adding %s to contra politicians", fullName)
			contraPoliticians = append(contraPoliticians, politician)
		} else {
			log.Printf("Unknown ranking_type: %s for person_id %d", ranking.RankingType, ranking.PersonID)
		}
	}

	log.Printf("Final stakeholders: %d pro, %d contra", len(proPoliticians), len(contraPoliticians))

	stakeholders := struct {
		Contra *[]api.Politician `json:"contra,omitempty"`
		Pro    *[]api.Politician `json:"pro,omitempty"`
	}{
		Pro:    &proPoliticians,
		Contra: &contraPoliticians,
	}

	// Get speech snippets for this topic
	type SpeechWithRole struct {
		ActivityID   int            `db:"activity_id"`
		Text         string         `db:"text"`
		ProtocolDate time.Time      `db:"protocol_date"`
		FirstName    string         `db:"first_name"`
		LastName     string         `db:"last_name"`
		Title        sql.NullString `db:"title"`
		GroupName    sql.NullString `db:"group_name"`
		Sentiment    float64        `db:"sentiment"`
	}

	var speechData []SpeechWithRole
	err = s.db.Select(&speechData, `
		SELECT 
			a.id as activity_id,
			a.text,
			p.date as protocol_date,
			r.first_name,
			r.last_name,
			r.title,
			pg.name as group_name,
			am.sentiment_value as sentiment
		FROM activities a
		JOIN activity_mappings am ON am.activity_id = a.id
		JOIN roles r ON r.id = a.role_id
		JOIN protocols p ON p.id = a.protocol_id
		LEFT JOIN parliamentary_groups pg ON pg.id = r.group_id
		WHERE am.topic_id = $1
		ORDER BY p.date DESC
		LIMIT 5
	`, topicID)
	if err != nil {
		log.Printf("Failed to get speeches: %v", err)
	}
	log.Printf("Found %d speeches for topic %d", len(speechData), topicID)

	speeches := []api.SpeechSnippet{}
	for _, speech := range speechData {
		speakerName := speech.FirstName + " " + speech.LastName
		if speech.Title.Valid && speech.Title.String != "" {
			speakerName = speech.Title.String + " " + speakerName
		}

		partyName := "Fraktionslos"
		if speech.GroupName.Valid {
			partyName = speech.GroupName.String
		}

		// Map sentiment to enum
		var sentimentEnum api.SpeechSnippetSentiment
		if speech.Sentiment > 0.5 {
			sentimentEnum = api.StarkPositiv
		} else if speech.Sentiment > 0.2 {
			sentimentEnum = api.Positiv
		} else if speech.Sentiment < -0.5 {
			sentimentEnum = api.StarkNegativ
		} else if speech.Sentiment < -0.2 {
			sentimentEnum = api.Negativ
		} else {
			sentimentEnum = api.Neutral
		}

		activityIDStr := strconv.Itoa(speech.ActivityID)
		topicIDStr := strconv.Itoa(topicID)

		speeches = append(speeches, api.SpeechSnippet{
			Id:           &activityIDStr,
			TopicId:      &topicIDStr,
			Speaker:      &speakerName,
			Party:        &partyName,
			Text:         &speech.Text,
			Date:         &speech.ProtocolDate,
			Sentiment:    &sentimentEnum,
			FullSpeechId: &activityIDStr, // Same as ID for now
		})
	}
	log.Printf("Built %d speech snippets", len(speeches))

	// Note: trendData and positionData should be fetched separately via /analysis/time-series endpoint
	log.Printf("Building response with %d party positions, %d pro politicians, %d contra politicians",
		len(partyPositions), len(proPoliticians), len(contraPoliticians))

	resp := api.TopicDetail{
		Category:       t.Category,
		Id:             t.Id,
		Relevance:      t.Relevance,
		Sentiment:      t.Sentiment,
		Title:          t.Title,
		Trend:          nil, // Trend not provided by analytics query
		PartyPositions: &partyPositions,
		Stakeholders:   &stakeholders,
		Speeches:       &speeches,
	}

	log.Printf("Sending response for topic %d", topicID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

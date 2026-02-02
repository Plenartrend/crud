package service

import (
	"database/sql"
	"log"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type AnalyticsService struct {
	db *sqlx.DB
}

func NewAnalyticsService(db *sqlx.DB) *AnalyticsService {
	return &AnalyticsService{db: db}
}

func (s *AnalyticsService) GetAnalysisTimeSeries(timeRange api.TimeRangeFilter, topicID, personID, groupID *int) ([]api.AnalysisOverTimePoint, error) {
	log.Printf("[Analytics] GetAnalysisTimeSeries called with timeRange=%s, topicID=%v, personID=%v, groupID=%v",
		timeRange, topicID, personID, groupID)

	// Calculate start and end dates for the time range
	startDate, endDate, err := s.getDateRangeForTimeRange(string(timeRange))
	if err != nil {
		log.Printf("[Analytics] ERROR: Failed to get date range: %v", err)
		return nil, err
	}
	log.Printf("[Analytics] Date range: %s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	// Call optimized SQL function that processes all weeks in one query
	type TimeSeriesRow struct {
		WeekDate       time.Time `db:"week_date"`
		TopicRelevance float64   `db:"topic_relevance"`
		AvgSentiment   float64   `db:"avg_sentiment"`
	}

	var rows []TimeSeriesRow
	err = s.db.Select(&rows,
		`SELECT * FROM get_time_series_analytics($1, $2, 20, $3, $4, $5)`,
		startDate, endDate, topicID, personID, groupID)
	if err != nil {
		log.Printf("[Analytics] ERROR: Failed to query time series: %v", err)
		return nil, err
	}

	log.Printf("[Analytics] Retrieved %d data points from SQL", len(rows))

	// Convert SQL results to API response format
	dataPoints := make([]api.AnalysisOverTimePoint, 0, len(rows))
	for _, row := range rows {
		period := openapi_types.Date{Time: row.WeekDate}
		relevance := float32(row.TopicRelevance)
		sentiment := float32(row.AvgSentiment)

		dataPoints = append(dataPoints, api.AnalysisOverTimePoint{
			Period:    &period,
			Relevance: &relevance,
			Sentiment: &sentiment,
		})
	}

	if len(dataPoints) > 0 {
		log.Printf("[Analytics] First point: date=%s, relevance=%.4f, sentiment=%.4f",
			dataPoints[0].Period.Time.Format("2006-01-02"),
			*dataPoints[0].Relevance,
			*dataPoints[0].Sentiment)
		log.Printf("[Analytics] Last point: date=%s, relevance=%.4f, sentiment=%.4f",
			dataPoints[len(dataPoints)-1].Period.Time.Format("2006-01-02"),
			*dataPoints[len(dataPoints)-1].Relevance,
			*dataPoints[len(dataPoints)-1].Sentiment)
	}

	return dataPoints, nil
}

type ContributionFactor string

const (
	ContributionFactorHigh   ContributionFactor = "High"
	ContributionFactorMedium ContributionFactor = "Medium"
	ContributionFactorLow    ContributionFactor = "Low"
)

func getContributionFactor(factor float64) ContributionFactor {
	if factor > 70 {
		return ContributionFactorHigh
	} else if factor > 30 {
		return ContributionFactorMedium
	} else {
		return ContributionFactorLow
	}
}

// TODO: Maybe instead compare to average number of activities per politician instead?
func (s *AnalyticsService) GetContributionFactor(electionPeriod int, personIDs []int) (map[int]ContributionFactor, error) {
	if len(personIDs) == 0 {
		return make(map[int]ContributionFactor), nil
	}

	query := `
		WITH contributions AS (
			SELECT r.person_id, COUNT(*) AS cnt
			FROM activities a
			JOIN roles r ON r.id = a.role_id
			JOIN protocols p ON p.id = a.protocol_id
			WHERE p.election_period = $1 AND a.type LIKE 'Rede%'
			GROUP BY r.person_id
		), max_count AS (
			SELECT MAX(cnt) AS max_cnt FROM contributions
		)
		SELECT 
			c.person_id,
			(c.cnt::FLOAT / m.max_cnt::FLOAT) * 100 AS relative_percentage
		FROM contributions c
		CROSS JOIN max_count m
		WHERE c.person_id = ANY($2)
	`

	type Result struct {
		PersonID   int     `db:"person_id"`
		Percentage float64 `db:"relative_percentage"`
	}

	var results []Result
	err := s.db.Select(&results, query, electionPeriod, pq.Array(personIDs))
	if err != nil {
		log.Printf("Failed to get contribution factors: %v", err)
		return nil, err
	}

	log.Printf("Got %d contribution factor results for %d person IDs", len(results), len(personIDs))

	cfactors := make(map[int]ContributionFactor)
	for _, r := range results {
		cfactors[r.PersonID] = getContributionFactor(r.Percentage)
	}

	return cfactors, nil
}

func (s *AnalyticsService) GetVolatility(electionPeriod int, personIDs []int) (map[int]float64, error) {
	if len(personIDs) == 0 {
		return make(map[int]float64), nil
	}

	query := `SELECT * FROM get_volatility_for_election_period($1, $2, $3, $4)`

	type Result struct {
		PersonID   int     `db:"person_id"`
		Volatility float64 `db:"volatility"`
	}

	var results []Result
	err := s.db.Select(&results, query, electionPeriod, pq.Array(personIDs), nil, nil)
	if err != nil {
		log.Printf("Failed to get volatilities: %v", err)
		return nil, err
	}

	log.Printf("Got %d volatility results for %d person IDs", len(results), len(personIDs))

	volatilities := make(map[int]float64)
	for _, r := range results {
		volatilities[r.PersonID] = r.Volatility
	}

	return volatilities, nil
}

// TODO: Maybe only show speeches we actually analyzed?
func (s *AnalyticsService) GetNumberOfSpeeches(electionPeriod int, personIDs []int) (map[int]int, error) {
	if len(personIDs) == 0 {
		return make(map[int]int), nil
	}

	query := `
		SELECT r.person_id, COUNT(*) as count
		FROM activities a
		JOIN protocols p ON p.id = a.protocol_id
		JOIN roles r ON r.id = a.role_id
		WHERE p.election_period = $1 
		AND a.type LIKE 'Rede%'
		AND r.person_id = ANY($2)
		GROUP BY r.person_id
	`

	type Result struct {
		PersonID int `db:"person_id"`
		Count    int `db:"count"`
	}

	var results []Result
	err := s.db.Select(&results, query, electionPeriod, pq.Array(personIDs))
	if err != nil {
		log.Printf("Failed to get speech counts: %v", err)
		return nil, err
	}

	log.Printf("Got %d speech count results for %d person IDs", len(results), len(personIDs))

	counts := make(map[int]int)
	for _, r := range results {
		counts[r.PersonID] = r.Count
	}

	return counts, nil
}

func (s *AnalyticsService) GetTopTopics(electionPeriod int, personIDs []int, numOfTopics int) (map[int][]types.Topic, error) {
	if len(personIDs) == 0 {
		return make(map[int][]types.Topic), nil
	}

	log.Printf("Getting top topics for %d persons in election period %d", len(personIDs), electionPeriod)
	query := `
		WITH top_topics AS (
			SELECT r.person_id, am.topic_id, COUNT(*) AS activity_count,
				   ROW_NUMBER() OVER (PARTITION BY r.person_id ORDER BY COUNT(*) DESC) as rn
			FROM activity_mappings am
			JOIN activities a ON a.id = am.activity_id
			JOIN roles r ON r.id = a.role_id
			JOIN protocols p ON p.id = a.protocol_id
			WHERE r.person_id = ANY($1) AND p.election_period = $2
			GROUP BY r.person_id, am.topic_id
		)
		SELECT tt.person_id, t.id, t.name, t.updated, t.created
		FROM top_topics tt
		JOIN topics t ON t.id = tt.topic_id
		WHERE tt.rn <= $3
		ORDER BY tt.person_id, tt.rn
	`

	type Result struct {
		PersonID int `db:"person_id"`
		types.Topic
	}

	var results []Result
	err := s.db.Select(&results, query, pq.Array(personIDs), electionPeriod, numOfTopics)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		return nil, err
	}

	log.Printf("Got %d top topic results for %d person IDs", len(results), len(personIDs))

	topicsMap := make(map[int][]types.Topic)
	for _, r := range results {
		topicsMap[r.PersonID] = append(topicsMap[r.PersonID], r.Topic)
	}

	log.Printf("Found top topics for %d persons", len(topicsMap))
	return topicsMap, nil
}

func (s *AnalyticsService) getFirstAndLastAnalyzedDate() (time.Time, time.Time, error) {
	firstProtocolDate, lastProtocolDate := time.Time{}, time.Time{}
	err := s.db.Get(&firstProtocolDate, "SELECT MIN(date) FROM analysed_protocols")
	if err != nil {
		log.Printf("Failed to get first protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	err = s.db.Get(&lastProtocolDate, "SELECT MAX(date) FROM analysed_protocols")
	if err != nil {
		log.Printf("Failed to get last protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	return firstProtocolDate, lastProtocolDate, nil
}

// getDateRangeForTimeRange calculates start and end dates for time series queries
func (s *AnalyticsService) getDateRangeForTimeRange(timeRange string) (time.Time, time.Time, error) {
	now := time.Now()
	endDate := now
	var startDate time.Time

	switch timeRange {
	case "last_month":
		startDate = now.AddDate(0, -1, 0)
	case "last_6_months":
		startDate = now.AddDate(0, -6, 0)
	case "ytd":
		startDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "last_year":
		startDate = now.AddDate(-1, 0, 0)
	case "last_2_years":
		startDate = now.AddDate(-2, 0, 0)
	case "last_5_years":
		startDate = now.AddDate(-5, 0, 0)
	case "max":
		firstProtocolDate, lastProtocolDate, err := s.getFirstAndLastAnalyzedDate()
		if err != nil {
			log.Printf("Failed to get first and last analyzed date: %v", err)
			return time.Time{}, time.Time{}, err
		}
		return firstProtocolDate, lastProtocolDate, nil
	default:
		log.Printf("Invalid time_range parameter: %s", timeRange)
		return time.Time{}, time.Time{}, sql.ErrNoRows
	}

	return startDate, endDate, nil
}

func (s *AnalyticsService) GetTopicDetail(topicID int) (*api.TopicDetail, error) {
	dataQuery := `
		SELECT t.id, t.name, t.updated, t.created, ta.topic_relevance, ta.avg_sentiment
		FROM topics t
		JOIN get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta ON ta.topic_id = t.id
		WHERE t.id = $1
	`
	var topicWithAnalytics types.TopicWithAnalytics
	err := s.db.Get(&topicWithAnalytics, dataQuery, topicID)
	if err != nil {
		log.Printf("Failed to query topic with analytics: %v", err)
		return nil, err
	}
	log.Printf("Found topic: %s (ID: %d, Relevance: %.4f, Sentiment: %.4f)",
		topicWithAnalytics.Name, topicWithAnalytics.ID, topicWithAnalytics.TopicRelevance, topicWithAnalytics.AvgSentiment)

	idStr := strconv.Itoa(topicWithAnalytics.ID)
	relevance := float32(topicWithAnalytics.TopicRelevance)
	sentiment := float32(topicWithAnalytics.AvgSentiment)
	t := api.Topic{
		Id:        &idStr,
		Title:     &topicWithAnalytics.Name,
		Relevance: &relevance,
		Sentiment: &sentiment,
	}

	partyPositions, err := s.getPartyPositions(topicID)
	if err != nil {
		log.Printf("Failed to get party positions: %v", err)
	}

	proPoliticians, contraPoliticians, err := s.getStakeholders(topicID)
	if err != nil {
		log.Printf("Failed to get stakeholders: %v", err)
	}

	stakeholders := struct {
		Contra *[]api.Politician `json:"contra,omitempty"`
		Pro    *[]api.Politician `json:"pro,omitempty"`
	}{
		Pro:    &proPoliticians,
		Contra: &contraPoliticians,
	}

	speeches, err := s.getSpeechSnippets(topicID)
	if err != nil {
		log.Printf("Failed to get speeches: %v", err)
	}

	log.Printf("Building response with %d party positions, %d pro politicians, %d contra politicians",
		len(partyPositions), len(proPoliticians), len(contraPoliticians))

	resp := api.TopicDetail{
		Category:       t.Category,
		Id:             t.Id,
		Relevance:      t.Relevance,
		Sentiment:      t.Sentiment,
		Title:          t.Title,
		Trend:          nil,
		PartyPositions: &partyPositions,
		Stakeholders:   &stakeholders,
		Speeches:       &speeches,
	}

	return &resp, nil
}

func (s *AnalyticsService) getPartyPositions(topicID int) ([]api.PartyPosition, error) {
	var analyticsPerParty = []types.PartyAnalytics{}
	log.Printf("Fetching party analytics for topic %d", topicID)
	err := s.db.Select(&analyticsPerParty, `SELECT * FROM get_topic_analytics_per_party($1, 30, $2)`, time.Now(), topicID)
	if err != nil {
		log.Printf("Failed to get analytics per party: %v", err)
		return nil, err
	}
	log.Printf("Found %d party analytics rows", len(analyticsPerParty))

	partyPositions := []api.PartyPosition{}
	for idx, analytics := range analyticsPerParty {
		// Skip rows with NULL group_id (independent politicians without party affiliation)
		if !analytics.GroupID.Valid {
			log.Printf("[Analytics.PartyPositions] Row %d: Skipping NULL group_id (independent politician)", idx)
			continue
		}

		groupID := int(analytics.GroupID.Int64)
		log.Printf("[Analytics.PartyPositions] Row %d: GroupID=%d, TopicRelevance=%.4f, AvgSentiment=%.4f",
			idx, groupID, analytics.TopicRelevance, analytics.AvgSentiment)

		var partyName string
		err := s.db.Get(&partyName, "SELECT name FROM parliamentary_groups WHERE id = $1", groupID)
		if err != nil {
			log.Printf("[Analytics.PartyPositions] ERROR: Failed to get party name for group_id %d: %v", groupID, err)
			continue
		}

		sentiment := float32(analytics.AvgSentiment * 100)
		relevance := float32(analytics.TopicRelevance)
		log.Printf("[Analytics.PartyPositions] Party '%s' (group_id=%d): Sentiment=%.2f, Relevance=%.4f",
			partyName, groupID, sentiment, relevance)
		partyPositions = append(partyPositions, api.PartyPosition{
			Party:     &partyName,
			Sentiment: &sentiment,
			Relevance: &relevance,
		})
	}

	log.Printf("[Analytics.PartyPositions] Built %d party positions", len(partyPositions))
	return partyPositions, nil
}

func (s *AnalyticsService) getStakeholders(topicID int) ([]api.Politician, []api.Politician, error) {
	var mostActivePoliticiansByRanking []types.PersonRanking
	numPoliticiansPerSide := 2
	err := s.db.Select(&mostActivePoliticiansByRanking,
		`SELECT * FROM get_most_active($1, $2, $3, $4)`,
		time.Now(), 30, numPoliticiansPerSide, topicID)
	if err != nil {
		log.Printf("Failed to get most active politicians: %v", err)
		return nil, nil, err
	}
	log.Printf("Found %d most active politicians for topic %d", len(mostActivePoliticiansByRanking), topicID)

	proPoliticians := []api.Politician{}
	contraPoliticians := []api.Politician{}

	for _, ranking := range mostActivePoliticiansByRanking {
		log.Printf("Processing politician ID %d with ranking_type: %s, score: %.2f",
			ranking.PersonID, ranking.RankingType, ranking.Score)

		var role types.Role
		err := s.db.Get(&role,
			"SELECT * FROM roles WHERE person_id = $1 ORDER BY election_period DESC LIMIT 1",
			ranking.PersonID)
		if err != nil {
			log.Printf("Failed to get role for person_id %d: %v", ranking.PersonID, err)
			continue
		}

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

		fullName := role.FirstName + " " + role.NameSuffix.String + " " + role.LastName
		if role.Title.Valid && role.Title.String != "" {
			fullName = role.Title.String
		}

		personIDStr := strconv.Itoa(ranking.PersonID)
		dummyContributionFactor := api.PoliticianContributionFactorMedium // Dummy value, not used in getStakeholders
		roleStr := "mock"                                                 //Not relevant here

		politician := api.Politician{
			ContributionFactor: &dummyContributionFactor,
			Id:                 &personIDStr,
			Name:               &fullName,
			Party:              &groupName,
			Role:               &roleStr,
			Similar:            nil,
			TopTopics:          nil,
			Volatility:         nil,
		}

		log.Printf("Added politician: %s (%s, ranking_type: %s)", fullName, groupName, ranking.RankingType)

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
	return proPoliticians, contraPoliticians, nil
}

func (s *AnalyticsService) getSpeechSnippets(topicID int) ([]api.SpeechSnippet, error) {
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
	err := s.db.Select(&speechData, `
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
		return nil, err
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
			FullSpeechId: &activityIDStr,
		})
	}
	log.Printf("Built %d speech snippets", len(speeches))
	return speeches, nil
}

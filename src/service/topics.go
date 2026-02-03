package service

import (
	"log"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type TopicsService struct {
	db             *sqlx.DB
	helpersService *HelpersService
}

func NewTopicsService(db *sqlx.DB, helpersService *HelpersService) *TopicsService {
	return &TopicsService{
		db:             db,
		helpersService: helpersService,
	}
}

func (s *TopicsService) GetTopics(pageSize int, offset int) ([]api.Topic, int, error) {

	var totalItems int
	err := s.db.Get(&totalItems, `
		SELECT COUNT(*) FROM get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta
		JOIN topic_clusters c ON c.id = ta.cluster_id
	`)
	if err != nil {
		log.Printf("Failed to count topics: %v", err)
		return nil, 0, err
	}

	dataQuery := `
		SELECT c.id, c.title as name, c.updated, c.created, ta.topic_relevance, ta.avg_sentiment
		FROM get_topic_analytics(CURRENT_DATE, 20, NULL, NULL) AS ta
		JOIN topic_clusters c ON c.id = ta.cluster_id
		ORDER BY ta.topic_relevance DESC
		LIMIT $1 OFFSET $2
	`
	var rows []types.TopicWithAnalytics
	err = s.db.Select(&rows, dataQuery, pageSize, offset)
	if err != nil {
		log.Printf("Failed to query topics: %v", err)
		return nil, 0, err
	}
	for _, row := range rows {
		log.Printf("Topic: %v", row.Name)
		log.Printf("TopicRelevance: %v", row.TopicRelevance)
		log.Printf("AvgSentiment: %v", row.AvgSentiment)
	}

	topic_result := make([]api.Topic, 0, len(rows))
	for _, row := range rows {
		idStr := strconv.Itoa(row.ID)
		rel := float32(row.TopicRelevance)
		sent := float32(row.AvgSentiment)
		topic_result = append(topic_result, api.Topic{
			Id:        &idStr,
			Title:     &row.Name,
			Relevance: &rel,
			Sentiment: &sent,
		})
	}

	return topic_result, totalItems, nil
}

func (s *TopicsService) GetTopicDetail(topicID int, groupID *int, personID *int, electionPeriod *int) (*api.TopicDetail, error) {
	dataQuery := `
	    WITH last_date_for_election_period AS (
			SELECT CASE 
				WHEN $4::int IS NULL THEN CURRENT_DATE 
				ELSE MAX(date)::date 
			END as last_date 
			FROM analysed_protocols 
			WHERE $4::int IS NULL OR election_period = $4::int
		)
		SELECT c.id, c.title as name, c.updated, c.created, ta.topic_relevance, ta.avg_sentiment
		FROM topic_clusters c
		JOIN get_topic_analytics((SELECT last_date FROM last_date_for_election_period), 20, $2, $3) AS ta ON ta.cluster_id = c.id
		WHERE c.id = $1
	`
	var topicWithAnalytics types.TopicWithAnalytics
	err := s.db.Get(&topicWithAnalytics, dataQuery, topicID, groupID, personID, electionPeriod)
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

	speeches, err := s.GetSpeechSnippets(&topicID, nil, nil, nil, 5)
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

func (s *TopicsService) GetAnalysisTimeSeries(timeRange api.TimeRangeFilter, topicID, personID, groupID *int) ([]api.AnalysisOverTimePoint, error) {
	log.Printf("[Analytics] GetAnalysisTimeSeries called with timeRange=%s, topicID=%v, personID=%v, groupID=%v",
		timeRange, topicID, personID, groupID)

	// Calculate start and end dates for the time range
	startDate, endDate, err := s.helpersService.GetDateRangeForTimeRange(string(timeRange), nil)
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

func (s *TopicsService) getPartyPositions(topicID int) ([]api.PartyPosition, error) {
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
			GroupId:   &groupID,
		})
	}

	log.Printf("[Analytics.PartyPositions] Built %d party positions", len(partyPositions))
	return partyPositions, nil
}

func (s *TopicsService) getStakeholders(topicID int) ([]api.Politician, []api.Politician, error) {
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

func (s *TopicsService) GetRelevanceTimeSeries(timeRange api.TimeRangeFilter, topicID, personID, groupID *int) ([]api.TrendDataPoint, error) {
	startDate, endDate, err := s.helpersService.GetDateRangeForTimeRange(string(timeRange), nil)
	if err != nil {
		log.Printf("Failed to get date range: %v", err)
		return nil, err
	}
	query := `
		SELECT * FROM get_relevance_time_series($1, $2, $3, $4)
	`
	var relevanceTimeSeries []api.TrendDataPoint
	err = s.db.Select(&relevanceTimeSeries, query, startDate, endDate, topicID, personID, groupID)
	if err != nil {
		log.Printf("Failed to get relevance time series: %v", err)
		return nil, err
	}
	return relevanceTimeSeries, nil
}

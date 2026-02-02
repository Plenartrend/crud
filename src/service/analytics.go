package service

import (
	"database/sql"
	"fmt"
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
	startDate, endDate, err := s.getDateRangeForTimeRange(string(timeRange), nil)
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

func getContributionFactor(percentileGroup int) ContributionFactor {
	if percentileGroup == 2 {
		return ContributionFactorHigh
	} else if percentileGroup == 1 {
		return ContributionFactorMedium
	} else if percentileGroup == 0 {
		return ContributionFactorLow
	} else {
		return ""
	}
}

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
		),
			percentiles AS (
				SELECT
					person_id,
					cnt,
					PERCENT_RANK() OVER (ORDER BY cnt) AS ranking
				FROM contributions
			)
		SELECT
			person_id,
			cnt AS speech_count,
			CASE
				WHEN ranking > 0.66 THEN 2
				WHEN ranking > 0.33 THEN 1
				ELSE 0
				END AS percentile_group
		FROM percentiles
		WHERE person_id = ANY($2)
		ORDER BY speech_count DESC
	`

	type Result struct {
		PersonID        int `db:"person_id"`
		SpeechCount     int `db:"speech_count"`
		PercentileGroup int `db:"percentile_group"`
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
		cfactors[r.PersonID] = getContributionFactor(r.PercentileGroup)
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
func (s *AnalyticsService) GetNumberOfSpeeches(electionPeriod int, personIDs []int, topicID *int) (map[int]int, error) {
	if len(personIDs) == 0 {
		return make(map[int]int), nil
	}

	query := `
		SELECT r.person_id, COUNT(*) as count
		FROM activities a
		JOIN protocols p ON p.id = a.protocol_id
		JOIN roles r ON r.id = a.role_id
		LEFT JOIN activity_mappings am ON am.activity_id = a.id
		WHERE p.election_period = $1 
		AND a.type LIKE 'Rede%'
		AND r.person_id = ANY($2)
		AND ($3::int IS NULL OR am.topic_id = $3::int)
		GROUP BY r.person_id
	`

	type Result struct {
		PersonID int `db:"person_id"`
		Count    int `db:"count"`
	}

	var results []Result
	err := s.db.Select(&results, query, electionPeriod, pq.Array(personIDs), topicID)
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
func (s *AnalyticsService) getDateRangeForTimeRange(timeRange string, endDate *time.Time) (time.Time, time.Time, error) {
	now := time.Now()
	if endDate == nil {
		endDate = &now
	}
	var startDate time.Time

	switch timeRange {
	case "last_month":
		startDate = endDate.AddDate(0, -1, 0)
	case "last_6_months":
		startDate = endDate.AddDate(0, -6, 0)
	case "ytd":
		startDate = time.Date(endDate.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "last_year":
		startDate = endDate.AddDate(-1, 0, 0)
	case "last_2_years":
		startDate = endDate.AddDate(-2, 0, 0)
	case "last_5_years":
		startDate = endDate.AddDate(-5, 0, 0)
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

	return startDate, *endDate, nil
}

func (s *AnalyticsService) GetTopicDetail(topicID int, groupID *int, personID *int) (*api.TopicDetail, error) {
	dataQuery := `
		SELECT t.id, t.name, t.updated, t.created, ta.topic_relevance, ta.avg_sentiment
		FROM topics t
		JOIN get_topic_analytics(CURRENT_DATE, 20, $2, $3) AS ta ON ta.topic_id = t.id
		WHERE t.id = $1
	`
	var topicWithAnalytics types.TopicWithAnalytics
	err := s.db.Get(&topicWithAnalytics, dataQuery, topicID, groupID, personID)
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

// TODO: Is this not simply getSpeeches?
func (s *AnalyticsService) GetSpeechSnippets(topicID *int, personID *int, groupId *int, electionPeriod *int, limit int) ([]api.SpeechSnippet, error) {
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
		SELECT DISTINCT ON (a.id)
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
		WHERE ($1::int IS NULL OR am.topic_id = $1::int)
		AND ($2::int IS NULL OR r.person_id = $2::int)
		AND ($3::int IS NULL OR r.group_id = $3::int)
		AND ($4::int IS NULL OR p.election_period = $4::int)
		ORDER BY a.id, p.date DESC
		LIMIT $5::int
	`, topicID, personID, groupId, electionPeriod, limit)
	if err != nil {
		log.Printf("Failed to get speeches: %v", err)
		return nil, err
	}
	log.Printf("Found %d speeches for topic %d, person %d, group %d", len(speechData), topicID, personID, groupId)

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
		var topicIDStr *string
		if topicID != nil {
			str := strconv.Itoa(*topicID)
			topicIDStr = &str
		}

		speeches = append(speeches, api.SpeechSnippet{
			Id:           &activityIDStr,
			TopicId:      topicIDStr,
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

func (s *AnalyticsService) GetActivityTimeSeries(timeRange api.TimeRangeFilter, personID, groupID *int) ([]api.TrendDataPoint, error) {
	startDate, endDate, err := s.getDateRangeForTimeRange(string(timeRange), nil)
	if err != nil {
		log.Printf("Failed to get date range: %v", err)
		return nil, err
	}

	type ActivityRow struct {
		MonthDate   string `db:"month_date"`
		SpeechCount int64  `db:"speech_count"`
	}

	query := `
		SELECT month_date::text as month_date, speech_count FROM get_time_series_activity($1::date, $2::date, $3::int, $4::int)
	`
	var rows []ActivityRow
	err = s.db.Select(&rows, query, startDate, endDate, personID, groupID)
	if err != nil {
		log.Printf("Failed to get activity time series: %v", err)
		return nil, err
	}

	// Convert to api.TrendDataPoint
	activityTimeSeries := make([]api.TrendDataPoint, len(rows))
	for i, row := range rows {
		date := row.MonthDate
		value := float32(row.SpeechCount)
		activityTimeSeries[i] = api.TrendDataPoint{
			Date:  &date,
			Value: &value,
		}
	}

	return activityTimeSeries, nil
}

func (s *AnalyticsService) GetRelevanceTimeSeries(timeRange api.TimeRangeFilter, topicID, personID, groupID *int) ([]api.TrendDataPoint, error) {
	startDate, endDate, err := s.getDateRangeForTimeRange(string(timeRange), nil)
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

func (s *AnalyticsService) GetPersonsWithSimilarSentiment(topicIDs []int, personID int, electionPeriod int, numOfPersons int) ([]api.Politician, error) {

	query := `
		WITH last_date_for_election_period AS (
			SELECT MAX(date)::date as last_date FROM analysed_protocols WHERE election_period = $1
		),
		this_person_topics AS (
			SELECT ta_this.topic_id, ta_this.avg_sentiment
			FROM get_topic_analytics(
				(SELECT last_date FROM last_date_for_election_period), 
				30::int, 
				NULL::int,
				$2::int
			) ta_this
			WHERE ta_this.topic_id = ANY($3::int[])
		),
		similar_persons AS (
			SELECT DISTINCT ta.person_id,
				AVG(ABS(ta.avg_sentiment - tpt.avg_sentiment)) as avg_sentiment_diff
			FROM get_topic_analytics_per_person(
				(SELECT last_date FROM last_date_for_election_period), 
				30::int, 
				NULL::int
			) ta
			JOIN this_person_topics tpt ON ta.topic_id = tpt.topic_id
			WHERE ta.person_id != $2::int
				AND ta.topic_id = ANY($3::int[])
			GROUP BY ta.person_id
			HAVING AVG(ABS(ta.avg_sentiment - tpt.avg_sentiment)) < 0.2
			ORDER BY avg_sentiment_diff ASC
			LIMIT $4::int
		)
		SELECT 
			p.id::text as id,
			COALESCE(r.title, '') as name,
			COALESCE(pg.name, '') as party
		FROM similar_persons sp
		JOIN persons p ON p.id = sp.person_id
		JOIN roles r ON r.person_id = p.id AND r.election_period = $1
		LEFT JOIN parliamentary_groups pg ON r.group_id = pg.id
	`

	var personsWithSimilarSentiment []api.Politician
	err := s.db.Select(&personsWithSimilarSentiment, query, electionPeriod, personID, pq.Array(topicIDs), numOfPersons)
	if err != nil {
		log.Printf("Failed to get persons with similar sentiment: %v", err)
		return nil, err
	}

	log.Printf("Found %d persons with similar sentiment for person %d", len(personsWithSimilarSentiment), personID)
	return personsWithSimilarSentiment, nil
}

func (s *AnalyticsService) GetMaxElectionPeriod(personID int) (int, error) {
	query := `
		SELECT MAX(election_period) as max_election_period
		FROM roles
		WHERE person_id = $1
	`
	var maxElectionPeriod int
	err := s.db.Get(&maxElectionPeriod, query, personID)
	if err != nil {
		log.Printf("Failed to get max election period: %v", err)
		return 0, err
	}
	return maxElectionPeriod, nil
}

func (s *AnalyticsService) GetActivePoliticians(electionPeriod int, limit int, mostActive bool) ([]api.ActivePolitician, error) {
	var orderBy string
	if mostActive {
		orderBy = "ORDER BY num_speeches DESC, word_count DESC"
	} else {
		orderBy = "ORDER BY num_speeches ASC, word_count ASC"
	}

	query := fmt.Sprintf(`
		SELECT 
			r.first_name || ' ' || r.last_name AS name,
			pg.name AS party,
			COUNT(DISTINCT a.id) AS num_speeches,
			COALESCE(SUM(array_length(string_to_array(TRIM(a.text), ' '), 1)), 0)::int AS word_count
		FROM roles r
		JOIN parliamentary_groups pg ON r.group_id = pg.id
		JOIN activities a ON a.role_id = r.id
		JOIN protocols p ON p.id = a.protocol_id
		WHERE p.election_period = $1
		AND a.type LIKE 'Rede%%'
		AND a.text IS NOT NULL
		AND TRIM(a.text) != ''
		AND pg.name IS NOT NULL
		GROUP BY r.person_id, r.first_name, r.last_name, pg.name
		HAVING COUNT(DISTINCT a.id) > 0
		%s
		LIMIT $2
	`, orderBy)

	type Result struct {
		Name        string `db:"name"`
		Party       string `db:"party"`
		NumSpeeches int    `db:"num_speeches"`
		WordCount   int    `db:"word_count"`
	}

	var results []Result
	err := s.db.Select(&results, query, electionPeriod, limit)
	if err != nil {
		log.Printf("Failed to get active politicians: %v", err)
		return nil, err
	}

	log.Printf("Got %d active politician results for election period %d", len(results), electionPeriod)

	activePoliticians := make([]api.ActivePolitician, len(results))
	for i, r := range results {
		activePoliticians[i] = api.ActivePolitician{
			Name:        r.Name,
			Party:       r.Party,
			NumSpeeches: r.NumSpeeches,
			WordCount:   r.WordCount,
		}
	}

	return activePoliticians, nil
}

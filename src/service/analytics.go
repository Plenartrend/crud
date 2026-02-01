package service

import (
	"database/sql"
	"log"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type AnalyticsService struct {
	db *sqlx.DB
}

func NewAnalyticsService(db *sqlx.DB) *AnalyticsService {
	return &AnalyticsService{db: db}
}

func (s *AnalyticsService) GetAnalysisTimeSeries(timeRange api.TimeRangeFilter, topicID, personID, groupID *int) ([]api.AnalysisOverTimePoint, error) {
	weekDates, err := s.getWeekDatesForTimeRange(string(timeRange))
	if err != nil {
		return nil, err
	}

	dataPoints := []api.AnalysisOverTimePoint{}

	for _, weekDate := range weekDates {
		dataQuery := `
			SELECT COALESCE(AVG(ta.topic_share), 0) as topic_share, 
			       COALESCE(AVG(ta.avg_sentiment), 0) as avg_sentiment
			FROM get_topic_analytics($1::date, 20, NULL, NULL) AS ta
			WHERE 1=1`

		args := []interface{}{weekDate}
		argPos := 2

		if topicID != nil {
			dataQuery += " AND ta.topic_id = $" + strconv.Itoa(argPos)
			args = append(args, *topicID)
			argPos++
		}

		if personID != nil {
			dataQuery += " AND ta.person_id = $" + strconv.Itoa(argPos)
			args = append(args, *personID)
			argPos++
		}

		if groupID != nil {
			dataQuery += " AND ta.group_id = $" + strconv.Itoa(argPos)
			args = append(args, *groupID)
			argPos++
		}

		var topicShare, avgSentiment float64
		err := s.db.QueryRow(dataQuery, args...).Scan(&topicShare, &avgSentiment)
		if err != nil {
			log.Printf("Failed to query data points for week %s: %v", weekDate, err)
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

	for i, j := 0, len(dataPoints)-1; i < j; i, j = i+1, j-1 {
		dataPoints[i], dataPoints[j] = dataPoints[j], dataPoints[i]
	}

	log.Printf("Generated %d data points AFTER reversal", len(dataPoints))
	if len(dataPoints) > 0 {
		log.Printf("First point (after reverse): %s", dataPoints[0].Period.Time.Format("2006-01-02"))
		log.Printf("Last point (after reverse): %s", dataPoints[len(dataPoints)-1].Period.Time.Format("2006-01-02"))
	}

	return dataPoints, nil
}

func (s *AnalyticsService) getWeekDatesForTimeRange(timeRange string) ([]string, error) {
	now := time.Now()
	isoYear, isoWeek := now.ISOWeek()

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

	var weekDates []string
	switch timeRange {
	case "last_month":
		weekDates = getWeeks(4)
	case "last_6_months":
		weekDates = getWeeks(26)
	case "ytd":
		weekDates = getWeeks(isoWeek)
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
		firstProtocolDate, lastProtocolDate, err := s.getFirstAndLastAnalyzedDate()
		if err != nil {
			log.Printf("Failed to get first and last analyzed date: %v", err)
			return nil, err
		}
		numWeeks := int(lastProtocolDate.Sub(firstProtocolDate).Hours() / 24 / 7)
		weekDates = getWeeks(numWeeks)
	default:
		log.Printf("Invalid time_range parameter: %s", timeRange)
		return nil, nil
	}

	return weekDates, nil
}

func (s *AnalyticsService) getFirstAndLastAnalyzedDate() (time.Time, time.Time, error) {
	firstProtocolDate, lastProtocolDate := time.Time{}, time.Time{}
	err := s.db.Get(&firstProtocolDate, "SELECT MIN(date) FROM analyzed_protocols")
	if err != nil {
		log.Printf("Failed to get first protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	err = s.db.Get(&lastProtocolDate, "SELECT MAX(date) FROM analyzed_protocols")
	if err != nil {
		log.Printf("Failed to get last protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	return firstProtocolDate, lastProtocolDate, nil
}

func (s *AnalyticsService) GetTopicDetail(topicID int) (*api.TopicDetail, error) {
	dataQuery := `
		SELECT t.id, t.name, t.updated, t.created, ta.topic_share, ta.avg_sentiment
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
	for _, analytics := range analyticsPerParty {
		var partyName string
		err := s.db.Get(&partyName, "SELECT name FROM parliamentary_groups WHERE id = $1", analytics.GroupID)
		if err != nil {
			log.Printf("Failed to get party name for group_id %d: %v", analytics.GroupID, err)
			continue
		}

		sentiment := float32(analytics.AvgSentiment * 100)
		relevance := float32(analytics.TopicRelevance)
		log.Printf("Party: %s (group_id: %d), Sentiment: %.2f, Relevance: %.4f", partyName, analytics.GroupID, sentiment, relevance)
		partyPositions = append(partyPositions, api.PartyPosition{
			Party:     &partyName,
			Sentiment: &sentiment,
			Relevance: &relevance,
		})
	}
	log.Printf("Built %d party positions", len(partyPositions))
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
		contributionFactor := float32(ranking.Score)
		gender := "d"
		image := "null"
		region := "Deutschland"
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

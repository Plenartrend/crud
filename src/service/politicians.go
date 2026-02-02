package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"sort"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type PoliticiansService struct {
	db             *sqlx.DB
	topicsService  *TopicsService
	helpersService *HelpersService
}

func NewPoliticiansService(db *sqlx.DB, topicsService *TopicsService, helpersService *HelpersService) *PoliticiansService {
	return &PoliticiansService{
		db:             db,
		topicsService:  topicsService,
		helpersService: helpersService,
	}
}

func (s *PoliticiansService) GetPoliticians(electionPeriod int, groupID *int, pageSize *int, offset int) ([]api.Politician, int, error) {
	politicians := []api.Politician{}
	log.Printf("Getting politicians for election period: %d, groupID: %v, pageSize: %d, offset: %d", electionPeriod, groupID, pageSize, offset)

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
	err := s.db.Get(&totalCount, s.db.Rebind(countQuery), electionPeriod, groupID)
	if err != nil {
		log.Printf("Failed to count politicians: %v", err)
		return nil, 0, err
	}

	log.Printf("Fetching politicians for election period: %d, groupID: %v, limit: %d, offset: %d", electionPeriod, groupID, pageSize, offset)

	type RoleWithFaction struct {
		types.Role
		FactionName sql.NullString `db:"faction_name"`
	}

	rolesWithFaction := []RoleWithFaction{}
	err = s.db.Select(&rolesWithFaction, s.db.Rebind(query), electionPeriod, pageSize, offset, groupID)
	if err != nil {
		log.Printf("Failed to query roles: %v", err)
		return nil, 0, err
	}

	personIDs := make([]int, len(rolesWithFaction))
	for i, roleWithFaction := range rolesWithFaction {
		personIDs[i] = roleWithFaction.Role.PersonID
	}

	cfactors, err := s.GetContributionFactor(electionPeriod, personIDs)
	if err != nil {
		log.Printf("Failed to get contribution factors: %v", err)
		cfactors = make(map[int]ContributionFactor)
	}

	volatilities, err := s.GetVolatility(electionPeriod, personIDs)
	if err != nil {
		log.Printf("Failed to get volatilities: %v", err)
		volatilities = make(map[int]float64)
	}

	topTopicsMap, err := s.GetTopTopics(electionPeriod, personIDs, 3)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		topTopicsMap = make(map[int][]types.Topic)
	}

	speechCounts, err := s.GetNumberOfSpeeches(electionPeriod, personIDs, nil)
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
		apiContributionFactor := contributionFactorToEnum(string(cfactor))

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

func (s *PoliticiansService) GetPoliticianDetail(personID int, electionPeriod int) (*api.PoliticianDetail, error) {
	query := `
	SELECT r.*, pg.name as faction_name
		FROM roles r
		LEFT JOIN parliamentary_groups pg ON r.group_id = pg.id
		WHERE r.person_id = $1
		AND r.election_period = $2
	LIMIT 1
	`

	type RoleWithFaction struct {
		types.Role
		FactionName sql.NullString `db:"faction_name"`
	}

	var roleWithFaction RoleWithFaction
	err := s.db.Get(&roleWithFaction, s.db.Rebind(query), personID, electionPeriod)
	if err != nil {
		log.Printf("Failed to query politician: %v", err)
		return nil, err
	}

	cfactors, err := s.GetContributionFactor(electionPeriod, []int{roleWithFaction.PersonID})
	if err != nil {
		log.Printf("Failed to get contribution factor: %v", err)
		cfactors = make(map[int]ContributionFactor)
	}
	contributionFactor := cfactors[roleWithFaction.PersonID]
	if contributionFactor == "" {
		contributionFactor = "low"
	}

	volatilities, err := s.GetVolatility(electionPeriod, []int{roleWithFaction.PersonID})
	if err != nil {
		log.Printf("Failed to get volatility: %v", err)
		volatilities = make(map[int]float64)
	}
	volatility := volatilities[roleWithFaction.PersonID]

	numsSpeeches, err := s.GetNumberOfSpeeches(electionPeriod, []int{roleWithFaction.PersonID}, nil)
	if err != nil {
		log.Printf("Failed to get number of speeches: %v", err)
		numsSpeeches = make(map[int]int)
	}
	numSpeeches := numsSpeeches[roleWithFaction.PersonID]

	topTopics, err := s.GetTopTopics(electionPeriod, []int{roleWithFaction.PersonID}, 4)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		topTopics = make(map[int][]types.Topic)
	}
	topTopicsList := topTopics[roleWithFaction.PersonID]

	topTopicsWithSentiment := make(map[int]api.TopicDetail)
	for _, topic := range topTopicsList {
		personIDPtr := &roleWithFaction.PersonID
		topicDetail, err := s.topicsService.GetTopicDetail(topic.ID, nil, personIDPtr, &electionPeriod)
		if err != nil {
			log.Printf("Failed to get topic detail: %v", err)
			continue
		}
		topTopicsWithSentiment[topic.ID] = *topicDetail
	}

	recentSpeeches, err := s.topicsService.GetSpeechSnippets(nil, &roleWithFaction.PersonID, nil, &electionPeriod, 5)
	if err != nil {
		log.Printf("Failed to get recent speeches: %v", err)
		recentSpeeches = []api.SpeechSnippet{}
	}

	// Convert to API types (pointers)
	idStr := strconv.Itoa(roleWithFaction.PersonID)
	var name string
	if roleWithFaction.Title.Valid && roleWithFaction.Title.String != "" {
		name = roleWithFaction.Title.String
	} else {
		var nameSuffix string
		if roleWithFaction.NameSuffix.Valid && roleWithFaction.NameSuffix.String != "" {
			nameSuffix = roleWithFaction.NameSuffix.String + " "
		}
		name = roleWithFaction.FirstName + " " + nameSuffix + roleWithFaction.LastName
	}
	party := ""
	if roleWithFaction.FactionName.Valid {
		party = roleWithFaction.FactionName.String
	}
	roleStr := ""
	if roleWithFaction.RoleName.Valid {
		roleStr = roleWithFaction.RoleName.String
	}
	volatilityStr := fmt.Sprintf("%.2f", volatility)

	// Convert contribution factor to API enum type
	apiContributionFactor := contributionFactorToEnum(string(contributionFactor))

	// Convert top topics to API format with sentiment and speech count
	apiTopTopics := make([]api.TopTopic, 0, len(topTopicsList))
	for _, topic := range topTopicsList {
		topicName := topic.Name
		stance := "neutral"
		var sentimentValue *float32

		// Get sentiment from topTopicsWithSentiment if available
		if topicDetail, ok := topTopicsWithSentiment[topic.ID]; ok && topicDetail.Sentiment != nil {
			sentiment := *topicDetail.Sentiment
			sentimentValue = &sentiment
		}

		// Get speech count for this topic
		topicIDPtr := &topic.ID
		speechCountMap, err := s.GetNumberOfSpeeches(electionPeriod, []int{roleWithFaction.PersonID}, topicIDPtr)
		if err != nil {
			log.Printf("Failed to get speech count for topic %d: %v", topic.ID, err)
		}
		var speechCount *int
		if count, ok := speechCountMap[roleWithFaction.PersonID]; ok {
			speechCount = &count
		}

		apiTopTopics = append(apiTopTopics, api.TopTopic{
			Topic:       &topicName,
			Stance:      &stance,
			Sentiment:   sentimentValue,
			SpeechCount: speechCount,
		})
	}

	// Cast to PoliticianDetailContributionFactor for PoliticianDetail type
	apiDetailContributionFactor := api.PoliticianDetailContributionFactor(apiContributionFactor)

	politicianDetail := &api.PoliticianDetail{
		Id:                 &idStr,
		Name:               &name,
		Party:              &party,
		Role:               &roleStr,
		Volatility:         &volatilityStr,
		ContributionFactor: &apiDetailContributionFactor,
		NumSpeeches:        &numSpeeches,
		TopTopics:          &apiTopTopics,
		Speeches:           &recentSpeeches,
	}

	return politicianDetail, nil
}

func (s *PoliticiansService) GetSimilarPoliticians(personID int, electionPeriod int) ([]api.Politician, error) {
	// Get top topics for this politician
	topTopics, err := s.GetTopTopics(electionPeriod, []int{personID}, 4)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		return []api.Politician{}, nil
	}
	topTopicsList := topTopics[personID]

	topTopicsWithSentiment := make(map[int]api.TopicDetail)
	for _, topic := range topTopicsList {
		personIDPtr := &personID
		topicDetail, err := s.topicsService.GetTopicDetail(topic.ID, nil, personIDPtr, &electionPeriod)
		if err != nil {
			log.Printf("Failed to get topic detail: %v", err)
			continue
		}
		topTopicsWithSentiment[topic.ID] = *topicDetail
	}

	// Extract topic IDs for similar sentiment query
	topicIDsForSimilar := make([]int, 0, len(topTopicsWithSentiment))
	for topicID := range topTopicsWithSentiment {
		topicIDsForSimilar = append(topicIDsForSimilar, topicID)
	}

	similar, err := s.GetPoliticiansWithSimilarSentiment(topicIDsForSimilar, personID, electionPeriod, 4)
	if err != nil {
		log.Printf("Failed to get persons with similar sentiment: %v", err)
		return []api.Politician{}, nil
	}

	return similar, nil
}

type TfidfEntry struct {
	DocID int     `json:"doc_id"`
	Term  string  `json:"term"`
	Tfidf float64 `json:"tfidf"`
}

func (s *PoliticiansService) GetPoliticianWordcloud(personID int) ([]api.WordCloudItem, error) {
	query := `
		SELECT tfidf_vector
		FROM activity_tfidf
		WHERE person_id = $1
	`

	var tfidfVectorJSON string
	err := s.db.Get(&tfidfVectorJSON, query, personID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("No tfidf data found for person_id %d", personID)
			return []api.WordCloudItem{}, nil
		}
		log.Printf("Failed to query tfidf_vector: %v", err)
		return nil, err
	}

	// Parse JSON array
	var tfidfEntries []TfidfEntry
	err = json.Unmarshal([]byte(tfidfVectorJSON), &tfidfEntries)
	if err != nil {
		log.Printf("Failed to parse tfidf_vector JSON: %v", err)
		return nil, err
	}

	// Sort by tfidf value (descending)
	sort.Slice(tfidfEntries, func(i, j int) bool {
		return tfidfEntries[i].Tfidf > tfidfEntries[j].Tfidf
	})

	// Take top 10
	maxItems := 10
	if len(tfidfEntries) < maxItems {
		maxItems = len(tfidfEntries)
	}

	wordcloudItems := make([]api.WordCloudItem, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		entry := tfidfEntries[i]
		wordcloudItems = append(wordcloudItems, api.WordCloudItem{
			Word:   entry.Term,
			Weight: float32(entry.Tfidf),
		})
	}

	return wordcloudItems, nil
}

func contributionFactorToEnum(cfactor string) api.PoliticianContributionFactor {
	switch strings.ToLower(cfactor) {
	case "high":
		return api.PoliticianContributionFactorHigh
	case "medium":
		return api.PoliticianContributionFactorMedium
	case "low":
		return api.PoliticianContributionFactorLow
	default:
		return api.PoliticianContributionFactorLow
	}
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

func (s *PoliticiansService) GetContributionFactor(electionPeriod int, personIDs []int) (map[int]ContributionFactor, error) {
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

func (s *PoliticiansService) GetVolatility(electionPeriod int, personIDs []int) (map[int]float64, error) {
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
func (s *PoliticiansService) GetNumberOfSpeeches(electionPeriod int, personIDs []int, topicID *int) (map[int]int, error) {
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

func (s *PoliticiansService) GetTopTopics(electionPeriod int, personIDs []int, numOfTopics int) (map[int][]types.Topic, error) {
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

func (s *PoliticiansService) GetPoliticiansWithSimilarSentiment(topicIDs []int, personID int, electionPeriod int, numOfPersons int) ([]api.Politician, error) {

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
			COALESCE(r.title, r.first_name || ' ' || r.last_name, '') as name,
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

func (s *PoliticiansService) GetActivityTimeSeries(timeRange api.TimeRangeFilter, personID, groupID *int) ([]api.TrendDataPoint, error) {
	startDate, endDate, err := s.helpersService.GetDateRangeForTimeRange(string(timeRange), nil)
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

func (s *PoliticiansService) GetActivePoliticians(electionPeriod int, limit int, mostActive bool) ([]api.ActivePolitician, error) {
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

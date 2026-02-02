package service

import (
	"database/sql"
	"fmt"
	"log"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

type PoliticiansService struct {
	db               *sqlx.DB
	analyticsService *AnalyticsService
}

func NewPoliticiansService(db *sqlx.DB, analyticsService *AnalyticsService) *PoliticiansService {
	return &PoliticiansService{
		db:               db,
		analyticsService: analyticsService,
	}
}

func (s *PoliticiansService) GetPoliticians(electionPeriod *int, groupID *int, pageSize *int, offset int) ([]api.Politician, int, error) {
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
		cfactors = make(map[int]ContributionFactor)
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

	speechCounts, err := s.analyticsService.GetNumberOfSpeeches(period, personIDs, nil)
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

	cfactors, err := s.analyticsService.GetContributionFactor(electionPeriod, []int{roleWithFaction.PersonID})
	if err != nil {
		log.Printf("Failed to get contribution factor: %v", err)
		cfactors = make(map[int]ContributionFactor)
	}
	contributionFactor := cfactors[roleWithFaction.PersonID]
	if contributionFactor == "" {
		contributionFactor = "low"
	}

	volatilities, err := s.analyticsService.GetVolatility(electionPeriod, []int{roleWithFaction.PersonID})
	if err != nil {
		log.Printf("Failed to get volatility: %v", err)
		volatilities = make(map[int]float64)
	}
	volatility := volatilities[roleWithFaction.PersonID]

	numsSpeeches, err := s.analyticsService.GetNumberOfSpeeches(electionPeriod, []int{roleWithFaction.PersonID}, nil)
	if err != nil {
		log.Printf("Failed to get number of speeches: %v", err)
		numsSpeeches = make(map[int]int)
	}
	numSpeeches := numsSpeeches[roleWithFaction.PersonID]

	topTopics, err := s.analyticsService.GetTopTopics(electionPeriod, []int{roleWithFaction.PersonID}, 4)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		topTopics = make(map[int][]types.Topic)
	}
	topTopicsList := topTopics[roleWithFaction.PersonID]

	topTopicsWithSentiment := make(map[int]api.TopicDetail)
	for _, topic := range topTopicsList {
		personIDPtr := &roleWithFaction.PersonID
		topicDetail, err := s.analyticsService.GetTopicDetail(topic.ID, nil, personIDPtr)
		if err != nil {
			log.Printf("Failed to get topic detail: %v", err)
			continue
		}
		topTopicsWithSentiment[topic.ID] = *topicDetail
	}

	recentSpeeches, err := s.analyticsService.GetSpeechSnippets(nil, &roleWithFaction.PersonID, nil, &electionPeriod, 5)
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
		speechCountMap, err := s.analyticsService.GetNumberOfSpeeches(electionPeriod, []int{roleWithFaction.PersonID}, topicIDPtr)
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
	topTopics, err := s.analyticsService.GetTopTopics(electionPeriod, []int{personID}, 4)
	if err != nil {
		log.Printf("Failed to get top topics: %v", err)
		return []api.Politician{}, nil
	}
	topTopicsList := topTopics[personID]

	topTopicsWithSentiment := make(map[int]api.TopicDetail)
	for _, topic := range topTopicsList {
		personIDPtr := &personID
		topicDetail, err := s.analyticsService.GetTopicDetail(topic.ID, nil, personIDPtr)
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

	similar, err := s.analyticsService.GetPersonsWithSimilarSentiment(topicIDsForSimilar, personID, electionPeriod, 4)
	if err != nil {
		log.Printf("Failed to get persons with similar sentiment: %v", err)
		return []api.Politician{}, nil
	}

	return similar, nil
}

func (s *PoliticiansService) GetPoliticianActivity(personID int, electionPeriod int, timeRange api.TimeRangeFilter) ([]api.TrendDataPoint, error) {
	activityTimeSeries, err := s.analyticsService.GetActivityTimeSeries(timeRange, &personID, nil)
	if err != nil {
		log.Printf("Failed to get activity time series: %v", err)
		return []api.TrendDataPoint{}, nil
	}

	return activityTimeSeries, nil
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

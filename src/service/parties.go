package service

import (
	"fmt"
	"log"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type PartyService struct {
	db             *sqlx.DB
	helpersService *HelpersService
	topicsService  *TopicsService
}

func NewPartyService(db *sqlx.DB, helpersService *HelpersService, topicsService *TopicsService) *PartyService {
	return &PartyService{
		db:             db,
		helpersService: helpersService,
		topicsService:  topicsService,
	}
}

func (p *PartyService) GetPartiesContributionFactorStrings(partyIds []int, electionPeriod int) (map[int]string, error) {
	type Result struct {
		PartyID     int     `db:"group_id"`
		SpeechCount int     `db:"speech_count"`
		Ranking     float64 `db:"ranking"`
	}

	if len(partyIds) == 0 {
		return make(map[int]string), nil
	}

	var results []Result

	err := p.db.Select(&results, `
		WITH contributions AS (
			SELECT r.group_id, COUNT(*) AS cnt
			FROM activities a
				JOIN roles r ON r.id = a.role_id
				JOIN protocols p ON p.id = a.protocol_id
			WHERE p.election_period = $2 AND a.type LIKE 'Rede%'
			GROUP BY r.group_id
		),
		percentiles AS (
			SELECT group_id, cnt, PERCENT_RANK() OVER (ORDER BY cnt) AS ranking
			FROM contributions
		)
		SELECT group_id, cnt AS speech_count, ranking
		FROM percentiles
		WHERE group_id = ANY($1)
		ORDER BY speech_count DESC
	`, pq.Array(partyIds), electionPeriod)

	if err != nil {
		return nil, fmt.Errorf("failed to get contribution factors: %v", err)
	}

	contributionFactorStrings := make(map[int]string)
	for _, r := range results {
		switch {
		case r.Ranking > 0.66:
			contributionFactorStrings[r.PartyID] = "high"
		case r.Ranking > 0.33:
			contributionFactorStrings[r.PartyID] = "medium"
		default:
			contributionFactorStrings[r.PartyID] = "low"
		}
	}
	return contributionFactorStrings, nil
}

func (p *PartyService) GetNumSpeechesOfParties(partyIDs []int, electionPeriod int) (map[int]int, error) {
	type SpeechCount struct {
		PartyID     int `db:"group_id"`
		SpeechCount int `db:"speech_count"`
	}

	var speechCounts []SpeechCount

	err := p.db.Select(&speechCounts, `
		SELECT group_id, COUNT(*) as speech_count
		FROM activities a, roles r
		WHERE a.role_id = r.id
			AND a.type LIKE 'Rede%'
			AND r.group_id = ANY($1)
			AND r.election_period = $2
		GROUP BY group_id
	`, pq.Array(partyIDs), electionPeriod)

	if err != nil {
		return nil, fmt.Errorf("failed to count speeches for party IDs %v: %v", partyIDs, err)
	}

	speechCountMap := make(map[int]int)
	for _, sc := range speechCounts {
		speechCountMap[sc.PartyID] = sc.SpeechCount
	}

	return speechCountMap, nil
}

func (p *PartyService) GetTopTopicsOfParties(partyIDs []int, electionPeriod int, numTopics int) (map[int][]api.TopicSummary, error) {
	type Result struct {
		PartyID int `db:"group_id"`
		types.Topic
	}

	if len(partyIDs) == 0 {
		return make(map[int][]api.TopicSummary), nil
	}

	query := `
		WITH top_topics AS (
			SELECT r.group_id, am.topic_id, COUNT(*) AS activity_count,
				   ROW_NUMBER() OVER (PARTITION BY r.group_id ORDER BY COUNT(*) DESC) as rn
			FROM activity_mappings am
			JOIN activities a ON a.id = am.activity_id
			JOIN roles r ON r.id = a.role_id
			JOIN protocols p ON p.id = a.protocol_id
			WHERE r.group_id = ANY($1) AND p.election_period = $2
			GROUP BY r.group_id, am.topic_id
		)
		SELECT tt.group_id, t.id, t.name, t.updated, t.created
		FROM top_topics tt
		JOIN topics t ON t.id = tt.topic_id
		WHERE tt.rn <= $3
		ORDER BY tt.group_id, tt.rn
	`

	var results []Result
	err := p.db.Select(&results, query, pq.Array(partyIDs), electionPeriod, numTopics)
	if err != nil {
		return nil, fmt.Errorf("Failed to get top topics: %v", err)
	}

	topicsMap := make(map[int][]api.TopicSummary)
	for _, r := range results {
		topicsMap[r.PartyID] = append(topicsMap[r.PartyID], api.TopicSummary{
			Id:    &r.ID,
			Title: &r.Name,
		})
	}
	return topicsMap, nil
}

func (p *PartyService) GetPartyVolatilities(partyIDs []int, electionPeriod int) (map[int]float64, error) {
	type Result struct {
		PartyID    int     `db:"group_id"`
		Volatility float64 `db:"volatility"`
	}

	if len(partyIDs) == 0 {
		return make(map[int]float64), nil
	}

	var results []Result
	err := p.db.Select(&results, "SELECT * FROM get_volatility_for_election_period_groups($1, $2, $3)", electionPeriod, pq.Array(partyIDs), nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to get volatilities: %v", err)
	}

	volatilities := make(map[int]float64)
	for _, r := range results {
		volatilities[r.PartyID] = r.Volatility
	}

	return volatilities, nil
}

func (p *PartyService) GetParties(electionPeriod int) ([]api.Party, error) {
	var parties []types.ParliamentaryGroup
	err := p.db.Select(&parties, `
		SELECT * FROM parliamentary_groups
		WHERE EXISTS (
			SELECT * FROM roles
			WHERE group_id = parliamentary_groups.id
			AND election_period = $1
		)
	`, electionPeriod)
	if err != nil {
		return nil, fmt.Errorf("Failed to query parliamentary groups: %v", err)
	}

	apiParties := make([]api.Party, len(parties))
	partyIds := make([]int, len(parties))
	for i, party := range parties {
		partyIds[i] = party.ID
	}

	contributionFactorStrings, err := p.GetPartiesContributionFactorStrings(partyIds, electionPeriod)
	if err != nil {
		return nil, fmt.Errorf("Failed to get contribution factor for party IDs %v: %v", partyIds, err)
	}

	numSpeeches, err := p.GetNumSpeechesOfParties(partyIds, electionPeriod)
	if err != nil {
		return nil, fmt.Errorf("Failed to get number of speeches for party IDs %v: %v", partyIds, err)
	}

	topTopics, err := p.GetTopTopicsOfParties(partyIds, electionPeriod, 3)
	if err != nil {
		return nil, fmt.Errorf("Failed to get top topics for party IDs %v: %v", partyIds, err)
	}

	volatilities, err := p.GetPartyVolatilities(partyIds, electionPeriod)
	if err != nil {
		return nil, fmt.Errorf("Failed to get volatility for party IDs %v: %v", partyIds, err)
	}

	for i, party := range parties {
		contributionFactorValue := api.PartyContributionFactor(contributionFactorStrings[party.ID])
		partyIdStr := strconv.Itoa(party.ID)
		numSpeechesValue := numSpeeches[party.ID]
		topTopicsValue := topTopics[party.ID]
		volatilitiesValue := fmt.Sprintf("%.2f", volatilities[party.ID])

		apiParties[i] = api.Party{
			ContributionFactor: &contributionFactorValue,
			Id:                 &partyIdStr,
			Name:               &party.Name.String,
			NumSpeeches:        &numSpeechesValue,
			TopTopics:          &topTopicsValue,
			Volatility:         &volatilitiesValue,
		}
	}

	return apiParties, nil
}

func (p *PartyService) GetActivityDataForParty(partyID int, timeRange api.TimeRangeFilter) ([]api.TrendDataPoint, error) {
	type ActivityDataPoint struct {
		MonthDate   string `db:"month_date"`
		SpeechCount int    `db:"speech_count"`
	}

	startDate, endDate, err := p.helpersService.GetDateRangeForTimeRange(string(timeRange), nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to get date range for time range %s: %v", timeRange, err)
	}

	var dataPoints []ActivityDataPoint

	err = p.db.Select(&dataPoints, `
		SELECT month_date::text AS month_date, speech_count FROM get_time_series_activity($1::date, $2::date, $3::int, $4::int)
	`, startDate, endDate, nil, partyID)

	if err != nil {
		return nil, fmt.Errorf("Failed to get activity data for party ID %d: %v", partyID, err)
	}

	trendDataPoints := make([]api.TrendDataPoint, len(dataPoints))
	for i, dp := range dataPoints {
		value := float32(dp.SpeechCount)
		trendDataPoints[i] = api.TrendDataPoint{
			Date:  &dp.MonthDate,
			Value: &value,
		}
	}

	log.Printf("[GetActivityDataForParty] Returning %v data points", len(trendDataPoints))

	return trendDataPoints, nil
}

func (p *PartyService) GetPartyMembers(partyID int, electionPeriod int) ([]api.Politician, error) {
	var members []types.Role
	err := p.db.Select(&members, "SELECT * FROM roles WHERE group_id = $1 AND election_period = $2", partyID, electionPeriod)
	if err != nil {
		return nil, fmt.Errorf("Failed to query party members: %v", err)
	}

	apiMembers := make([]api.Politician, 0, len(members))
	for _, member := range members {
		personIDStr := strconv.Itoa(member.PersonID)

		fullName := member.FirstName + " " + member.NameSuffix.String + " " + member.LastName
		if member.Title.Valid && member.Title.String != "" {
			fullName = member.Title.String
		}

		apiMember := api.Politician{
			ContributionFactor: nil,
			Id:                 &personIDStr,
			Name:               &fullName,
			Party:              nil,
			Role:               &member.RoleName.String,
			TopTopics:          nil,
			Volatility:         nil,
		}
		apiMembers = append(apiMembers, apiMember)
	}

	return apiMembers, nil
}

func (p *PartyService) GetPrintedPapersForParty(partyID int, electionPeriod int) ([]api.PrintedPaper, error) {
	var printedPapers []types.PrintedPaper

	err := p.db.Select(&printedPapers, `
		SELECT pp.*
		FROM printed_papers pp
		WHERE EXISTS (
			SELECT 1
			FROM activities a, roles r
			WHERE pp.id = a.printed_paper_id
				AND a.role_id = r.id
				AND r.group_id = $1
				AND r.election_period = $2
		)
	`, partyID, electionPeriod)

	if err != nil {
		return nil, fmt.Errorf("Failed to query printed papers: %v", err)
	}

	apiPrintedPapers := make([]api.PrintedPaper, len(printedPapers))

	for i, pp := range printedPapers {
		idStr := strconv.Itoa(pp.ID)

		apiPrintedPapers[i] = api.PrintedPaper{
			Date:   &pp.Date,
			Id:     &idStr,
			Number: &pp.DocumentNumber,
			Title:  &pp.Title,
			Type:   &pp.Type,
		}
	}

	return apiPrintedPapers, nil
}

func (p *PartyService) GetPartiesId(partyID int, electionPeriod int, timeRange api.TimeRangeFilter) (api.PartyDetail, error) {
	var party types.ParliamentaryGroup
	err := p.db.Get(&party, "SELECT * FROM parliamentary_groups WHERE id = $1", partyID)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to query parliamentary group: %v", err)
	}

	activityData, err := p.GetActivityDataForParty(partyID, timeRange)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get activity data for party ID %d: %v", partyID, err)
	}

	contributionFactorStrings, err := p.GetPartiesContributionFactorStrings([]int{partyID}, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get contribution factor for party ID %d: %v", partyID, err)
	}
	contributionFactorValue := api.PartyDetailContributionFactor(contributionFactorStrings[partyID])

	idStr := strconv.Itoa(partyID)

	members, err := p.GetPartyMembers(partyID, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get party members for party ID %d: %v", partyID, err)
	}

	numSpeeches, err := p.GetNumSpeechesOfParties([]int{partyID}, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get number of speeches for party ID %d: %v", partyID, err)
	}
	numSpeechesValue := numSpeeches[partyID]

	printedPapers, err := p.GetPrintedPapersForParty(partyID, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get printed papers for party ID %d: %v", partyID, err)
	}

	recentSpeeches, err := p.topicsService.GetSpeechSnippets(nil, nil, &partyID, &electionPeriod, 5)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get recent speeches for party ID %d: %v", partyID, err)
	}

	topTopics, err := p.GetTopTopicsOfParties([]int{partyID}, electionPeriod, 5)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get top topics for party ID %d: %v", partyID, err)
	}
	topTopicsValue := topTopics[partyID]

	volatilities, err := p.GetPartyVolatilities([]int{partyID}, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get volatility for party ID %d: %v", partyID, err)
	}
	volatilityValue := fmt.Sprintf("%.2f", volatilities[partyID])

	partyDetail := api.PartyDetail{
		ActivityData:       &activityData,
		ContributionFactor: &contributionFactorValue,
		Id:                 &idStr,
		Members:            &members,
		Name:               &party.Name.String,
		NumSpeeches:        &numSpeechesValue,
		PrintedPapers:      &printedPapers,
		RecentSpeeches:     &recentSpeeches,
		TopTopics:          &topTopicsValue,
		Volatility:         &volatilityValue,
	}

	return partyDetail, nil
}

package service

import (
	"fmt"
	api "plenartrend/crud/src/openAPI"
	"plenartrend/crud/src/types"
	"strconv"

	"github.com/jmoiron/sqlx"
)

type PartyService struct {
	db            *sqlx.DB
	topicsService *TopicsService
}

func NewPartyService(db *sqlx.DB, topicsService *TopicsService) *PartyService {
	return &PartyService{
		db:            db,
		topicsService: topicsService,
	}
}

func (p *PartyService) GetPartyContributionFactorStr(partyID int) (string, error) {
	return "Medium", nil
}

func (p *PartyService) GetNumSpeechesParty(partyID int, electionPeriod int) (int, error) {
	return 1, nil
}

func (p *PartyService) GetTopTopicsForParty(partyID int, numTopics int) ([]api.TopTopic, error) {
	return []api.TopTopic{}, nil
}

func (p *PartyService) GetPartyVolatility(partyID int) (string, error) {
	return "Mittel", nil
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
	`)
	if err != nil {
		return nil, fmt.Errorf("Failed to query parliamentary groups: %v", err)
	}

	apiParties := make([]api.Party, 0, len(parties))

	for i, party := range parties {
		contributionFactorStr, err := p.GetPartyContributionFactorStr(party.ID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get contribution factor for party ID %d: %v", party.ID, err)
		}
		contributionFactor := api.PartyContributionFactor(contributionFactorStr)

		partyIdStr := strconv.Itoa(party.ID)

		numSpeeches, err := p.GetNumSpeechesParty(party.ID, electionPeriod)
		if err != nil {
			return nil, fmt.Errorf("Failed to get number of speeches for party ID %d: %v", party.ID, err)
		}

		topTopics, err := p.GetTopTopicsForParty(party.ID, 3)
		if err != nil {
			return nil, fmt.Errorf("Failed to get top topics for party ID %d: %v", party.ID, err)
		}

		volatility, err := p.GetPartyVolatility(party.ID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get volatility for party ID %d: %v", party.ID, err)
		}

		apiParties[i] = api.Party{
			ContributionFactor: &contributionFactor,
			Id:                 &partyIdStr,
			Name:               &party.Name.String,
			NumSpeeches:        &numSpeeches,
			TopTopics:          &topTopics,
			Volatility:         &volatility,
		}
	}

	return apiParties, nil
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

func (p *PartyService) GetPartiesId(partyID int, electionPeriod int, timeRange api.TimeRangeFilter) (api.PartyDetail, error) {
	var party types.ParliamentaryGroup
	err := p.db.Get(&party, "SELECT * FROM parliamentary_groups WHERE id = $1", partyID)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to query parliamentary group: %v", err)
	}

	idStr := strconv.Itoa(partyID)

	numSpeeches, err := p.GetNumSpeechesParty(partyID, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get number of speeches for party ID %d: %v", partyID, err)
	}

	volatility, err := p.GetPartyVolatility(partyID)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get volatility for party ID %d: %v", partyID, err)
	}

	contributionFactorStr, err := p.GetPartyContributionFactorStr(partyID)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get contribution factor for party ID %d: %v", partyID, err)
	}
	contributionFactor := api.PartyDetailContributionFactor(contributionFactorStr)

	topTopics, err := p.GetTopTopicsForParty(partyID, 5)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get top topics for party ID %d: %v", partyID, err)
	}

	members, err := p.GetPartyMembers(partyID, electionPeriod)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get party members for party ID %d: %v", partyID, err)
	}

	recentSpeeches, err := p.topicsService.GetSpeechSnippets(nil, nil, &partyID, &electionPeriod, 5)
	if err != nil {
		return api.PartyDetail{}, fmt.Errorf("Failed to get recent speeches for party ID %d: %v", partyID, err)
	}

	partyDetail := api.PartyDetail{
		Id:                 &idStr,
		Name:               &party.Name.String,
		NumSpeeches:        &numSpeeches,
		Volatility:         &volatility,
		ContributionFactor: &contributionFactor,
		TopTopics:          &topTopics,
		Members:            &members,
		RecentSpeeches:     &recentSpeeches,
	}

	return partyDetail, nil
}

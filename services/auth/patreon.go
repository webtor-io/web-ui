package auth

// PatreonIdentityResponse represents the complete response from Patreon Identity API
type PatreonIdentityResponse struct {
	Data     PatreonUser           `json:"data"`
	Included []PatreonIncludedItem `json:"included"`
	Links    PatreonResponseLinks  `json:"links"`
}

// PatreonUser represents the main user data from Patreon
type PatreonUser struct {
	ID            string                   `json:"id"`
	Type          string                   `json:"type"`
	Attributes    PatreonUserAttributes    `json:"attributes"`
	Relationships PatreonUserRelationships `json:"relationships"`
}

// PatreonUserAttributes contains user-specific attributes
type PatreonUserAttributes struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

// PatreonUserRelationships contains related data references
type PatreonUserRelationships struct {
	Campaign PatreonCampaignRelationship `json:"campaign"`
}

// PatreonCampaignRelationship represents the campaign relationship
type PatreonCampaignRelationship struct {
	Data  PatreonRelationshipData  `json:"data"`
	Links PatreonRelationshipLinks `json:"links"`
}

// PatreonRelationshipData represents relationship data reference
type PatreonRelationshipData struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// PatreonRelationshipLinks contains related resource links
type PatreonRelationshipLinks struct {
	Related string `json:"related"`
}

// PatreonIncludedItem represents items in the included array
type PatreonIncludedItem struct {
	ID         string                    `json:"id"`
	Type       string                    `json:"type"`
	Attributes PatreonCampaignAttributes `json:"attributes"`
}

// PatreonCampaignAttributes contains campaign-specific attributes
type PatreonCampaignAttributes struct {
	IsMonthly bool   `json:"is_monthly"`
	Summary   string `json:"summary"`
}

// PatreonResponseLinks contains response-level links
type PatreonResponseLinks struct {
	Self string `json:"self"`
}

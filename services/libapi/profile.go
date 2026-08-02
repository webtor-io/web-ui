package libapi

// ProfileTier is the plan the account is on. Id 0 is the free tier — the same
// value services/claims.IsPaid gates on — and the API is not reachable on it,
// so a caller seeing this is on a paid plan by construction.
type ProfileTier struct {
	ID   uint32 `json:"id" example:"1"`
	Name string `json:"name,omitempty" example:"Pro"`
}

// ProfileSettings are the per-account preferences the profile page exposes.
// Pointers on the request side only (see ProfileSettingsRequest): here every
// field is always present.
type ProfileSettings struct {
	// ShowAdult opts out of the server-side blur on adult content.
	ShowAdult bool `json:"show_adult" example:"false"`
}

// ProfileResponse is who you are and what the account is allowed to do.
type ProfileResponse struct {
	UserID   string          `json:"user_id" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	Email    string          `json:"email,omitempty" example:"user@example.com"`
	Tier     ProfileTier     `json:"tier"`
	Settings ProfileSettings `json:"settings"`
	// Scopes are what the key used for this request may do.
	Scopes []string `json:"scopes" example:"api:read,api:write"`
}

// ProfileSettingsRequest is a partial update: a field left out is left alone.
// That is what the pointers are for — `{"show_adult": false}` and `{}` mean
// different things, and a plain bool cannot tell them apart.
type ProfileSettingsRequest struct {
	ShowAdult *bool `json:"show_adult" example:"true"`
}

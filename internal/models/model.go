package models

// Model represents an OpenAI-compatible model object.
type Model struct {
	ID      string `json:"id" example:"claude-sonnet-4-6"`
	Object  string `json:"object" example:"model"`
	Created int64  `json:"created" example:"1719792000"`
	OwnedBy string `json:"owned_by" example:"anthropic"`
}

// ModelList represents an OpenAI-compatible list of models.
type ModelList struct {
	Object string  `json:"object" example:"list"`
	Data   []Model `json:"data"`
}

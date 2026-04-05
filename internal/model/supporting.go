package model

// Principal represents an actor identity — human, agent, or service.
type Principal struct {
	PrincipalID  string        `json:"principal_id"`
	PrincipalType PrincipalType `json:"principal_type"`
	DisplayName  string        `json:"display_name"`
	OriginSystem string        `json:"origin_system"`
}

// ExternalRef links to an object in an external system.
type ExternalRef struct {
	RefType    RefType  `json:"ref_type"`
	Provider   Provider `json:"provider"`
	ExternalID string   `json:"external_id"`
	URL        string   `json:"url,omitempty"`
}

// Selector identifies a set of resources (files, components) for scope matching.
type Selector struct {
	Paths      []string `json:"paths,omitempty"`
	Components []string `json:"components,omitempty"`
}

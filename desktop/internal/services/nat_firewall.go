package services

type NATFirewallState struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Allowed   bool   `json:"allowed"`
	Detail    string `json:"detail,omitempty"`
}

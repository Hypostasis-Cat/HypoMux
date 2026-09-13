package platform

// SharingConnection describes Windows ICS configuration, not traffic proof.
type SharingConnection struct {
	GUID string `json:"guid"`
	Name string `json:"name"`
	Role int    `json:"role"`
}

type SharingSnapshot struct {
	Connections []SharingConnection `json:"connections"`
}

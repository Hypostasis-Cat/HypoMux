package services

type adapterMetadata struct {
	Description    string
	Gateway        string
	DNSServers     []string
	Metric         int
	AutoMetric     bool
	IPv6IfIndex    int
	IPv6Gateway    string
	IPv6Metric     int
	IPv6AutoMetric bool
	PreferredIPv6  string
}

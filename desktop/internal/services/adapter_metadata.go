package services

type adapterMetadata struct {
	Gateway     string
	DNSServers  []string
	Metric      int
	AutoMetric  bool
	Name        string
	Description string
	Kind        string
}

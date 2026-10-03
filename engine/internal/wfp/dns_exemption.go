package wfp

type Adapter struct {
	Name        string
	SourceIP    string
	IfIndex     uint32
	SourceIPv6  string
	IPv6IfIndex uint32
}

type DNSExemption interface {
	Close() error
	FilterIDs() []uint64
}

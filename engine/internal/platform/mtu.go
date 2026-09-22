package platform

import "fmt"

type MTUChange struct {
	IfIndex  int    `json:"if_index"`
	GUID     string `json:"guid"`
	Expected int    `json:"expected"`
	Value    int    `json:"value"`
}

func (p MTUChange) Validate() error {
	if p.IfIndex <= 0 || len(p.GUID) != 36 || p.Expected < 576 || p.Expected > 65535 || p.Value < 576 || p.Value > 65535 {
		return fmt.Errorf("invalid IPv4 MTU change")
	}
	for i, c := range p.GUID {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return fmt.Errorf("invalid adapter GUID")
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return fmt.Errorf("invalid adapter GUID")
		}
	}
	return nil
}

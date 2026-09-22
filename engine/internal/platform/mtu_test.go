package platform

import "testing"

func TestMTUChangeValidation(t *testing.T) {
	valid := MTUChange{IfIndex: 7, GUID: "12345678-1234-1234-1234-123456789abc", Expected: 1500, Value: 1492}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*MTUChange){
		func(p *MTUChange) { p.IfIndex = 0 },
		func(p *MTUChange) { p.Value = 575 },
		func(p *MTUChange) { p.Value = 65536 },
		func(p *MTUChange) { p.Expected = 0 },
		func(p *MTUChange) { p.GUID = "12345678-1234-1234-1234-123456789ab'" },
	} {
		p := valid
		mutate(&p)
		if p.Validate() == nil {
			t.Fatalf("accepted invalid change: %+v", p)
		}
	}
}

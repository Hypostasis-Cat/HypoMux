package dns

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

func TestAllCDNAddressesPreserveLegacySelectionAndMinimumTTL(t *testing.T) {
	query, id, err := buildQuery("cache1.steamcontent.com", dnsTypeA)
	if err != nil {
		t.Fatal(err)
	}
	first := answerForQuery(t, query, dnsTypeA, "1.2.3.4", 60)
	second := answerForQuery(t, query, dnsTypeA, "5.6.7.8", 30)
	response := append(first, second[len(query):]...)
	binary.BigEndian.PutUint16(response[6:8], 2)
	answer, err := parseResponse(response, id, dnsTypeA)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Address != "5.6.7.8" || answer.TTL != 30*time.Second || !slices.Equal(answer.Addresses, []string{"1.2.3.4", "5.6.7.8"}) {
		t.Fatalf("%+v", answer)
	}
}

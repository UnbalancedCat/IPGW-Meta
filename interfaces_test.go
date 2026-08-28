package ipgw

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestNormalizeInterfacesDeduplicatesPerInterfaceWithoutMutatingInput(t *testing.T) {
	shared := netip.MustParseAddr("192.0.2.10")
	input := []Interface{
		{Name: "ethernet", Index: 7, IP: netip.MustParseAddr("198.51.100.2")},
		{Name: "wifi", Index: 3, IP: shared},
		{Name: "ethernet", Index: 7, IP: shared},
		{Name: "wifi", Index: 3, IP: netip.MustParseAddr("192.0.2.2")},
		{Name: "wifi", Index: 3, IP: shared},
	}
	original := append([]Interface(nil), input...)

	got := normalizeInterfaces(input)
	want := []Interface{
		{Name: "wifi", Index: 3, IP: netip.MustParseAddr("192.0.2.2")},
		{Name: "wifi", Index: 3, IP: shared},
		{Name: "ethernet", Index: 7, IP: shared},
		{Name: "ethernet", Index: 7, IP: netip.MustParseAddr("198.51.100.2")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeInterfaces() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("normalizeInterfaces mutated input: got %#v, want %#v", input, original)
	}
}

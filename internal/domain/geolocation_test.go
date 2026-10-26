package domain

import (
	"math"
	"strings"
	"testing"
)

func TestGeoLocationValidation(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	good := GeoLocation{Country: "JP", City: "Tokyo", Lat: ptr(35.68), Lng: ptr(139.76), Precision: "city"}
	for _, tc := range []struct {
		name   string
		change func(*GeoLocation)
	}{
		{"country", func(g *GeoLocation) { g.Country = "ZZ" }},
		{"partial", func(g *GeoLocation) { g.Lng = nil }},
		{"latitude", func(g *GeoLocation) { g.Lat = ptr(90.01) }},
		{"longitude", func(g *GeoLocation) { g.Lng = ptr(-180.01) }},
		{"nan", func(g *GeoLocation) { g.Lat = ptr(math.NaN()) }},
		{"infinity", func(g *GeoLocation) { g.Lng = ptr(math.Inf(1)) }},
		{"city-empty", func(g *GeoLocation) { g.City = " " }},
		{"markup", func(g *GeoLocation) { g.City = "<script>" }},
		{"control", func(g *GeoLocation) { g.Subdivision = "a\nb" }},
		{"bidi", func(g *GeoLocation) { g.City = "a\u202Eb" }},
		{"length", func(g *GeoLocation) { g.City = strings.Repeat("界", 129) }},
		{"unknown-precision", func(g *GeoLocation) { g.Precision = "exact" }},
		{"country-with-coordinates", func(g *GeoLocation) { g.Precision = "country"; g.City = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := good.Clone()
			tc.change(g)
			if g.Validate() == nil {
				t.Fatalf("accepted %+v", g)
			}
		})
	}
	for _, g := range []GeoLocation{good, {Country: "US", Precision: "country"}, {Country: "US", City: "Saint John's", Precision: "city", Lat: ptr(0), Lng: ptr(0)}} {
		if err := g.Validate(); err != nil {
			t.Fatal(g, err)
		}
	}
}

func TestLocationSettingsNormalizeValidateAndClone(t *testing.T) {
	zero := 0.0
	s := &LocationSettings{Mode: "manual", Manual: &GeoLocation{Country: " us ", City: " Example City ", Subdivision: " Example State ", Lat: &zero, Lng: &zero, Precision: "city"}}
	s.Normalize()
	if s.Manual.Country != "US" || s.Manual.City != "Example City" || s.Manual.Subdivision != "Example State" || s.Validate() != nil {
		t.Fatal(s)
	}
	c := s.Clone()
	*c.Manual.Lat = 10
	c.Manual.City = "Other"
	if *s.Manual.Lat != 0 || s.Manual.City != "Example City" {
		t.Fatal("clone shared mutable fields")
	}
	for _, bad := range []LocationSettings{{Mode: "manual"}, {Mode: "inherit"}, {Mode: "off", Manual: &GeoLocation{Country: "bad", Precision: "country"}}} {
		if bad.Validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	for _, mode := range []string{"auto", "off"} {
		if err := (LocationSettings{Mode: mode}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

package domain

import (
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

var ErrGeoLocation = errors.New("invalid location: select a country and, for city precision, enter a city and valid latitude/longitude")
var ErrLocationSettings = errors.New("invalid location settings: expected auto, manual or off")

// GeoLocation is display metadata, never an address used for routing. Country
// precision deliberately carries no coordinates: clients may use an explicitly
// approximate country marker, but must not present a country centre as a city.
type GeoLocation struct {
	Country     string   `json:"country"`
	Subdivision string   `json:"subdivision,omitempty"`
	City        string   `json:"city,omitempty"`
	Lat         *float64 `json:"lat,omitempty"`
	Lng         *float64 `json:"lng,omitempty"`
	Precision   string   `json:"precision"` // city | country
}

// LocationSettings is private configuration. Only its resolved GeoLocation is
// published; a disabled manual draft must never leak through a public response.
type LocationSettings struct {
	Mode   string       `json:"mode"` // auto | manual | off
	Manual *GeoLocation `json:"manual,omitempty"`
}

func ValidCountryCode(code string) bool {
	if len(code) != 2 || code != strings.ToUpper(code) {
		return false
	}
	r, err := language.ParseRegion(code)
	return err == nil && r.IsCountry() && !r.IsPrivateUse() && r.String() == code && r.Canonicalize().String() == code
}

func (g *GeoLocation) Normalize() {
	if g == nil {
		return
	}
	g.Country = strings.ToUpper(strings.TrimSpace(g.Country))
	g.Subdivision = strings.TrimSpace(g.Subdivision)
	g.City = strings.TrimSpace(g.City)
}

func locationLabel(s string) bool {
	if !utf8.ValidString(s) || len(s) > 512 || utf8.RuneCountInString(s) > 128 || strings.ContainsAny(s, "<>") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func (g GeoLocation) Validate() error {
	if !ValidCountryCode(g.Country) || !locationLabel(g.City) || !locationLabel(g.Subdivision) {
		return ErrGeoLocation
	}
	switch g.Precision {
	case "country":
		if g.City != "" || g.Lat != nil || g.Lng != nil {
			return ErrGeoLocation
		}
	case "city":
		if strings.TrimSpace(g.City) == "" || g.Lat == nil || g.Lng == nil {
			return ErrGeoLocation
		}
		if math.IsNaN(*g.Lat) || math.IsInf(*g.Lat, 0) || *g.Lat < -90 || *g.Lat > 90 || math.IsNaN(*g.Lng) || math.IsInf(*g.Lng, 0) || *g.Lng < -180 || *g.Lng > 180 {
			return ErrGeoLocation
		}
	default:
		return ErrGeoLocation
	}
	return nil
}

func (g *GeoLocation) Clone() *GeoLocation {
	if g == nil {
		return nil
	}
	out := *g
	if g.Lat != nil {
		v := *g.Lat
		out.Lat = &v
	}
	if g.Lng != nil {
		v := *g.Lng
		out.Lng = &v
	}
	return &out
}

func (s *LocationSettings) Normalize() {
	if s == nil {
		return
	}
	s.Manual.Normalize()
}

func (s LocationSettings) Validate() error {
	if s.Mode != "auto" && s.Mode != "manual" && s.Mode != "off" {
		return ErrLocationSettings
	}
	if s.Mode == "manual" && s.Manual == nil {
		return ErrGeoLocation
	}
	if s.Manual != nil {
		return s.Manual.Validate()
	}
	return nil
}

func (s *LocationSettings) Clone() *LocationSettings {
	if s == nil {
		return nil
	}
	out := *s
	out.Manual = s.Manual.Clone()
	return &out
}

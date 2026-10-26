package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type countryTransport func(*http.Request) (*http.Response, error)

func (f countryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCountryLookupValidatesResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body, country string
		status              int
		bad                 bool
	}{
		{"ok", `{"ip":"198.51.100.20","country":"JP"}`, "JP", 200, false},
		{"wrong-IP", `{"ip":"203.0.113.30","country":"JP"}`, "", 200, true},
		{"invalid-country", `{"ip":"198.51.100.20","country":"<script>"}`, "", 200, true},
		{"unknown", `{"ip":"198.51.100.20","country":null}`, "", 200, false},
		{"oversize", strings.Repeat("x", 5000), "", 200, true},
		{"not-found", ``, "", 404, false}, {"limited", ``, "", 429, true}, {"invalid-json", `{`, "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &http.Client{Transport: countryTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.country.is/198.51.100.20?fields=city,subdivision,location" || r.Header.Get("Authorization") != "" {
					t.Fatal(r.URL)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			code, err := lookupCountryWithClient(context.Background(), "198.51.100.20", c)
			if (err != nil) != tc.bad || code != tc.country {
				t.Fatal(code, err)
			}
		})
	}
	c := &http.Client{Transport: countryTransport(func(*http.Request) (*http.Response, error) { t.Fatal("private/invalid address sent"); return nil, nil })}
	for _, ip := range []string{"127.0.0.1", "10.10.0.2", "100.64.0.1", "fe80::1", "example.com", "https://example.com"} {
		if _, err := lookupCountryWithClient(context.Background(), ip, c); err == nil {
			t.Fatal(ip)
		}
	}
}

func TestLocationLookupParsesCityAndRejectsInvalidData(t *testing.T) {
	for _, tc := range []struct {
		name, fields, precision string
		bad                     bool
	}{
		{"city", `"city":"Tokyo","subdivision":"13","location":{"latitude":35.68,"longitude":139.76}`, "city", false},
		{"zero-coordinates", `"city":"Example City","location":{"latitude":0,"longitude":0}`, "city", false},
		{"country-only", `"city":null,"subdivision":null,"location":null`, "country", false},
		{"country-centre-is-not-city", `"city":null,"location":{"latitude":35,"longitude":139}`, "country", false},
		{"city-without-coordinates", `"city":"Tokyo","location":null`, "country", false},
		{"partial-coordinates", `"city":"Tokyo","location":{"latitude":35}`, "country", false},
		{"empty-city", `"city":" ","location":{"latitude":35,"longitude":139}`, "", true},
		{"markup", `"city":"<b>Tokyo</b>","location":{"latitude":35,"longitude":139}`, "", true},
		{"control", `"city":"Tokyo\nStation","location":{"latitude":35,"longitude":139}`, "", true},
		{"invalid-subdivision", `"subdivision":"<script>","city":null`, "", true},
		{"latitude-range", `"city":"Tokyo","location":{"latitude":91,"longitude":139}`, "", true},
		{"longitude-range", `"city":"Tokyo","location":{"latitude":35,"longitude":181}`, "", true},
		{"coordinate-type", `"city":"Tokyo","location":{"latitude":"35","longitude":139}`, "", true},
		{"partial-invalid", `"city":null,"location":{"latitude":91}`, "", true},
		{"oversize-city", `"city":"` + strings.Repeat("x", 129) + `"`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"ip":"2001:db8::1","country":"JP",` + tc.fields + `}`
			c := &http.Client{Transport: countryTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.country.is/2001:db8::1?fields=city,subdivision,location" {
					t.Fatal(r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			got, err := lookupLocationWithClient(context.Background(), "2001:db8::1", c)
			if (err != nil) != tc.bad {
				t.Fatalf("location=%+v err=%v", got, err)
			}
			if tc.bad {
				return
			}
			if got == nil || got.Country != "JP" || got.Precision != tc.precision {
				t.Fatalf("%+v", got)
			}
			if tc.precision == "country" && (got.Lat != nil || got.Lng != nil || got.City != "") {
				t.Fatalf("approximate location claimed city coordinates: %+v", got)
			}
			if tc.precision == "city" && (got.Lat == nil || got.Lng == nil || got.City == "") {
				t.Fatalf("lost city fields: %+v", got)
			}
		})
	}
}

func TestLocationLookupNeverFollowsProviderRedirect(t *testing.T) {
	calls := 0
	c := &http.Client{Transport: countryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			t.Fatal("followed geolocation redirect")
		}
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://example.com/collect"}}}, nil
	})}
	if _, err := lookupLocationWithClient(context.Background(), "198.51.100.20", c); err == nil || calls != 1 {
		t.Fatal(calls, err)
	}
}

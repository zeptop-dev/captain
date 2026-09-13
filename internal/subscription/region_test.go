package subscription

import "testing"

func TestRegionOf(t *testing.T) {
	cases := map[[2]string]string{
		{"Tokyo IPLC 01", ""}:            "JP",
		{"香港 BGP", ""}:                   "HK",
		{"HK-IEPL-02", ""}:               "HK",
		{"Hong Kong 01", ""}:             "HK",
		{"US Los Angeles", ""}:           "US",
		{"新加坡 01", ""}:                   "SG",
		{"Premium 01", "jp1.zeptop.dev"}: "JP",
		{"Premium 01", "1.2.3.4"}:        "",
		{"美", ""}:                        "US",
		{"Frankfurt", ""}:                "DE",
		{"random", ""}:                   "",
		{"HKG-1", ""}:                    "", // hkg is not a keyword
	}
	for in, want := range cases {
		if got := RegionOf(in[0], in[1]); got != want {
			t.Errorf("RegionOf(%q,%q)=%q want %q", in[0], in[1], got, want)
		}
	}
	if Flag("jp") != "🇯🇵" || Flag("x") != "" {
		t.Fatal("flag")
	}
	if WithFlag("🇯🇵 Tokyo", "", "", true) != "🇯🇵 Tokyo" || WithFlag("Tokyo", "", "", true) != "🇯🇵 Tokyo" || WithFlag("Tokyo", "", "HK", true) != "🇭🇰 Tokyo" || WithFlag("Tokyo", "", "", false) != "Tokyo" {
		t.Fatal("WithFlag")
	}
}

func TestUnescapeAstral(t *testing.T) {
	in := []byte(`name: "\U0001F1EF\U0001F1F5 JP"` + "\n" + `pw: "a\\U0001F1EF"` + "\n")
	want := "name: \"🇯🇵 JP\"\npw: \"a\\\\U0001F1EF\"\n"
	if got := string(unescapeAstral(in)); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

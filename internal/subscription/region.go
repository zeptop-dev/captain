package subscription

import (
	"regexp"
	"strings"
)

// Region detection for node names: turns "Tokyo IPLC 01" into "JP" so the
// subscription can prefix a flag, as most panels do. Explicit codes win;
// detection only looks at the name and the display host.

var regionWords = map[string][]string{
	"JP": {"jp", "japan", "tokyo", "osaka", "日本", "东京", "東京", "大阪"},
	"HK": {"hk", "hongkong", "hong kong", "香港", "港"},
	"TW": {"tw", "taiwan", "taipei", "台湾", "台灣", "台北", "台"},
	"SG": {"sg", "singapore", "新加坡", "狮城", "獅城", "新"},
	"US": {"us", "usa", "america", "united states", "los angeles", "san jose", "seattle", "silicon", "美国", "美國", "洛杉矶", "洛杉磯", "圣何塞", "西雅图", "美"},
	"KR": {"kr", "korea", "seoul", "韩国", "韓國", "首尔", "首爾", "韩"},
	"GB": {"uk", "gb", "britain", "england", "london", "英国", "英國", "伦敦", "倫敦", "英"},
	"DE": {"de", "germany", "frankfurt", "德国", "德國", "法兰克福", "德"},
	"FR": {"fr", "france", "paris", "法国", "法國", "巴黎", "法"},
	"NL": {"nl", "netherlands", "amsterdam", "荷兰", "荷蘭", "阿姆斯特丹", "荷"},
	"RU": {"ru", "russia", "moscow", "俄罗斯", "俄羅斯", "莫斯科", "俄"},
	"CA": {"ca", "canada", "toronto", "vancouver", "加拿大", "多伦多", "温哥华", "加"},
	"AU": {"au", "australia", "sydney", "澳大利亚", "澳洲", "悉尼", "澳"},
	"IN": {"in", "india", "mumbai", "印度"},
	"BR": {"br", "brazil", "sao paulo", "巴西"},
	"TR": {"tr", "turkey", "istanbul", "土耳其", "土"},
	"AR": {"ar", "argentina", "阿根廷"},
	"MY": {"my", "malaysia", "kuala lumpur", "马来西亚", "馬來西亞", "马来"},
	"TH": {"th", "thailand", "bangkok", "泰国", "泰國", "泰"},
	"VN": {"vn", "vietnam", "越南", "越"},
	"PH": {"ph", "philippines", "manila", "菲律宾", "菲律賓", "菲"},
	"ID": {"id", "indonesia", "jakarta", "印尼", "印度尼西亚"},
	"AE": {"ae", "uae", "dubai", "迪拜", "阿联酋"},
	"CH": {"ch", "switzerland", "zurich", "瑞士"},
	"SE": {"se", "sweden", "stockholm", "瑞典"},
	"FI": {"fi", "finland", "helsinki", "芬兰", "芬蘭"},
	"IT": {"it", "italy", "milan", "意大利"},
	"ES": {"es", "spain", "madrid", "西班牙"},
	"PL": {"pl", "poland", "warsaw", "波兰", "波蘭"},
	"UA": {"ua", "ukraine", "乌克兰", "烏克蘭"},
	"CN": {"cn", "china", "中国", "中國"},
	"MO": {"mo", "macau", "macao", "澳门", "澳門"},
	"IE": {"ie", "ireland", "dublin", "爱尔兰", "愛爾蘭"},
	"IL": {"il", "israel", "以色列"},
	"ZA": {"za", "south africa", "南非"},
	"MX": {"mx", "mexico", "墨西哥"},
	"CL": {"cl", "chile", "智利"},
	"NZ": {"nz", "new zealand", "新西兰", "紐西蘭"},
	"PT": {"pt", "portugal", "lisbon", "葡萄牙"},
	"AT": {"at", "austria", "vienna", "奥地利"},
	"BE": {"be", "belgium", "比利时"},
	"CZ": {"cz", "czech", "prague", "捷克"},
	"HU": {"hu", "hungary", "匈牙利"},
	"RO": {"ro", "romania", "罗马尼亚"},
	"NO": {"no", "norway", "oslo", "挪威"},
	"DK": {"dk", "denmark", "丹麦", "丹麥"},
	"KZ": {"kz", "kazakhstan", "哈萨克"},
	"EG": {"eg", "egypt", "埃及"},
	"SA": {"sa", "saudi", "沙特"},
	"PK": {"pk", "pakistan", "巴基斯坦"},
	"NG": {"ng", "nigeria", "尼日利亚"},
	"GR": {"gr", "greece", "希腊"},
	"BG": {"bg", "bulgaria", "保加利亚"},
	"LV": {"lv", "latvia", "拉脱维亚"},
	"LT": {"lt", "lithuania", "立陶宛"},
	"EE": {"ee", "estonia", "爱沙尼亚"},
	"LU": {"lu", "luxembourg", "卢森堡"},
	"IS": {"is", "iceland", "冰岛"},
	"CO": {"co", "colombia", "哥伦比亚"},
	"PE": {"pe", "peru", "秘鲁"},
	"KH": {"kh", "cambodia", "柬埔寨"},
	"BD": {"bd", "bangladesh", "孟加拉"},
	"NP": {"np", "nepal", "尼泊尔"},
	"MN": {"mn", "mongolia", "蒙古"},
}

// Keywords that must match a whole ASCII word (two-letter codes), listed
// longest-first so "hong kong" beats "hk" ambiguity; single CJK characters
// are only tried after every longer keyword failed.
var (
	regionOrder  []string
	asciiWord    = regexp.MustCompile(`[a-z]+`)
	flagRunes    = regexp.MustCompile(`^\s*[\x{1F1E6}-\x{1F1FF}]{2}`)
	regionOrders = func() []struct{ code, word string } {
		var out []struct{ code, word string }
		for code, words := range regionWords {
			for _, w := range words {
				out = append(out, struct{ code, word string }{code, w})
			}
		}
		// longest keyword first; ties broken by code for determinism
		for i := 1; i < len(out); i++ {
			for j := i; j > 0 && less(out[j], out[j-1]); j-- {
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
		return out
	}()
)

func less(a, b struct{ code, word string }) bool {
	la, lb := len([]rune(a.word)), len([]rune(b.word))
	if la != lb {
		return la > lb
	}
	if a.word != b.word {
		return a.word < b.word
	}
	return a.code < b.code
}

// RegionOf guesses the ISO 3166-1 alpha-2 region from a node name and its
// host name; "" when nothing matches.
func RegionOf(name, host string) string {
	for _, text := range []string{name, hostLabel(host)} {
		if code := regionIn(text); code != "" {
			return code
		}
	}
	return ""
}

// hostLabel keeps the first label of a domain ("jp1.example.com" -> "jp1"),
// never a bare IP.
func hostLabel(host string) string {
	if host == "" || strings.Trim(host, "0123456789.:abcdefABCDEF[]") == "" {
		return ""
	}
	if i := strings.Index(host, "."); i > 0 {
		return host[:i]
	}
	return host
}

func regionIn(text string) string {
	lower := strings.ToLower(text)
	words := map[string]bool{}
	for _, w := range asciiWord.FindAllString(lower, -1) {
		words[w] = true
	}
	for _, rw := range regionOrders {
		w := rw.word
		isASCII := w[0] < 0x80
		switch {
		case isASCII && !strings.Contains(w, " "):
			if words[w] {
				return rw.code
			}
		case isASCII:
			if strings.Contains(lower, w) {
				return rw.code
			}
		case len([]rune(w)) > 1:
			if strings.Contains(text, w) {
				return rw.code
			}
		}
	}
	// Single-character CJK abbreviations last ("港", "美"): only when the name
	// is short enough for that to be the point.
	for _, rw := range regionOrders {
		if rw.word[0] >= 0x80 && len([]rune(rw.word)) == 1 && strings.Contains(text, rw.word) {
			return rw.code
		}
	}
	return ""
}

// Flag renders a region code as its emoji flag ("JP" -> 🇯🇵); "" when the
// code is not two letters.
func Flag(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return string([]rune{rune(0x1F1E6 + int(code[0]-'A')), rune(0x1F1E6 + int(code[1]-'A'))})
}

// HasFlag reports whether a name already begins with an emoji flag.
func HasFlag(name string) bool { return flagRunes.MatchString(name) }

// WithFlag prefixes a flag when the name has none: the explicit region if
// set, else (when auto is on) whatever RegionOf detects.
func WithFlag(name, host, region string, auto bool) string {
	if HasFlag(name) {
		return name
	}
	if region == "" && auto {
		region = RegionOf(name, host)
	}
	if f := Flag(region); f != "" {
		return f + " " + name
	}
	return name
}

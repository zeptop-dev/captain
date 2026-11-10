// City reference points from GeoNames, CC BY 4.0. See docs/LOCATION_CORRECTIONS.md.
// Approximate city markers, never datacenter coordinates or IP geolocation evidence.
export interface LocationCity { id: string; country: string; city: string; zh: string; lat: number; lng: number }
export const locationCities: readonly LocationCity[] = [
  {
    "id": "2643743",
    "country": "GB",
    "city": "London",
    "zh": "伦敦",
    "lat": 51.5085,
    "lng": -0.1257
  },
  {
    "id": "5368361",
    "country": "US",
    "city": "Los Angeles",
    "zh": "洛杉矶",
    "lat": 34.0522,
    "lng": -118.2437
  },
  {
    "id": "5392171",
    "country": "US",
    "city": "San Jose",
    "zh": "圣何塞",
    "lat": 37.3394,
    "lng": -121.895
  },
  {
    "id": "5809844",
    "country": "US",
    "city": "Seattle",
    "zh": "西雅图",
    "lat": 47.6062,
    "lng": -122.3321
  },
  {
    "id": "5128581",
    "country": "US",
    "city": "New York City",
    "zh": "纽约",
    "lat": 40.7143,
    "lng": -74.006
  },
  {
    "id": "2925533",
    "country": "DE",
    "city": "Frankfurt am Main",
    "zh": "法兰克福",
    "lat": 50.1155,
    "lng": 8.6842
  },
  {
    "id": "2759794",
    "country": "NL",
    "city": "Amsterdam",
    "zh": "阿姆斯特丹",
    "lat": 52.374,
    "lng": 4.8897
  },
  {
    "id": "2988507",
    "country": "FR",
    "city": "Paris",
    "zh": "巴黎",
    "lat": 48.8534,
    "lng": 2.3488
  },
  {
    "id": "1850147",
    "country": "JP",
    "city": "Tokyo",
    "zh": "东京",
    "lat": 35.6895,
    "lng": 139.6917
  },
  {
    "id": "1853909",
    "country": "JP",
    "city": "Osaka",
    "zh": "大阪",
    "lat": 34.6938,
    "lng": 135.5011
  },
  {
    "id": "1880252",
    "country": "SG",
    "city": "Singapore",
    "zh": "新加坡",
    "lat": 1.2897,
    "lng": 103.8501
  },
  {
    "id": "1819729",
    "country": "HK",
    "city": "Hong Kong",
    "zh": "香港",
    "lat": 22.2783,
    "lng": 114.1747
  },
  {
    "id": "1668341",
    "country": "TW",
    "city": "Taipei",
    "zh": "台北",
    "lat": 25.0531,
    "lng": 121.5264
  },
  {
    "id": "1835848",
    "country": "KR",
    "city": "Seoul",
    "zh": "首尔",
    "lat": 37.566,
    "lng": 126.9784
  },
  {
    "id": "2147714",
    "country": "AU",
    "city": "Sydney",
    "zh": "悉尼",
    "lat": -33.8678,
    "lng": 151.2073
  },
  {
    "id": "6167865",
    "country": "CA",
    "city": "Toronto",
    "zh": "多伦多",
    "lat": 43.6533,
    "lng": -79.3839
  }
]

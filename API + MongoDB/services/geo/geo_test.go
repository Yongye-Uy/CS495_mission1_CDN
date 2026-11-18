package geo

import (
	"math"
	"net/http/httptest"
	"testing"
)

func TestHaversine(t *testing.T) {
	// London: 51.5074, -0.1278
	// Paris: 48.8566, 2.3522
	// Expected distance: ~344 km
	c1 := Coordinates{Latitude: 51.5074, Longitude: -0.1278}
	c2 := Coordinates{Latitude: 48.8566, Longitude: 2.3522}

	dist := Haversine(c1, c2)
	if math.Abs(dist-344.0) > 10.0 {
		t.Errorf("Expected Haversine distance between London and Paris ~344km, got %.2f km", dist)
	}

	// Distance to self must be 0
	if Haversine(c1, c1) != 0.0 {
		t.Errorf("Distance to self should be 0, got %f", Haversine(c1, c1))
	}
}

func TestGeoService_SimulatedCountries(t *testing.T) {
	service := NewGeoService()

	nodes := map[string]string{
		"singapore": "http://sg:8081",
		"newyork":   "http://ny:8081",
		"london":    "http://london:8081",
	}

	tests := []struct {
		name            string
		headerCountry   string
		queryCountry    string
		expectedNearest string
	}{
		{
			name:            "Cambodia routes to Singapore",
			headerCountry:   "KH",
			expectedNearest: "singapore",
		},
		{
			name:            "USA routes to New York",
			headerCountry:   "US",
			expectedNearest: "newyork",
		},
		{
			name:            "United Kingdom routes to London",
			headerCountry:   "GB",
			expectedNearest: "london",
		},
		{
			name:            "Query parameter country=KH routes to Singapore",
			queryCountry:    "KH",
			expectedNearest: "singapore",
		},
		{
			name:            "Query parameter country=US routes to New York",
			queryCountry:    "US",
			expectedNearest: "newyork",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/files/test"
			if tc.queryCountry != "" {
				url += "?country=" + tc.queryCountry
			}
			req := httptest.NewRequest("GET", url, nil)
			if tc.headerCountry != "" {
				req.Header.Set("X-Test-Country", tc.headerCountry)
			}

			clientLoc := service.ResolveClientLocation(req)
			ranked := service.RankStorageNodes(clientLoc, nodes)

			if len(ranked) == 0 {
				t.Fatalf("No ranked nodes returned")
			}

			if ranked[0].Location != tc.expectedNearest {
				t.Errorf("Expected nearest node %s, got %s (distance: %.2f km)",
					tc.expectedNearest, ranked[0].Location, ranked[0].DistanceKm)
			}
		})
	}
}

package geo

import (
	"math"
	"net"
	"net/http"
	"sort"
	"strings"

	"mini-cdn/api/models"
)

const EarthRadiusKm = 6371.0

type Coordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type ClientLocation struct {
	IP          string      `json:"ip"`
	Country     string      `json:"country"`
	CountryCode string      `json:"countryCode"`
	Coords      Coordinates `json:"coords"`
	IsTest      bool        `json:"isTest"`
}

type RankedNode struct {
	Location   string  `json:"location"`
	URL        string  `json:"url"`
	DistanceKm float64 `json:"distanceKm"`
}

type GeoService struct {
	StorageLocations map[string]Coordinates
	CountryPresets   map[string]ClientLocation
}

func NewGeoService() *GeoService {
	// Standard coordinates for storage locations
	storageLocs := map[string]Coordinates{
		"singapore": {Latitude: 1.3521, Longitude: 103.8198},
		"newyork":   {Latitude: 40.7128, Longitude: -74.0060},
		"london":    {Latitude: 51.5074, Longitude: -0.1278},
		"japan":     {Latitude: 35.6762, Longitude: 139.6503},
		"tokyo":     {Latitude: 35.6762, Longitude: 139.6503},
		"asia":      {Latitude: 35.6762, Longitude: 139.6503},
		"local":     {Latitude: 1.3521, Longitude: 103.8198}, // Single storage node default
	}

	// Country test presets for local simulation
	presets := map[string]ClientLocation{
		"KH": {
			Country:     "Cambodia",
			CountryCode: "KH",
			Coords:      Coordinates{Latitude: 11.5564, Longitude: 104.9282},
			IsTest:      true,
		},
		"CAMBODIA": {
			Country:     "Cambodia",
			CountryCode: "KH",
			Coords:      Coordinates{Latitude: 11.5564, Longitude: 104.9282},
			IsTest:      true,
		},
		"US": {
			Country:     "United States",
			CountryCode: "US",
			Coords:      Coordinates{Latitude: 38.9072, Longitude: -77.0369},
			IsTest:      true,
		},
		"USA": {
			Country:     "United States",
			CountryCode: "US",
			Coords:      Coordinates{Latitude: 38.9072, Longitude: -77.0369},
			IsTest:      true,
		},
		"GB": {
			Country:     "United Kingdom",
			CountryCode: "GB",
			Coords:      Coordinates{Latitude: 51.5074, Longitude: -0.1278},
			IsTest:      true,
		},
		"UK": {
			Country:     "United Kingdom",
			CountryCode: "GB",
			Coords:      Coordinates{Latitude: 51.5074, Longitude: -0.1278},
			IsTest:      true,
		},
		"SG": {
			Country:     "Singapore",
			CountryCode: "SG",
			Coords:      Coordinates{Latitude: 1.3521, Longitude: 103.8198},
			IsTest:      true,
		},
		"FR": {
			Country:     "France",
			CountryCode: "FR",
			Coords:      Coordinates{Latitude: 48.8566, Longitude: 2.3522},
			IsTest:      true,
		},
		"JP": {
			Country:     "Japan",
			CountryCode: "JP",
			Coords:      Coordinates{Latitude: 35.6762, Longitude: 139.6503},
			IsTest:      true,
		},
	}

	return &GeoService{
		StorageLocations: storageLocs,
		CountryPresets:   presets,
	}
}

// GetCoordinatesForLocation retrieves known geographic coordinates for a location or region name.
func (g *GeoService) GetCoordinatesForLocation(loc string) Coordinates {
	locLower := strings.ToLower(strings.TrimSpace(loc))
	if coords, ok := g.StorageLocations[locLower]; ok {
		return coords
	}
	locUpper := strings.ToUpper(strings.TrimSpace(loc))
	if preset, ok := g.CountryPresets[locUpper]; ok {
		return preset.Coords
	}
	// Common aliases
	switch locLower {
	case "asia", "japan", "tokyo", "jp":
		return Coordinates{Latitude: 35.6762, Longitude: 139.6503}
	case "singapore", "sg":
		return Coordinates{Latitude: 1.3521, Longitude: 103.8198}
	case "europe", "germany", "frankfurt", "de":
		return Coordinates{Latitude: 50.1109, Longitude: 8.6821}
	case "uk", "london", "great britain":
		return Coordinates{Latitude: 51.5074, Longitude: -0.1278}
	case "us", "usa", "newyork", "ny":
		return Coordinates{Latitude: 40.7128, Longitude: -74.0060}
	}
	return Coordinates{Latitude: 0, Longitude: 0}
}

// Haversine calculates great-circle distance between two coordinates in kilometers.
func Haversine(c1, c2 Coordinates) float64 {
	lat1Rad := c1.Latitude * math.Pi / 180.0
	lon1Rad := c1.Longitude * math.Pi / 180.0
	lat2Rad := c2.Latitude * math.Pi / 180.0
	lon2Rad := c2.Longitude * math.Pi / 180.0

	dLat := lat2Rad - lat1Rad
	dLon := lon2Rad - lon1Rad

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return EarthRadiusKm * c
}

// ExtractClientIP retrieves the real or proxy IP address from request headers.
func ExtractClientIP(r *http.Request) string {
	// 1. Check X-Forwarded-For
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		clientIP := strings.TrimSpace(parts[0])
		if clientIP != "" {
			return clientIP
		}
	}

	// 2. Check X-Real-IP
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// 3. RemoteAddr (strip port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}

	return r.RemoteAddr
}

// ResolveClientLocation identifies the geographic coordinates of the caller.
// Prioritizes explicit testing header X-Test-Country or query parameter ?country=,
// enabling local demonstration without physical relocation.
func (g *GeoService) ResolveClientLocation(r *http.Request) ClientLocation {
	clientIP := ExtractClientIP(r)

	// Check test country override via header or URL query
	testCountry := r.Header.Get("X-Test-Country")
	if testCountry == "" {
		testCountry = r.URL.Query().Get("country")
	}

	if testCountry != "" {
		normalized := strings.ToUpper(strings.TrimSpace(testCountry))
		if preset, found := g.CountryPresets[normalized]; found {
			preset.IP = clientIP
			return preset
		}
	}

	// If no test country header was provided:
	// For production or non-test requests, check if it's localhost / private IP
	if isPrivateOrLoopbackIP(clientIP) {
		// Default local development position (Cambodia university default)
		def := g.CountryPresets["KH"]
		def.IP = clientIP
		def.IsTest = false
		return def
	}

	// For real public IP in production, could perform GeoIP lookup.
	// As a reliable fallback, return Cambodia or nearest default:
	def := g.CountryPresets["KH"]
	def.IP = clientIP
	def.IsTest = false
	return def
}

func isPrivateOrLoopbackIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true // Treat unparseable like localhost in dev
	}
	return ip.IsLoopback() || ip.IsPrivate() || ipStr == "::1" || ipStr == "localhost"
}

// RankStorageNodes orders the given storage nodes by proximity to the client.
func (g *GeoService) RankStorageNodes(clientLoc ClientLocation, nodeURLs map[string]string) []RankedNode {
	var ranked []RankedNode

	for location, url := range nodeURLs {
		locLower := strings.ToLower(location)
		coords, ok := g.StorageLocations[locLower]
		var dist float64
		if !ok {
			// If location coordinates not registered, set a high distance
			dist = 99999.0
		} else {
			dist = Haversine(clientLoc.Coords, coords)
		}

		ranked = append(ranked, RankedNode{
			Location:   location,
			URL:        url,
			DistanceKm: math.Round(dist*100) / 100, // Round to 2 decimal places
		})
	}

	// Sort ascending by distance
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].DistanceKm < ranked[j].DistanceKm
	})

	return ranked
}

// RankDynamicNodes orders dynamic StorageNode models by proximity to the client.
func (g *GeoService) RankDynamicNodes(clientLoc ClientLocation, nodes []*models.StorageNode) []RankedNode {
	var ranked []RankedNode

	for _, n := range nodes {
		coords := Coordinates{Latitude: n.Latitude, Longitude: n.Longitude}
		if coords.Latitude == 0 && coords.Longitude == 0 {
			locLower := strings.ToLower(n.Region)
			if c, ok := g.StorageLocations[locLower]; ok {
				coords = c
			} else if preset, ok := g.CountryPresets[strings.ToUpper(n.Country)]; ok {
				coords = preset.Coords
			} else {
				coords = Coordinates{Latitude: 0, Longitude: 0}
			}
		}

		dist := Haversine(clientLoc.Coords, coords)
		name := n.NodeID
		if name == "" {
			name = n.Region
		}

		ranked = append(ranked, RankedNode{
			Location:   name,
			URL:        n.BaseURL(),
			DistanceKm: math.Round(dist*100) / 100,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].DistanceKm < ranked[j].DistanceKm
	})

	return ranked
}

// countries.go answers one question for the adverts map: which country holds a point, and what
// does its outline cover (research/advert-map-basemap.md §5). The frame of the map comes from this
// answer, so a bad advertised coordinate can never stretch it. The data is generated
// (basemap_countries.go): Natural Earth 1:110m admin-0 polygons and tiny-country points, public
// domain, never drawn.

package web

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type countryShape struct {
	name string
	// bbox over every ring, for the quick reject before the ray cast.
	west, east, south, north float64
	// rings are flat lon,lat pairs, unclosed. Even-odd across all rings of a country handles
	// holes (Lesotho inside South Africa) and disjoint parts (islands, overseas rings).
	rings [][]float64
}

type countryIndex struct {
	countries []countryShape
	tiny      []struct {
		name     string
		lat, lon float64
	}
}

var (
	countryOnce   sync.Once
	countryParsed countryIndex
)

// maxMercLat bounds the projection: Web Mercator has no pole, and no mesh node stands past it.
const maxMercLat = 85.0

// mercatorY maps a latitude to the basemap's y (Web Mercator, in longitude degrees' units; the
// generator projected the basemap with the same formula). Latitudes past the bound clamp.
func mercatorY(lat float64) float64 {
	lat = min(max(lat, -maxMercLat), maxMercLat)
	return 180 / math.Pi * math.Log(math.Tan(math.Pi/4+lat*math.Pi/360))
}

// cityPlace is one major city of the vendored Natural Earth list: its name and scaled rank, 0 the
// most major. Names are generated data, never radio text (D45).
type cityPlace struct {
	name       string
	lon, lat   float64
	rank       int
	population int
	local      bool // from the curated addendum rather than Natural Earth
}

var (
	cityOnce     sync.Once
	citiesParsed []cityPlace
)

// cities reads the generated city list plus the small local addendum, most major first, so a
// frame cap keeps the big ones.
func cities() []cityPlace {
	cityOnce.Do(func() {
		citiesParsed = parseCityPlaces(cityPlaces, citiesParsed, false)
		for _, city := range parseCityPlaces(localPlaces, nil, true) {
			citiesParsed = mergeCity(citiesParsed, city) // a future Natural Earth drop may already name it
		}
		for _, city := range parseCityPlaces(dutchPlaces, nil, false) {
			citiesParsed = mergeCity(citiesParsed, city) // Natural Earth may already name the larger ones
		}
		slices.SortFunc(citiesParsed, func(a, b cityPlace) int {
			if a.rank != b.rank {
				return a.rank - b.rank
			}
			if a.local != b.local { // the curated local tier leads its rank
				if a.local {
					return -1
				}
				return 1
			}
			if a.population != b.population { // the principal city wins a dense area's spacing
				return b.population - a.population
			}
			return strings.Compare(a.name, b.name)
		})
	})
	return citiesParsed
}

func mergeCity(list []cityPlace, city cityPlace) []cityPlace {
	for i := range list {
		if samePlace(list[i], city) {
			if city.population > list[i].population {
				list[i].population = city.population
			}
			return list
		}
	}
	return append(list, city)
}

func samePlace(a, b cityPlace) bool {
	return strings.EqualFold(a.name, b.name) && math.Abs(a.lon-b.lon) < 0.1 && math.Abs(a.lat-b.lat) < 0.1
}

func parseCityPlaces(data string, into []cityPlace, local bool) []cityPlace {
	for line := range strings.SplitSeq(data, "\n") {
		name, rest, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		lon, rest, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		lat, rankRest, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		rankText, populationText, _ := strings.Cut(rankRest, "|")
		x, errX := strconv.ParseFloat(lon, 64)
		y, errY := strconv.ParseFloat(lat, 64)
		rank, errRank := strconv.Atoi(rankText)
		population, _ := strconv.Atoi(populationText)
		if errX != nil || errY != nil || errRank != nil || name == "" {
			continue
		}
		into = append(into, cityPlace{name: name, lon: x, lat: y, rank: rank, population: population, local: local})
	}
	return into
}

func countryData() countryIndex {
	countryOnce.Do(func() {
		for line := range strings.SplitSeq(countryPolygons, "\n") {
			head, rings, ok := strings.Cut(line, "|")
			if !ok {
				continue
			}
			shape := countryShape{name: head, west: 180, east: -180, south: 90, north: -90}
			for part := range strings.SplitSeq(rings, "|") {
				var ring []float64
				for pair := range strings.SplitSeq(part, " ") {
					lon, lat, ok := strings.Cut(pair, ",")
					x, errX := strconv.ParseFloat(lon, 64)
					y, errY := strconv.ParseFloat(lat, 64)
					if !ok || errX != nil || errY != nil {
						ring = nil // unparsable coordinate: skip the ring, never guess a place
						break
					}
					ring = append(ring, x, y)
					shape.west, shape.east = min(shape.west, x), max(shape.east, x)
					shape.south, shape.north = min(shape.south, y), max(shape.north, y)
				}
				if len(ring) >= 6 { // a ring needs three points to hold an area
					shape.rings = append(shape.rings, ring)
				}
			}
			if len(shape.rings) > 0 {
				countryParsed.countries = append(countryParsed.countries, shape)
			}
		}
		for line := range strings.SplitSeq(countryTiny, "\n") {
			name, rest, ok := strings.Cut(line, "|")
			if !ok {
				continue
			}
			lon, lat, ok := strings.Cut(rest, ",")
			x, errX := strconv.ParseFloat(lon, 64)
			y, errY := strconv.ParseFloat(lat, 64)
			if !ok || errX != nil || errY != nil {
				continue
			}
			countryParsed.tiny = append(countryParsed.tiny, struct {
				name string
				lat  float64
				lon  float64
			}{name, y, x})
		}
	})
	return countryParsed
}

// evenOdd reports whether the point sits inside the flat lon,lat rings under the even-odd rule.
func evenOdd(rings [][]float64, px, py float64) bool {
	inside := false
	for _, ring := range rings {
		n := len(ring) / 2
		for j := 0; j < n; j++ {
			i := (j - 1 + n) % n // the edge from the previous point, wrapping to close the ring
			x0, y0, x1, y1 := ring[2*i], ring[2*i+1], ring[2*j], ring[2*j+1]
			if (y0 > py) == (y1 > py) {
				continue
			}
			if px < x0+(py-y0)*(x1-x0)/(y1-y0) {
				inside = !inside
			}
		}
	}
	return inside
}

// countryAt names the country whose polygon holds the point and returns the bounds of the ring
// that holds it — metropolitan France, not France with French Guiana — as west, east, south and
// north in degrees. A point no polygon holds (a coastal fix past the 110m generalisation, a
// microstate) falls through to the tiny-country points.
func countryAt(lat, lon float64) (name string, west, east, south, north float64, ok bool) {
	data := countryData()
	for _, shape := range data.countries {
		if lon < shape.west || lon > shape.east || lat < shape.south || lat > shape.north {
			continue
		}
		if !evenOdd(shape.rings, lon, lat) {
			continue
		}
		for _, ring := range shape.rings {
			if evenOdd([][]float64{ring}, lon, lat) {
				west, east, south, north = 180, -180, 90, -90
				for k := 0; k < len(ring)/2; k++ {
					x, y := ring[2*k], ring[2*k+1]
					west, east = min(west, x), max(east, x)
					south, north = min(south, y), max(north, y)
				}
				return shape.name, west, east, south, north, true
			}
		}
		return shape.name, shape.west, shape.east, shape.south, shape.north, true
	}
	// Countries the 110m polygons leave out (Malta, Monaco, Singapore…): a tiny-country point
	// nearby wins, with a window big enough to frame it generously.
	for _, entry := range data.tiny {
		if math.Abs(lat-entry.lat) <= 0.25 && math.Abs(lon-entry.lon) <= 0.25 {
			return entry.name, entry.lon - 0.5, entry.lon + 0.5, entry.lat - 0.5, entry.lat + 0.5, true
		}
	}
	return "", 0, 0, 0, 0, false
}

package web

import (
	"math"
	"testing"
)

// TestCountryAtFrameRing checks the point-in-polygon lookup and the ring choice that makes the
// adverts frame a country the operator recognises: the ring containing the point (MON-AD-6, D43).
func TestCountryAtFrameRing(t *testing.T) {
	name, west, east, south, north, ok := countryAt(52.7792, 6.9069) // Emmen
	if !ok || name != "Netherlands" {
		t.Fatalf("Emmen: %q %v", name, ok)
	}
	// The mainland ring, not a bounding box over every overseas part of the country.
	if west < 3 || east > 7.5 || south < 50 || north > 54 {
		t.Fatalf("Netherlands ring bounds are not the mainland: %v %v %v %v", west, east, south, north)
	}
	// Home inside the ring bounds, since the ring is the one holding it.
	if west > 6.9069 || east < 6.9069 || south > 52.7792 || north < 52.7792 {
		t.Errorf("home does not sit inside the returned ring bounds")
	}
	if _, _, _, _, _, ok := countryAt(-31.95, 115.86); !ok {
		t.Errorf("Perth names no country")
	}
	name, west, east, south, north, _ = countryAt(39.5, -98.35) // Kansas
	if name != "United States of America" {
		t.Fatalf("Kansas: %q", name)
	}
	// The conterminous ring, not the whole sovereignty with Alaska and Hawaii in its bounds.
	if west < -125 || east > -66 || south < 24 || north > 50 {
		t.Fatalf("US ring bounds are not conterminous: %v %v %v %v", west, east, south, north)
	}
	// Alaska is its own ring of the United States feature, so the frame fits Alaska when home is
	// there — not the 24°N–49°N conterminous bounds nor the whole sovereignty.
	name, west, east, south, north, _ = countryAt(64.9, -147.1) // Fairbanks
	if name != "United States of America" || south < 50 || north > 72 || east > -129 {
		t.Fatalf("Fairbanks: %q %v %v %v %v", name, west, east, south, north)
	}
}

// TestCountryAtTiny checks the fallback for countries the 110m polygons leave out (MON-AD-6).
func TestCountryAtTiny(t *testing.T) {
	name, west, east, south, north, ok := countryAt(35.9375, 14.3754) // Malta
	if !ok || name != "Malta" {
		t.Fatalf("Malta: %q %v", name, ok)
	}
	if east-west != 1 || north-south != 1 {
		t.Errorf("tiny countries get a ±0.5° window, got %v × %v", east-west, north-south)
	}
	if _, _, _, _, _, ok := countryAt(0, 0); ok {
		t.Errorf("Null Island names a country")
	}
}

// TestMercatorY pins the projection against hand-computed values and the equator.
func TestMercatorY(t *testing.T) {
	if y := mercatorY(0); math.Abs(y) > 1e-9 {
		t.Errorf("mercatorY(0) = %v, want 0", y)
	}
	// 180/π · ln(1+√2) ≈ 50.49 for 45° N.
	if y := mercatorY(45); y < 50.0 || y > 51.0 {
		t.Errorf("mercatorY(45) = %v, want 50.49", y)
	}
	if y := mercatorY(-45); y > -50.0 || y < -51.0 {
		t.Errorf("mercatorY(-45) = %v, want -50.49", y)
	}
	if y := mercatorY(89); y < 178 || y > 180 { // clamped past 85°: the tangent stays bounded
		t.Errorf("mercatorY(89) = %v, want the 85° clamped value ≈ 179.4", y)
	}
}

#!/bin/sh
# Regenerate internal/web/basemap_cities_nl.go from the GeoNames Netherlands dump.
# Populated places (feature class P, excluding PPLX city sections) with population >= 30,000.
# GeoNames data is licensed CC BY 4.0: https://www.geonames.org/
set -eu
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL https://download.geonames.org/export/dump/NL.zip -o "$tmp/NL.zip"
unzip -p "$tmp/NL.zip" NL.txt |
	awk -F'\t' '$7=="P" && $8!="PPLX" && $15+0>=30000 && $2!="Hoogvliet" && $2!="Ypenburg" {
		n=$2; if (n=="Almere Stad") n="Almere";
		print n "|" $6 "|" $5 "|7|" $15
	}' |
	sort -t'|' -k1,1 > "$tmp/places.txt"
awk 'BEGIN{
	print "package web";
	print "";
	print "// Code generated from the GeoNames NL dump; do not edit by hand.";
	print "// Populated places (feature class P, excluding PPLX sections) with population >= 30,000,";
	print "// in NAME|lon|lat|rank|population form. This supplements Natural Earth at the";
	print "// Emmen/Coevorden tier; the runtime merge folds population into Natural Earth duplicates.";
	print "// Source: https://download.geonames.org/export/dump/NL.zip, licensed CC BY 4.0";
	print "// (https://www.geonames.org/).";
	print "const dutchPlaces = `";
} { print } END { print "`" }' "$tmp/places.txt" > internal/web/basemap_cities_nl.go

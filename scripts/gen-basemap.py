#!/usr/bin/env python3
"""Generate internal/web/basemap_data.go from Natural Earth 1:110m (public domain).

Run from the kammon root: python3 scripts/gen-basemap.py

The output contains only longitude/latitude coordinates and SVG path commands, rounded to
0.01 degree. Natural Earth is public domain: https://www.naturalearthdata.com/
"""

import json
import urllib.request

BASE = "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/"
LAND = BASE + "ne_110m_land.geojson"
BORDERS = BASE + "ne_110m_admin_0_boundary_lines_land.geojson"
OUT = "internal/web/basemap_data.go"


def num(value):
    text = f"{value:.2f}".rstrip("0").rstrip(".")
    return "0" if text in ("", "-0") else text


def points(coords):
    out = []
    for lon, lat in coords:
        point = (num(lon), num(lat))
        if not out or out[-1] != point:
            out.append(point)
    return out


def path(coords, close):
    pts = points(coords)
    if len(pts) < (3 if close else 2):
        return ""
    out = ["M" + pts[0][0] + " " + pts[0][1]]
    out += ["L" + lon + " " + lat for lon, lat in pts[1:]]
    if close:
        out.append("Z")
    return "".join(out)


def collect(url, polygons):
    with urllib.request.urlopen(url, timeout=30) as response:
        data = json.load(response)
    parts = []
    for feature in data["features"]:
        kind = feature["geometry"]["type"]
        coords = feature["geometry"]["coordinates"]
        if polygons:
            if kind == "Polygon":
                rings = coords
            elif kind == "MultiPolygon":
                rings = [ring for polygon in coords for ring in polygon]
            else:
                continue
            parts += [path(ring, True) for ring in rings]
        else:
            if kind == "LineString":
                lines = [coords]
            elif kind == "MultiLineString":
                lines = coords
            else:
                continue
            parts += [path(line, False) for line in lines]
    return "".join(part for part in parts if part)


def main():
    land = collect(LAND, True)
    borders = collect(BORDERS, False)
    with open(OUT, "w") as handle:
        handle.write(
            "// Code generated from Natural Earth 1:110m (public domain) on 2026-11-22; DO NOT EDIT.\n"
            "// Coordinates are longitude/latitude pairs in SVG path commands, rounded to 0.01 degree.\n"
            "// The land outline and country borders are numbers only, so this data can never carry\n"
            "// text from the radio. Regenerate with scripts/gen-basemap.py.\n"
            "package web\n\n"
        )
        handle.write("// basemapLand is every Natural Earth 1:110m land polygon, in longitude/latitude space.\n")
        handle.write("const basemapLand = `" + land + "`\n\n")
        handle.write("// basemapBorders is every Natural Earth 1:110m country border line, in longitude/latitude space.\n")
        handle.write("const basemapBorders = `" + borders + "`\n")
    print(f"wrote {OUT}: land {len(land)} bytes, borders {len(borders)} bytes")


if __name__ == "__main__":
    main()

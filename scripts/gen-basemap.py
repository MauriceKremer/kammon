#!/usr/bin/env python3
"""Generate internal/web/basemap_svg.go, basemap_countries.go and basemap_cities.go from Natural
Earth (public domain): https://www.naturalearthdata.com/

Run from the kammon root: python3 scripts/gen-basemap.py

- basemap_svg.go holds the drawn basemap: Natural Earth 1:50m land, country borders and lakes,
  already projected to Web Mercator (x = longitude in degrees, y = 180/pi * ln(tan(45° + lat/2)),
  rounded to 0.01 — about 1.1 km at the equator). The adverts map draws them through one
  <use> reference (research/advert-map-basemap.md §3).
- basemap_countries.go holds the country lookup that frames the map near the configured location:
  1:110m admin-0 polygons per country and 1:110m tiny-country points. It is never drawn
  (research/advert-map-basemap.md §5).
- basemap_cities.go holds the major cities drawn over the basemap: Natural Earth 10m populated
  places up to a SCALERANK cap. Names are vendored data, never radio text.
"""

import json
import math
import urllib.request

BASE = "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/"
LAND = BASE + "ne_50m_land.geojson"
BORDERS = BASE + "ne_50m_admin_0_boundary_lines_land.geojson"
LAKES = BASE + "ne_50m_lakes.geojson"
COUNTRIES = BASE + "ne_110m_admin_0_countries.geojson"
TINY = BASE + "ne_110m_admin_0_tiny_countries.geojson"
CITIES = BASE + "ne_10m_populated_places.geojson"
CITIES_MAX_RANK = 10  # the full Natural Earth town list: 0 is the most major
SVG_OUT = "internal/web/basemap_svg.go"
COUNTRIES_OUT = "internal/web/basemap_countries.go"
CITIES_OUT = "internal/web/basemap_cities.go"


def merc_y(lat):
    """Web Mercator y for a latitude in degrees, in the same units as longitude degrees."""
    return 180 / math.pi * math.log(math.tan(math.pi / 4 + math.radians(lat) / 2))


def num(value):
    text = f"{value:.2f}".rstrip("0").rstrip(".")
    return "0" if text in ("", "-0") else text


def points(coords):
    """Project to Mercator, rounded to 0.01, consecutive repeats dropped. Floats, for simplify()."""
    out = []
    for lon, lat in coords:
        point = (round(lon, 2), round(merc_y(lat), 2))
        if not out or out[-1] != point:
            out.append(point)
    return out


def simplify(pts, tolerance):
    """Douglas-Peucker on projected points; keeps paths within `tolerance` units of the original."""
    if len(pts) < 3:
        return pts
    keep = [False] * len(pts)
    keep[0] = keep[-1] = True
    stack = [(0, len(pts) - 1)]
    while stack:
        lo, hi = stack.pop()
        if hi <= lo + 1:
            continue
        ax, ay = pts[lo]
        bx, by = pts[hi]
        dx, dy = bx - ax, by - ay
        norm2 = dx * dx + dy * dy
        far, far_d = -1, -1.0
        for i in range(lo + 1, hi):
            px, py = pts[i]
            if norm2 == 0:
                d = math.hypot(px - ax, py - ay)
            else:
                d = abs(dy * (px - ax) - dx * (py - ay)) / math.sqrt(norm2)
            if d > far_d:
                far, far_d = i, d
        if far_d > tolerance:
            keep[far] = True
            stack += [(lo, far), (far, hi)]
    return [p for p, k in zip(pts, keep) if k]


def path(coords, close, tolerance=0.0):
    """One SVG subpath of implicit linetos after the M: `M x y x y x y`."""
    pts = simplify(points(coords), tolerance) if tolerance else points(coords)
    if len(pts) < (3 if close else 2):
        return ""
    rest = "".join(f" {num(lon)} {num(y)}" for lon, y in pts[1:])
    return f"M{num(pts[0][0])} {num(pts[0][1])}{rest}" + ("Z" if close else "")


def ring_pairs(coords):
    """A ring as flat lon,lat pairs on 0.01, first and last points not repeated, at least 3."""
    out = []
    for i, (lon, lat) in enumerate(coords):
        point = (num(lon), num(lat))
        if out and out[-1] == point:
            continue
        if i == len(coords) - 1 and out and out[0] == point:
            continue  # the closing repeat of the first point
        out.append(point)
    if len(out) < 3:
        return []
    return out


def fetch(url):
    with urllib.request.urlopen(url, timeout=60) as response:
        return json.load(response)


def collect(url, polygons, tolerance=0.012):
    """SVG path strings, one per ring (polygons) or line (lines). Every path is simplified by
    Douglas-Peucker: a 0.012-unit corridor (~1.3 km) keeps country-level and 16×-zoom views crisp
    without the raw 50m point weight. Lakes take a finer corridor: their narrow rings collapse
    into self-intersections under more, and even-odd filling then spills over the land."""
    parts = []
    for feature in fetch(url)["features"]:
        kind = feature["geometry"]["type"]
        coords = feature["geometry"]["coordinates"]
        if polygons:
            if kind == "Polygon":
                rings = coords
            elif kind == "MultiPolygon":
                rings = [ring for polygon in coords for ring in polygon]
            else:
                continue
            parts += [path(ring, True, tolerance) for ring in rings]
        else:
            if kind == "LineString":
                coords = [coords]
            elif kind != "MultiLineString":
                continue
            parts += [path(ring, False, tolerance) for ring in coords]
    return "".join(part for part in parts if part)


def write(out, name, string):
    body = f"package web\n\n{string}\n"
    with open(out, "w") as handle:
        handle.write(body)
    print(f"{out}: {len(body) / 1024:.0f} kB ({name})")


def main():
    land = collect(LAND, True)
    borders = collect(BORDERS, False)
    lakes = collect(LAKES, True, 0.005)  # narrow lakes collapse under the usual corridor

    svg = "".join([
        "// Code generated by scripts/gen-basemap.py; DO NOT EDIT.\n//\n",
        "// basemapSVG is the drawn basemap of the adverts map: Natural Earth 1:50m land, country\n",
        "// borders and lakes (public domain), projected once to Web Mercator at generate time. The\n",
        "// map places them with one <use> reference served from /static/basemap.svg\n",
        "// (research/advert-map-basemap.md). The colours carry the page theme through CSS custom\n",
        "// properties with dark-mode fallbacks.\n",
        'const basemapSVG = `<svg xmlns="http://www.w3.org/2000/svg"><g id="basemap">\n',
        '<path fill-rule="evenodd" vector-effect="non-scaling-stroke" '
        'style="fill:var(--map-land,#2c2c30);stroke:var(--map-land-line,#ffffff12);stroke-width:1" '
        f'd="{land}"/>\n',
        '<path fill-rule="evenodd" vector-effect="non-scaling-stroke" '
        'style="fill:var(--map-lake,#18181b)" '
        f'd="{lakes}"/>\n',
        '<path fill="none" vector-effect="non-scaling-stroke" '
        'style="fill:none;stroke:var(--map-border,#ffffff1f)" '
        f'd="{borders}"/>\n',
        "</g></svg>`\n",
    ])
    write(SVG_OUT, "basemap SVG", svg)

    # The country lookup: one line per country: name|ring|ring..., a ring of "lon,lat lon,lat".
    lines = []
    for feature in fetch(COUNTRIES)["features"]:
        name = feature["properties"].get("NAME", "")
        geometry = feature["geometry"]
        if not name or geometry["type"] not in ("Polygon", "MultiPolygon"):
            continue
        if geometry["type"] == "Polygon":
            rings = [geometry["coordinates"]]
        else:
            rings = geometry["coordinates"]
        parts = []
        for polygon in rings:
            for ring in polygon:
                pairs = ring_pairs(ring)
                if pairs:
                    parts.append(" ".join(f"{lon},{lat}" for lon, lat in pairs))
        if parts:
            lines.append(name + "|" + "|".join(parts))

    tiny = ["%s|%s,%s" % (
        feature["properties"].get("NAME", ""),
        num(feature["geometry"]["coordinates"][0]),
        num(feature["geometry"]["coordinates"][1]),
    ) for feature in fetch(TINY)["features"] if feature["properties"].get("NAME")]

    countries_go = "".join([
        "// Code generated by scripts/gen-basemap.py; DO NOT EDIT.\n//\n",
        "// The country lookup that frames the adverts map near the configured location: Natural Earth\n",
        "// 1:110m admin-0 country polygons as name|ring|ring lines (rings of lon,lat points) and\n",
        "// 1:110m tiny-country points that the 110m polygons leave out. Never drawn;\n",
        "// internal/web/countries.go parses and matches it (research/advert-map-basemap.md §5).\n",
        "const countryPolygons = `" + "\n".join(lines) + "`\n",
        "\nconst countryTiny = `" + "\n".join(tiny) + "`\n",
    ])
    write(COUNTRIES_OUT, "country lookup", countries_go)

    # The major cities: one line per city NAME|lon|lat|rank, rank 0 the most major.
    city_lines = []
    for feature in fetch(CITIES)["features"]:
        rank = feature["properties"].get("SCALERANK")
        name = feature["properties"].get("NAME", "")
        coords = feature["geometry"]["coordinates"]
        if rank is None or not name or rank > CITIES_MAX_RANK:
            continue
        city_lines.append(f"{name}|{num(coords[0])}|{num(coords[1])}|{int(rank)}")
    print(f"cities kept (SCALERANK <= {CITIES_MAX_RANK}): {len(city_lines)}")

    cities_go = "".join([
        "// Code generated by scripts/gen-basemap.py; DO NOT EDIT.\n//\n",
        "// The major cities of the adverts map: Natural Earth 10m populated places (public domain) of\n",
        "// SCALERANK up to ", str(CITIES_MAX_RANK), ", one NAME|lon|lat|rank per line, rank 0 the most major.\n",
        "// internal/web/countries.go parses and chart.go draws them filtered to the frame; the names\n",
        "// are vendored data, never radio text.\n",
        'const cityPlaces = `" + "" + "'.replace(' + \"\" + ', '') + "`\n" if False else
        'const cityPlaces = `' + "\n".join(city_lines) + "`\n",
    ])
    write(CITIES_OUT, "city places", cities_go)


if __name__ == "__main__":
    main()

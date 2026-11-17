"""Builds region.geojson, the area whose map data the maps service holds
(task 100, ADR 0063, p. 4): the countries the owner chose on 2026-11-15
(Europe with Turkey and Cyprus, Russia, the other CIS countries, North,
Central and South America, the Caribbean and Greenland), each widened by
about 20 km so that a map centred near a border or a coast still has its
surroundings, and cut at the 180th meridian (Chukotka, the Aleutians).

Run by make-region.sh in a pinned Python container:

    python make_region.py <ne_10m_admin_0_countries.geojson> <output.geojson>

The country borders are Natural Earth Admin 0, 1:10m (public domain). Each
polygon of a country is simplified (about 1 km) and buffered (about 20 km)
in an azimuthal equidistant projection centred on that polygon, so the
distances are metres wherever the polygon lies; every Natural Earth polygon
already ends at the 180th meridian, and the projection keeps longitudes
continuous around its centre, so a buffer that crosses the meridian comes
back with longitudes beyond 180 and is folded to the other side here.
"""

import json
import math
import sys

import numpy as np
import shapely
from shapely.geometry import MultiPolygon, Polygon, box, mapping
from shapely.affinity import translate

# The continents of Natural Earth taken whole (CONTINENT field).
CONTINENTS = {"Europe", "North America", "South America"}
# Taken from the other continents (ADM0_A3): Turkey, Cyprus with Northern
# Cyprus, the UN buffer zone and the British bases; the Caucasus and Central
# Asia members of the CIS, with the Baikonur lease.
EXTRA = {
    "TUR", "CYP", "CYN", "CNM", "ESB", "WSB",
    "GEO", "ARM", "AZE",
    "KAZ", "KAB", "KGZ", "TJK", "TKM", "UZB",
}
# Left out of the continents above: the United States Minor Outlying
# Islands are specks spread over the Pacific, not the Americas.
EXCLUDE = {"UMI"}

R = 6371008.8  # the mean radius of the Earth, metres
SIMPLIFY_M = 1000.0
BUFFER_M = 20000.0
# The latitude limit of Web Mercator: no tile lies beyond it.
MAX_LAT = 85.0511
# The output grid: 1e-4 degrees, about 11 m.
GRID = 1e-4
# The final simplification in degrees (about 1 km along a meridian); it
# cuts at most that much off the 20 km margin and keeps the file small.
FINAL_SIMPLIFY_DEG = 0.01


def aeqd(lon0, lat0):
    """Forward and inverse spherical azimuthal equidistant projection
    centred on (lon0, lat0), as numpy functions of an (n, 2) array."""
    l0 = math.radians(lon0)
    p0 = math.radians(lat0)
    sp0, cp0 = math.sin(p0), math.cos(p0)

    def forward(xy):
        lam = np.radians(xy[:, 0]) - l0
        phi = np.radians(xy[:, 1])
        sp, cp = np.sin(phi), np.cos(phi)
        cosc = np.clip(sp0 * sp + cp0 * cp * np.cos(lam), -1.0, 1.0)
        c = np.arccos(cosc)
        sinc = np.sin(c)
        k = np.where(c < 1e-12, 1.0, c / np.where(sinc == 0, 1.0, sinc))
        x = R * k * cp * np.sin(lam)
        y = R * k * (cp0 * sp - sp0 * cp * np.cos(lam))
        return np.column_stack([x, y])

    def inverse(xy):
        x, y = xy[:, 0], xy[:, 1]
        rho = np.hypot(x, y)
        c = rho / R
        sinc, cosc = np.sin(c), np.cos(c)
        safe = np.where(rho == 0, 1.0, rho)
        phi = np.where(
            rho == 0,
            p0,
            np.arcsin(np.clip(cosc * sp0 + y * sinc * cp0 / safe, -1.0, 1.0)),
        )
        # atan2 gives the longitude relative to the centre in (-180, 180]:
        # continuous across the 180th meridian.
        lam = l0 + np.arctan2(x * sinc, rho * cp0 * cosc - y * sp0 * sinc)
        return np.column_stack([np.degrees(lam), np.degrees(phi)])

    return forward, inverse


def widen(polygon):
    """Simplifies and buffers one polygon in metres around its own centre."""
    minx, miny, maxx, maxy = polygon.bounds
    forward, inverse = aeqd((minx + maxx) / 2, (miny + maxy) / 2)
    projected = shapely.transform(polygon, forward)
    projected = projected.simplify(SIMPLIFY_M, preserve_topology=True)
    widened = projected.buffer(BUFFER_M, quad_segs=4)
    return shapely.transform(widened, inverse)


def polygons(geometry):
    if isinstance(geometry, Polygon):
        return [geometry]
    if isinstance(geometry, MultiPolygon):
        return list(geometry.geoms)
    if geometry.is_empty:
        return []
    raise ValueError(f"unexpected geometry {geometry.geom_type}")


def fold(geometry):
    """Moves what lies beyond the 180th meridian to the other side and
    keeps what lies within Web Mercator's latitudes."""
    world = box(-180, -MAX_LAT, 180, MAX_LAT)
    parts = [geometry.intersection(world)]
    east = geometry.intersection(box(180, -MAX_LAT, 540, MAX_LAT))
    if not east.is_empty:
        parts.append(translate(east, xoff=-360))
    west = geometry.intersection(box(-540, -MAX_LAT, -180, MAX_LAT))
    if not west.is_empty:
        parts.append(translate(west, xoff=360))
    return shapely.union_all(parts)


def main(source, output):
    with open(source, encoding="utf-8") as f:
        countries = json.load(f)

    chosen = []
    widened = []
    for feature in countries["features"]:
        p = feature["properties"]
        code = p["ADM0_A3"]
        if code in EXCLUDE:
            continue
        if p["CONTINENT"] not in CONTINENTS and code not in EXTRA:
            continue
        chosen.append(code)
        geometry = shapely.geometry.shape(feature["geometry"])
        for polygon in polygons(geometry):
            widened.append(widen(polygon))

    missing = EXTRA - set(chosen)
    if missing:
        raise SystemExit(f"countries not found in {source}: {sorted(missing)}")

    region = fold(shapely.union_all(widened))
    region = region.simplify(FINAL_SIMPLIFY_DEG, preserve_topology=True)
    region = shapely.set_precision(region, GRID)
    region = shapely.make_valid(region)
    parts = [g for g in polygons(region) if g.area > 0]
    if not parts:
        raise SystemExit("the region is empty")
    region = MultiPolygon(parts)
    # Already on the grid; rounding makes every coordinate print short.
    region = shapely.transform(region, lambda xy: np.round(xy, 4))
    if not region.is_valid:
        raise SystemExit(f"the region is not valid: {shapely.is_valid_reason(region)}")
    minx, miny, maxx, maxy = region.bounds
    if minx < -180 or maxx > 180 or miny < -MAX_LAT or maxy > MAX_LAT:
        raise SystemExit(f"the region leaves the world: {region.bounds}")

    collection = {
        "type": "FeatureCollection",
        "features": [
            {
                "type": "Feature",
                "properties": {
                    "name": "pluma-maps-region",
                    "source": "Natural Earth Admin 0 1:10m v5.1.2 (public domain)",
                    "buffer_m": BUFFER_M,
                    "countries": sorted(chosen),
                },
                "geometry": mapping(region),
            }
        ],
    }
    with open(output, "w", encoding="utf-8") as f:
        json.dump(collection, f, separators=(",", ":"))
        f.write("\n")
    vertices = shapely.get_num_coordinates(region)
    print(
        f"{len(chosen)} countries, {len(parts)} polygons, {vertices} vertices, "
        f"bounds {tuple(round(v, 4) for v in region.bounds)}"
    )


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: make_region.py <countries.geojson> <output.geojson>")
    main(sys.argv[1], sys.argv[2])

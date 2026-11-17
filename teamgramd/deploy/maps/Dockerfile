# The image of the maps services (docs/ops/maps.md): tileserver-gl on
# Protomaps vector data on the host's disk (/data, read-only), started with
# one of two configs:
# - /app/config.json, the maps service (task 100, ADR 0063): draws the static
#   map images bff asks for upload.getWebFile;
# - /app/config-public.json, the maps-tiles service (task 112, ADR 0070):
#   serves the styles, glyphs, sprites and vector tiles of the clients'
#   interactive maps, through Caddy, without a renderer.
#
# Everything either reads is local: the styles are built here, the glyphs
# and the sprites are baked in, the tiles are the file in /data. Built from
# the root of the repository (docker-compose.maps.yaml):
#
#   docker build -f teamgramd/deploy/maps/Dockerfile -t pluma-maps:local .
#
# MAPS_LANGS is the list of label languages (default ru,en; the same list as
# MAPS_STYLES of bff), a light style each and a dark one for the public
# config: every rendered style has renderers of its own, so each language
# costs memory (docs/ops/maps.md, "Память").
#
# The renderer pools of config.json are at most one renderer per style and
# scale factor, none kept idle: bff draws at most MAPS_CONCURRENCY (4) images
# at once, and a renderer is several hundred megabytes. Measured on
# 2026-11-16 (two styles, every scale 1-4, sizes up to 1024x1024@2 and
# 512x512@4, 8 at once): about 300 MiB idle, a peak of about 2 GiB, a render
# of 0.1-0.3 s for a chat bubble and up to 2 s for the largest images, the
# first one after idling about 0.7 s. Pools of two renderers reached 3 GiB.

# The styles: @protomaps/basemaps 5.7.2 (BSD-3-Clause), by package-lock.json.
FROM node:24.22.3-trixie-slim@sha256:af02d57b57719b9657a45199a79264a9437940cd55b3c7b3fc99615654f124a0 AS styles
WORKDIR /build
COPY teamgramd/deploy/maps/package.json teamgramd/deploy/maps/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY teamgramd/deploy/maps/build-styles.mjs teamgramd/deploy/maps/config.json ./
ARG MAPS_LANGS=ru,en
RUN node build-styles.mjs "$MAPS_LANGS" config.json /out

# Glyphs and sprites: protomaps/basemaps-assets at commit 028c18f7 of
# 2025-10-31, the archive of that commit checked by its SHA-256. Fonts: Noto
# Sans (SIL OFL 1.1; Devanagari is mostly links to Regular); sprites v4 of
# the light and the dark flavor, 1x and 2x (MIT, the icons of Mapzen; see
# licenses/protomaps-basemaps.LICENSE).
FROM node:24.22.3-trixie-slim@sha256:af02d57b57719b9657a45199a79264a9437940cd55b3c7b3fc99615654f124a0 AS assets
ADD --checksum=sha256:57e40e8c512bd8042d0a3a251f19d0d1c8523ad963c666c3c6643bada4dc92d0 \
    https://github.com/protomaps/basemaps-assets/archive/028c18f713baecad011301ff7a69acc39bcc2ae7.tar.gz /assets.tar.gz
RUN mkdir /src /out \
 && tar -xzf /assets.tar.gz -C /src --strip-components=1 \
 && mkdir /out/fonts /out/sprites \
 && for f in "Noto Sans Regular" "Noto Sans Medium" "Noto Sans Italic" "Noto Sans Devanagari Regular v1"; do \
      cp -a "/src/fonts/$f" /out/fonts/; \
    done \
 && cp /src/sprites/v4/light.json /src/sprites/v4/light.png \
       /src/sprites/v4/light@2x.json /src/sprites/v4/light@2x.png \
       /src/sprites/v4/dark.json /src/sprites/v4/dark.png \
       /src/sprites/v4/dark@2x.json /src/sprites/v4/dark@2x.png /out/sprites/ \
 && cp /src/fonts/OFL.txt /out/fonts.OFL.txt

# tileserver-gl v5.7.0 (BSD-2-Clause, /usr/src/app/LICENSE.md), the image
# index of the release (amd64 sha256:61f07d91…). Not lower: from v5.7.0 the
# zoom of a static image is that of 256-pixel tiles, as the clients count it
# (src/render_mode.js:10-13), so bff passes the client's zoom as it is.
FROM maptiler/tileserver-gl:v5.7.0@sha256:24d0364e1d40da0ae3fb3269f617c26e36a883ad4b7454e5c4064a8795bfc807
COPY --from=styles /out/config.json /out/config-public.json /app/
COPY --from=styles /out/styles/ /app/styles/
COPY --from=assets /out/fonts/ /app/fonts/
COPY --from=assets /out/sprites/ /app/sprites/
COPY --from=assets /out/fonts.OFL.txt /licenses/noto-sans.OFL.txt
COPY teamgramd/deploy/maps/licenses/ /licenses/
# The only host bff calls the maps service by (MAPS_URL). With --config,
# tileserver-gl v5.7.0 takes the allowed hosts from this variable only
# (src/main.js:177-187 reads config.options before the file is loaded); they
# matter only for the URLs of JSON answers, which bff does not ask for. The
# maps-tiles service sets it to the public name of the site
# (docker-compose.maps.yaml).
ENV TILESERVER_GL_ALLOWED_HOSTS=maps:8080
# /health answers 200 once every style is loaded and its renderers, if any,
# exist.
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["node", "/usr/src/app/src/healthcheck.js"]
# The entrypoint of the image starts Xvfb (it writes /tmp/.X99-lock) and
# then tileserver-gl with these arguments; the compose file adds the log
# options.
CMD ["--config", "/app/config.json"]

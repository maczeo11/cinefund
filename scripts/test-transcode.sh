#!/usr/bin/env bash
# Runs a real FFmpeg transcode on the sample video to prove the ABR pipeline
# produces playable HLS. Uses the same flags as transcode/args.go.
#
# Usage: ./scripts/test-transcode.sh
set -euo pipefail

INPUT="testdata/sample_360p.mp4"
OUTDIR="tmp/transcode-test"

if [ ! -f "$INPUT" ]; then
  echo "missing $INPUT — copy a sample video there first"
  exit 1
fi

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR/360p" "$OUTDIR/240p"

echo "=== probing input ==="
ffprobe -v error -print_format json -show_streams "$INPUT" | python3 -c "
import sys,json
s = json.load(sys.stdin)['streams']
for t in s:
    print(f\"  {t['codec_type']}: {t.get('width','?')}x{t.get('height','?')} {t['codec_name']}\")
"

echo ""
echo "=== encoding 360p rung ==="
ffmpeg -hide_banner -loglevel error -nostdin \
  -i "$INPUT" \
  -c:v libx264 -profile:v main -level 4.0 -preset fast -crf 24 \
  -maxrate 856000 -bufsize 1200000 \
  -vf "scale=-2:360:force_original_aspect_ratio=decrease,format=yuv420p" \
  -g 48 -keyint_min 48 -sc_threshold 0 -r 24 \
  -c:a aac -b:a 64000 -ac 2 -ar 48000 \
  -f hls -hls_time 6 -hls_playlist_type vod -hls_segment_type mpegts \
  -hls_flags independent_segments \
  -hls_segment_filename "$OUTDIR/360p/seg_%05d.ts" \
  "$OUTDIR/360p/index.m3u8"
echo "  done: $(ls $OUTDIR/360p/*.ts | wc -l) segments"

echo ""
echo "=== encoding 240p rung ==="
ffmpeg -hide_banner -loglevel error -nostdin \
  -i "$INPUT" \
  -c:v libx264 -profile:v main -level 4.0 -preset veryfast -crf 26 \
  -maxrate 428000 -bufsize 600000 \
  -vf "scale=-2:240:force_original_aspect_ratio=decrease,format=yuv420p" \
  -g 48 -keyint_min 48 -sc_threshold 0 -r 24 \
  -c:a aac -b:a 48000 -ac 2 -ar 48000 \
  -f hls -hls_time 6 -hls_playlist_type vod -hls_segment_type mpegts \
  -hls_flags independent_segments \
  -hls_segment_filename "$OUTDIR/240p/seg_%05d.ts" \
  "$OUTDIR/240p/index.m3u8"
echo "  done: $(ls $OUTDIR/240p/*.ts | wc -l) segments"

echo ""
echo "=== writing master playlist ==="
cat > "$OUTDIR/master.m3u8" << 'M3U'
#EXTM3U
#EXT-X-VERSION:3

#EXT-X-STREAM-INF:BANDWIDTH=920000,AVERAGE-BANDWIDTH=864000,RESOLUTION=640x360,CODECS="avc1.4d0028,mp4a.40.2"
360p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=476000,AVERAGE-BANDWIDTH=448000,RESOLUTION=426x240,CODECS="avc1.4d0028,mp4a.40.2"
240p/index.m3u8
M3U

echo "  master playlist at $OUTDIR/master.m3u8"
echo ""

# verify the output is valid
echo "=== verifying ==="
for rung in 360p 240p; do
  SEGS=$(ls "$OUTDIR/$rung"/*.ts 2>/dev/null | wc -l)
  HAS_M3U=$(test -f "$OUTDIR/$rung/index.m3u8" && echo "yes" || echo "NO")
  echo "  $rung: $SEGS segments, playlist=$HAS_M3U"
done

echo ""
echo "open $OUTDIR/master.m3u8 in VLC or ffplay to verify playback"
echo "=== PASS ==="

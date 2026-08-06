package transcode

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
)

// BuildMasterPlaylist renders the master m3u8 for a completed ladder.
//
// It is written ONLY after every rung has fully uploaded. That ordering is what
// makes the job atomic from a player's point of view: until the master exists
// there is nothing to play, and once it exists everything it references is
// already in storage. A master written first is a player showing a broken
// stream for the duration of the encode.
func BuildMasterPlaylist(ladder []Rung, srcW, srcH int) []byte {
	// Highest bandwidth first. Many players fetch the first variant for the
	// initial segment; highest-first favours quality on a good connection and
	// the ABR algorithm corrects within a segment or two either way.
	sorted := make([]Rung, len(ladder))
	copy(sorted, ladder)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].PeakBandwidth() > sorted[j].PeakBandwidth()
	})

	var b bytes.Buffer
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")

	for _, r := range sorted {
		w := r.Width(srcW, srcH)
		fmt.Fprintf(&b,
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=\"%s,%s\"\n",
			r.PeakBandwidth(), r.AverageBandwidth(), w, r.Height,
			codecString(), audioCodecString)
		fmt.Fprintf(&b, "%s/index.m3u8\n", r.Name)
	}
	return b.Bytes()
}

// RenditionPrefix is the deterministic storage prefix for one rung.
//
// Deterministic keys mean a re-run overwrites byte-identical content in place:
// no duplicates, no orphans, no _retry2 suffixes. pipelineVersion in the path
// means bumping it produces a fresh tree, so the old one can be dropped by a
// lifecycle rule once the new one is live - a swap that feels atomic without
// needing to be.
func RenditionPrefix(assetID string, pipelineVersion int, rung string) string {
	return path.Join("renditions", assetID, fmt.Sprintf("v%d", pipelineVersion), rung)
}

// MasterKey is where the master playlist lives.
func MasterKey(assetID string, pipelineVersion int) string {
	return path.Join("renditions", assetID, fmt.Sprintf("v%d", pipelineVersion), "master.m3u8")
}

// Presigner signs a storage key for reading.
type Presigner interface {
	PresignedGet(key string) (string, error)
}

// RewriteVariantPlaylist replaces each segment filename in a variant playlist
// with a presigned URL.
//
// A master playlist references variants by relative path and variants reference
// segments by relative path. With a private bucket the player fetches the
// playlist with a valid signature and then requests seg_00001.ts with none,
// getting a 403. Rewriting is what lets the bucket stay private without a CDN.
//
// The API serves only playlists - a few KB of text. Segments go straight from
// storage to the player, so video bytes still never pass through Go.
func RewriteVariantPlaylist(playlist []byte, assetID string, pipelineVersion int, rung string, p Presigner) ([]byte, error) {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(playlist))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	prefix := RenditionPrefix(assetID, pipelineVersion, rung)

	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		// A media line. Validate before signing: this playlist is a file we
		// generated, but treating it as untrusted input costs two lines and
		// closes the path where a crafted filename becomes a signed URL to an
		// arbitrary object. Test M12.
		if strings.ContainsAny(line, "/\\") || strings.Contains(line, "..") {
			return nil, fmt.Errorf("unexpected path in playlist: %q", line)
		}

		url, err := p.PresignedGet(path.Join(prefix, line))
		if err != nil {
			return nil, fmt.Errorf("presign %q: %w", line, err)
		}
		out.WriteString(url)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

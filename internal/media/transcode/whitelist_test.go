package transcode

import (
	"context"
	"strings"
	"testing"
)

func whitelistOf(t *testing.T, args []string) map[string]bool {
	t.Helper()
	i := indexOf(args, "-protocol_whitelist")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("no -protocol_whitelist in %v", args)
	}
	set := map[string]bool{}
	for _, p := range strings.Split(args[i+1], ",") {
		set[p] = true
	}
	return set
}

func TestProtocolWhitelistFor(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		allowed []string
		denied  []string
	}{
		{"presigned https url", "https://bucket.s3.ap-south-1.amazonaws.com/uploads/u/a?X-Amz-Signature=abc", []string{"file", "crypto", "https", "tls", "tcp"}, []string{"http", "ftp", "rtmp", "udp", "concat"}},
		{"local minio over http", "http://localhost:9000/bucket/key?X-Amz-Signature=abc", []string{"file", "crypto", "http", "tcp"}, []string{"https", "tls", "ftp"}},
		{"local file", "/tmp/sample.mp4", []string{"file", "crypto"}, []string{"http", "https", "tcp", "tls"}},
		{"empty", "", []string{"file", "crypto"}, []string{"http", "https"}},
	}
	for _, tc := range cases {
		set := map[string]bool{}
		for _, p := range strings.Split(protocolWhitelistFor(tc.input), ",") {
			set[p] = true
		}
		for _, p := range tc.allowed {
			if !set[p] {
				t.Errorf("%s: %q should be allowed, whitelist = %v", tc.name, p, set)
			}
		}
		for _, p := range tc.denied {
			if set[p] {
				t.Errorf("%s: %q must not be allowed, whitelist = %v", tc.name, p, set)
			}
		}
	}
}

// Regression: the encode step used a flat "file,crypto" whitelist, but its
// input is a presigned https URL, so ffmpeg refused to open it ("Protocol
// 'https' not on whitelist") and every real transcode failed.
func TestRunnerCommand_AllowsPresignedHTTPSInput(t *testing.T) {
	url := "https://cinefund-media.s3.ap-south-1.amazonaws.com/uploads/u/a?X-Amz-Signature=abc"
	args := RenditionArgs(url, fullLadder[1], "/tmp/out")

	cmd := NewRunner("ffmpeg").command(context.Background(), args...)
	set := whitelistOf(t, cmd.Args)
	for _, p := range []string{"https", "tls", "tcp", "file", "crypto"} {
		if !set[p] {
			t.Errorf("encode of an https input needs %q, got %v", p, set)
		}
	}
	if set["http"] {
		t.Errorf("plain http must stay blocked for an https input, got %v", set)
	}
}

func TestRunnerCommand_PosterInheritsInputAwareWhitelist(t *testing.T) {
	url := "https://bucket.example.com/uploads/u/a?sig=1"
	cmd := NewRunner("ffmpeg").command(context.Background(), PosterArgs(url, 20, "/tmp/p.jpg")...)
	if !whitelistOf(t, cmd.Args)["https"] {
		t.Errorf("poster extraction reads the same https input: %v", cmd.Args)
	}
}

func TestWithProtocolWhitelist_ProbeArgsAreRestrictedToo(t *testing.T) {
	url := "https://bucket.example.com/uploads/u/a?sig=1"
	args := withProtocolWhitelist(ProbeArgs(url))

	if !whitelistOf(t, args)["https"] || whitelistOf(t, args)["http"] {
		t.Errorf("probe of an https input should allow https only: %v", args)
	}
	if args[len(args)-1] != url {
		t.Errorf("input must remain the final ffprobe argument, got %q", args[len(args)-1])
	}

	// ffprobe's input is positional: a hostile playlist-as-upload must not get
	// http just because the flag was absent.
	local := withProtocolWhitelist(ProbeArgs("/tmp/x.mp4"))
	if set := whitelistOf(t, local); set["https"] || set["http"] {
		t.Errorf("local probe should stay file-only: %v", set)
	}
}

func TestWithProtocolWhitelist_DoesNotOverrideExplicitChoice(t *testing.T) {
	in := []string{"-protocol_whitelist", "file", "-i", "https://x/y"}
	out := withProtocolWhitelist(in)
	if len(out) != len(in) || out[1] != "file" {
		t.Errorf("explicit whitelist must be kept untouched, got %v", out)
	}
}

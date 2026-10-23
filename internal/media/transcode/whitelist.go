package transcode

import "strings"

// protocolWhitelistFor returns the protocols FFmpeg/ffprobe may open while
// reading one input.
//
// The whitelist exists because a hostile upload can pretend to be media but
// actually be a playlist (HLS, concat, ...) that makes FFmpeg fetch URLs of the
// attacker's choosing. So the default is as small as it can be: local files and
// the crypto wrapper. The one thing we do need is the transport for the single
// presigned URL we hand FFmpeg ourselves, and only the transport that URL uses:
//
//   - https://  needs https, tls and tcp
//   - http://   (local MinIO in development) needs http and tcp
//
// Plain http is never allowed for an https input, which keeps cloud metadata
// endpoints (http://169.254.169.254) out of reach in production.
func protocolWhitelistFor(input string) string {
	switch {
	case strings.HasPrefix(input, "https://"):
		return "file,crypto,https,tls,tcp"
	case strings.HasPrefix(input, "http://"):
		return "file,crypto,http,tcp"
	default:
		return "file,crypto"
	}
}

// inputOf finds the media input in an FFmpeg or ffprobe argument list: the
// value after -i for ffmpeg, or the final positional argument for ffprobe.
func inputOf(args []string) string {
	for i, a := range args {
		if a == "-i" && i+1 < len(args) {
			return args[i+1]
		}
	}
	if len(args) > 0 {
		return args[len(args)-1]
	}
	return ""
}

// withProtocolWhitelist prepends -protocol_whitelist, sized for the input, unless
// the caller already chose one.
func withProtocolWhitelist(args []string) []string {
	for _, a := range args {
		if a == "-protocol_whitelist" {
			return args
		}
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "-protocol_whitelist", protocolWhitelistFor(inputOf(args)))
	return append(out, args...)
}

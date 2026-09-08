package transcode

import (
	"context"
	"testing"
)

func TestRunner_CommandIncludesProtocolWhitelist(t *testing.T) {
	runner := NewRunner("ffmpeg")
	ctx := context.Background()

	cmd := runner.command(ctx, "-i", "input.mp4", "output.m3u8")
	args := cmd.Args // Note: args[0] is executable path, followed by arguments

	idx := indexOf(args, "-protocol_whitelist")
	if idx < 0 {
		t.Fatalf("expected -protocol_whitelist in command args: %v", args)
	}
	if idx+1 >= len(args) || args[idx+1] != "file,crypto" {
		t.Fatalf("expected -protocol_whitelist to be 'file,crypto', got %v", args)
	}

	// Verify it does not duplicate if already present
	cmd2 := runner.command(ctx, "-protocol_whitelist", "file,crypto", "-i", "input.mp4")
	count := 0
	for _, a := range cmd2.Args {
		if a == "-protocol_whitelist" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one -protocol_whitelist flag, got %d in %v", count, cmd2.Args)
	}
}

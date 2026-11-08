package builder

import (
	"strings"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// A bare-minimum valid UUID for VLESS URIs.
const testVLESSUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

// TestBuildNodeOutbound_VLESSInvalidPacketEncodingRejected ensures that an
// unsupported packetEncoding value is rejected at build time. Without this the
// sing-box library panics while formatting its own error (stringifying a
// *string), crashing the whole process. See the xtls/packetEncoding crash.
func TestBuildNodeOutbound_VLESSInvalidPacketEncodingRejected(t *testing.T) {
	uri := "vless://" + testVLESSUUID + "@example.com:443?packetEncoding=packet#Bad"
	_, err := buildNodeOutbound("bad-vless", uri, false)
	if err == nil {
		t.Fatalf("expected error for unsupported packetEncoding, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported packetEncoding") {
		t.Fatalf("expected unsupported packetEncoding error, got %v", err)
	}
}

func TestBuildNodeOutbound_VLESSValidPacketEncodingAccepted(t *testing.T) {
	for _, enc := range []string{"xudp", "packetaddr"} {
		uri := "vless://" + testVLESSUUID + "@example.com:443?packetEncoding=" + enc + "#Good"
		outbound, err := buildNodeOutbound("ok-vless", uri, false)
		if err != nil {
			t.Fatalf("packetEncoding %q: unexpected error: %v", enc, err)
		}
		if outbound.Type != C.TypeVLESS {
			t.Fatalf("packetEncoding %q: expected type %q, got %q", enc, C.TypeVLESS, outbound.Type)
		}
		opts, ok := outbound.Options.(*option.VLESSOutboundOptions)
		if !ok {
			t.Fatalf("packetEncoding %q: expected *option.VLESSOutboundOptions, got %T", enc, outbound.Options)
		}
		if opts.PacketEncoding == nil || *opts.PacketEncoding != enc {
			t.Fatalf("packetEncoding %q: not propagated, got %v", enc, opts.PacketEncoding)
		}
	}
}

func TestBuildNodeOutbound_VLESSNoPacketEncoding(t *testing.T) {
	uri := "vless://" + testVLESSUUID + "@example.com:443#Plain"
	outbound, err := buildNodeOutbound("plain-vless", uri, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	opts, ok := outbound.Options.(*option.VLESSOutboundOptions)
	if !ok {
		t.Fatalf("expected *option.VLESSOutboundOptions, got %T", outbound.Options)
	}
	if opts.PacketEncoding != nil {
		t.Fatalf("expected nil PacketEncoding, got %v", *opts.PacketEncoding)
	}
}
package avatarsdkgo

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewOggOpusStreamEncoderRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := NewOggOpusStreamEncoder(44100, 0, nil, false)
	if err == nil || !strings.Contains(err.Error(), "supports sample rates") {
		t.Fatalf("expected unsupported sample rate error, got %v", err)
	}

	_, err = NewOggOpusStreamEncoder(24000, 0, &OggOpusEncoderConfig{
		FrameDurationMS: 15,
		Application:     OggOpusApplicationAudio,
	}, false)
	if err == nil || !strings.Contains(err.Error(), "supports frame durations") {
		t.Fatalf("expected unsupported frame duration error, got %v", err)
	}

	_, err = NewOggOpusStreamEncoder(24000, 0, &OggOpusEncoderConfig{
		FrameDurationMS: 20,
		Application:     OggOpusApplication("speech"),
	}, false)
	if err == nil || !strings.Contains(err.Error(), "application must be one of") {
		t.Fatalf("expected unsupported application error, got %v", err)
	}
}

func TestOggOpusStreamEncoderBuffersUntilFrameReady(t *testing.T) {
	t.Parallel()

	encoder, err := NewOggOpusStreamEncoder(24000, 32000, nil, false)
	if err != nil {
		t.Fatalf("NewOggOpusStreamEncoder returned error: %v", err)
	}

	first, err := encoder.Encode(bytes.Repeat([]byte{0x00, 0x00}, 100), false)
	if err != nil {
		t.Fatalf("Encode returned error for partial frame: %v", err)
	}
	if len(first.Payload) != 0 {
		t.Fatalf("expected no payload for partial frame, got %d bytes", len(first.Payload))
	}

	second, err := encoder.Encode(bytes.Repeat([]byte{0x00, 0x00}, 380), true)
	if err != nil {
		t.Fatalf("Encode returned error for flushed frame: %v", err)
	}
	if !bytes.HasPrefix(second.Payload, []byte("OggS")) {
		t.Fatalf("expected Ogg payload, got %q", second.Payload[:min(4, len(second.Payload))])
	}
	if second.CompletedStream != nil {
		t.Fatal("expected completed stream to be nil when collection is disabled")
	}
}

func TestOggOpusStreamEncoderCollectsCompletedStream(t *testing.T) {
	t.Parallel()

	encoder, err := NewOggOpusStreamEncoder(24000, 32000, &OggOpusEncoderConfig{
		FrameDurationMS: 20,
		Application:     OggOpusApplicationAudio,
	}, true)
	if err != nil {
		t.Fatalf("NewOggOpusStreamEncoder returned error: %v", err)
	}

	chunk, err := encoder.Encode(bytes.Repeat([]byte{0x00, 0x00}, 480), true)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if !bytes.HasPrefix(chunk.Payload, []byte("OggS")) {
		t.Fatalf("expected OggS prefix, got %q", chunk.Payload[:min(4, len(chunk.Payload))])
	}
	if !bytes.Equal(chunk.Payload, chunk.CompletedStream) {
		t.Fatal("expected completed stream to equal payload for a single terminal chunk")
	}
	if !bytes.Contains(chunk.Payload, []byte("OpusHead")) {
		t.Fatal("expected payload to include OpusHead packet")
	}
	if !bytes.Contains(chunk.Payload, []byte("OpusTags")) {
		t.Fatal("expected payload to include OpusTags packet")
	}
}

func TestOggOpusStreamEncoderRejectsOddPCMInput(t *testing.T) {
	t.Parallel()

	encoder, err := NewOggOpusStreamEncoder(24000, 0, nil, false)
	if err != nil {
		t.Fatalf("NewOggOpusStreamEncoder returned error: %v", err)
	}

	_, err = encoder.Encode([]byte{0x01}, false)
	if err == nil || !strings.Contains(err.Error(), "16-bit aligned") {
		t.Fatalf("expected alignment error, got %v", err)
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

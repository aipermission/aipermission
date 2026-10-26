package kafka

import (
	"bytes"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestBoundedZstdDecoderHandlesAdvisoryFrameWithoutPanic(t *testing.T) {
	// Two-block regression from GHSA-fpmh-frq8-7rj8 exercises stale FSE state.
	frame := bytes.Join([][]byte{
		{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x38, 0x74, 0x06, 0x00, 0x42, 0xb8, 0x32, 0x0d, 0x1a, 0x00, 0x00, 0x00, 0x90, 0xaf, 0xfa, 0xff, 0x03, 0x00, 0x80, 0xff, 0x7f},
		bytes.Repeat([]byte{0xa5, 0x94, 0x52, 0x4a, 0x29}, 37),
		{0xa5, 0x94, 0x12, 0x00, 0xad, 0x56, 0x03, 0xee, 0xff, 0xdf, 0xb3, 0x1a, 0x1c, 0x1a, 0x00, 0xe0, 0xff, 0x1f, 0x0f, 0xb1, 0x9c, 0x5a, 0xcf, 0x4d, 0xe4, 0x1d, 0x7b, 0xab, 0x99, 0x5e, 0x01, 0x51, 0xa4, 0x73, 0xd8, 0xb1, 0xf3, 0x39, 0x4b, 0x67, 0xf6, 0xab, 0x1a, 0xab, 0x1a, 0xab, 0x1a},
		bytes.Repeat([]byte{0x8c, 0x31, 0xc6, 0x18, 0x63}, 1365),
		{0x8c, 0x31},
		bytes.Repeat([]byte{0x63, 0x8c, 0x31, 0xc6, 0x18}, 1365),
		{0x63, 0x2c},
		bytes.Repeat([]byte{0x31, 0xc6, 0x18, 0x63, 0x8c}, 1365),
		{0x31, 0x46},
		bytes.Repeat([]byte{0x31, 0xc6, 0x18, 0x63, 0x8c}, 1365),
		{0x31, 0x06, 0x00},
	}, nil)
	decoder := boundedDecompressor{maxBytes: maxDecompressedBatchBytes}
	output, err := decoder.Decompress(frame, kgo.CodecZstd)
	if err != nil && output != nil {
		t.Fatal("failed decode returned a partial payload")
	}
	if int64(len(output)) > decoder.maxBytes {
		t.Fatal("advisory frame exceeded the decompression limit")
	}
}

func TestBoundedZstdDecoderPreservesValidPayloadAndLimit(t *testing.T) {
	writer, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	payload := bytes.Repeat([]byte("bounded Kafka message\n"), 128)
	for frames := 1; frames <= 2; frames++ {
		compressed := bytes.Repeat(writer.EncodeAll(payload[:len(payload)/frames], nil), frames)
		for _, limit := range []int64{int64(len(payload) - 1), int64(len(payload)), int64(len(payload) + 1)} {
			output, err := (boundedDecompressor{maxBytes: limit}).Decompress(compressed, kgo.CodecZstd)
			if limit < int64(len(payload)) {
				if err == nil || output != nil {
					t.Fatal("oversized zstd payload was not rejected without partial output")
				}
				if frames > 1 && !strings.Contains(err.Error(), "Kafka record batch exceeds") {
					t.Fatalf("concatenated frames did not reach the cumulative output limit: %v", err)
				}
				continue
			}
			if err != nil || !bytes.Equal(output, payload) {
				t.Fatalf("valid zstd payload failed at limit %d: %v", limit, err)
			}
		}
	}
}

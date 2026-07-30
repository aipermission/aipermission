package kafka

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/twmb/franz-go/pkg/kgo"
)

const maxDecompressedBatchBytes = 2 << 20

var xerialSnappyPrefix = []byte{130, 83, 78, 65, 80, 80, 89, 0}

type boundedDecompressor struct {
	maxBytes int64
}

func (d boundedDecompressor) Decompress(src []byte, codec kgo.CompressionCodecType) ([]byte, error) {
	if d.maxBytes < 1 {
		return nil, errors.New("invalid Kafka decompression limit")
	}
	switch codec {
	case kgo.CodecNone:
		if int64(len(src)) > d.maxBytes {
			return nil, d.tooLarge()
		}
		return src, nil
	case kgo.CodecGzip:
		reader, err := gzip.NewReader(bytes.NewReader(src))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return d.readBounded(reader)
	case kgo.CodecSnappy:
		return d.decodeSnappy(src)
	case kgo.CodecLz4:
		return d.readBounded(lz4.NewReader(bytes.NewReader(src)))
	case kgo.CodecZstd:
		reader, err := zstd.NewReader(
			bytes.NewReader(src),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(uint64(d.maxBytes)),
		)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return d.readBounded(reader)
	default:
		return nil, fmt.Errorf("unsupported Kafka compression codec %d", codec)
	}
}

func (d boundedDecompressor) readBounded(reader io.Reader) ([]byte, error) {
	var output bytes.Buffer
	if _, err := io.Copy(&output, io.LimitReader(reader, d.maxBytes+1)); err != nil {
		return nil, err
	}
	if int64(output.Len()) > d.maxBytes {
		return nil, d.tooLarge()
	}
	return output.Bytes(), nil
}

func (d boundedDecompressor) decodeSnappy(src []byte) ([]byte, error) {
	if len(src) > 16 && bytes.HasPrefix(src, xerialSnappyPrefix) {
		src = src[16:]
		output := make([]byte, 0)
		for len(src) > 0 {
			if len(src) < 4 {
				return nil, errors.New("malformed xerial snappy framing")
			}
			chunkSize := binary.BigEndian.Uint32(src[:4])
			src = src[4:]
			if uint64(chunkSize) > uint64(len(src)) {
				return nil, errors.New("malformed xerial snappy chunk")
			}
			size := int(chunkSize)
			decodedLength, err := s2.DecodedLen(src[:size])
			if err != nil {
				return nil, err
			}
			if int64(decodedLength) > d.maxBytes-int64(len(output)) {
				return nil, d.tooLarge()
			}
			decoded, err := s2.Decode(nil, src[:size])
			if err != nil {
				return nil, err
			}
			output = append(output, decoded...)
			src = src[size:]
		}
		return output, nil
	}
	decodedLength, err := s2.DecodedLen(src)
	if err != nil {
		return nil, err
	}
	if int64(decodedLength) > d.maxBytes {
		return nil, d.tooLarge()
	}
	return s2.Decode(nil, src)
}

func (d boundedDecompressor) tooLarge() error {
	return fmt.Errorf("Kafka record batch exceeds the %d-byte decompression limit", d.maxBytes)
}

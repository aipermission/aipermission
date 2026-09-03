package redisconnector

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	redisCommandTimeout  = 10 * time.Second
	maxRESPLineBytes     = 64 << 10
	maxRESPBulkBytes     = 1 << 20
	maxRESPResponseBytes = 8 << 20
	maxRESPArrayItems    = 4096
	maxRESPValues        = 8192
	maxRESPNestingDepth  = 16
)

type respKind byte

const (
	respSimpleString respKind = '+'
	respError        respKind = '-'
	respInteger      respKind = ':'
	respBulkString   respKind = '$'
	respArray        respKind = '*'
)

type respValue struct {
	kind   respKind
	text   string
	number int64
	array  []respValue
	null   bool
}

type respReadBudget struct {
	bytes  int
	values int
}

type redisClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

func newRedisClient(conn net.Conn) *redisClient {
	return &redisClient{conn: conn, reader: bufio.NewReader(conn)}
}

func (client *redisClient) Close() error {
	if client == nil || client.conn == nil {
		return nil
	}
	return client.conn.Close()
}

func (client *redisClient) Do(args ...string) (respValue, error) {
	if client == nil || client.conn == nil {
		return respValue{}, fmt.Errorf("redis connection is not open")
	}
	if len(args) == 0 {
		return respValue{}, fmt.Errorf("redis command is required")
	}
	if err := client.conn.SetDeadline(time.Now().Add(redisCommandTimeout)); err != nil {
		return respValue{}, fmt.Errorf("set redis command deadline: %w", err)
	}
	var payload bytes.Buffer
	payload.WriteByte('*')
	payload.WriteString(strconv.Itoa(len(args)))
	payload.WriteString("\r\n")
	for _, arg := range args {
		payload.WriteByte('$')
		payload.WriteString(strconv.Itoa(len(arg)))
		payload.WriteString("\r\n")
		payload.WriteString(arg)
		payload.WriteString("\r\n")
	}
	if _, err := client.conn.Write(payload.Bytes()); err != nil {
		return respValue{}, err
	}
	value, err := readRESPValue(client.reader)
	if err != nil {
		return respValue{}, err
	}
	if value.kind == respError {
		return respValue{}, fmt.Errorf("redis error: %s", value.text)
	}
	return value, nil
}

func readRESPValue(reader *bufio.Reader) (respValue, error) {
	return readRESPValueAtDepth(reader, 0, &respReadBudget{})
}

func readRESPValueAtDepth(reader *bufio.Reader, depth int, budget *respReadBudget) (respValue, error) {
	if depth > maxRESPNestingDepth {
		return respValue{}, fmt.Errorf("redis response nesting exceeds %d levels", maxRESPNestingDepth)
	}
	if err := budget.consumeValue(); err != nil {
		return respValue{}, err
	}
	prefix, err := reader.ReadByte()
	if err != nil {
		return respValue{}, err
	}
	if err := budget.consumeBytes(1); err != nil {
		return respValue{}, err
	}
	switch respKind(prefix) {
	case respSimpleString:
		text, err := readRESPLine(reader, budget)
		return respValue{kind: respSimpleString, text: text}, err
	case respError:
		text, err := readRESPLine(reader, budget)
		return respValue{kind: respError, text: text}, err
	case respInteger:
		text, err := readRESPLine(reader, budget)
		if err != nil {
			return respValue{}, err
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return respValue{}, err
		}
		return respValue{kind: respInteger, number: number}, nil
	case respBulkString:
		text, err := readRESPLine(reader, budget)
		if err != nil {
			return respValue{}, err
		}
		size, err := strconv.Atoi(text)
		if err != nil {
			return respValue{}, err
		}
		if size == -1 {
			return respValue{kind: respBulkString, null: true}, nil
		}
		if size < -1 {
			return respValue{}, fmt.Errorf("invalid redis bulk string size %d", size)
		}
		if size > maxRESPBulkBytes {
			return respValue{}, fmt.Errorf("redis bulk string exceeds %d bytes", maxRESPBulkBytes)
		}
		if err := budget.consumeBytes(size + 2); err != nil {
			return respValue{}, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return respValue{}, err
		}
		if !bytes.HasSuffix(buf, []byte("\r\n")) {
			return respValue{}, fmt.Errorf("invalid redis bulk string terminator")
		}
		return respValue{kind: respBulkString, text: string(buf[:size])}, nil
	case respArray:
		text, err := readRESPLine(reader, budget)
		if err != nil {
			return respValue{}, err
		}
		count, err := strconv.Atoi(text)
		if err != nil {
			return respValue{}, err
		}
		if count == -1 {
			return respValue{kind: respArray, null: true}, nil
		}
		if count < -1 {
			return respValue{}, fmt.Errorf("invalid redis array size %d", count)
		}
		if count > maxRESPArrayItems {
			return respValue{}, fmt.Errorf("redis array exceeds %d items", maxRESPArrayItems)
		}
		items := make([]respValue, 0, count)
		for i := 0; i < count; i++ {
			item, err := readRESPValueAtDepth(reader, depth+1, budget)
			if err != nil {
				return respValue{}, err
			}
			items = append(items, item)
		}
		return respValue{kind: respArray, array: items}, nil
	default:
		return respValue{}, fmt.Errorf("unsupported redis response prefix %q", prefix)
	}
}

func readRESPLine(reader *bufio.Reader, budget *respReadBudget) (string, error) {
	line := make([]byte, 0, 128)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxRESPLineBytes {
			return "", fmt.Errorf("redis response line exceeds %d bytes", maxRESPLineBytes)
		}
		if consumeErr := budget.consumeBytes(len(fragment)); consumeErr != nil {
			return "", consumeErr
		}
		line = append(line, fragment...)
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return "", err
		}
	}
	if !bytes.HasSuffix(line, []byte("\r\n")) {
		return "", fmt.Errorf("invalid redis response line terminator")
	}
	return string(line[:len(line)-2]), nil
}

func (budget *respReadBudget) consumeBytes(count int) error {
	if count < 0 || budget.bytes > maxRESPResponseBytes-count {
		return fmt.Errorf("redis response exceeds %d bytes", maxRESPResponseBytes)
	}
	budget.bytes += count
	return nil
}

func (budget *respReadBudget) consumeValue() error {
	if budget.values >= maxRESPValues {
		return fmt.Errorf("redis response exceeds %d values", maxRESPValues)
	}
	budget.values++
	return nil
}

func respString(value respValue) string {
	switch value.kind {
	case respSimpleString, respBulkString:
		if value.null {
			return ""
		}
		return value.text
	case respInteger:
		return strconv.FormatInt(value.number, 10)
	default:
		return value.text
	}
}

func respStringSlice(value respValue) []string {
	if value.kind != respArray {
		return nil
	}
	out := make([]string, 0, len(value.array))
	for _, item := range value.array {
		out = append(out, respString(item))
	}
	return out
}

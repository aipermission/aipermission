package redisconnector

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const redisCommandTimeout = 10 * time.Second

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
	_ = client.conn.SetDeadline(time.Now().Add(redisCommandTimeout))
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
	prefix, err := reader.ReadByte()
	if err != nil {
		return respValue{}, err
	}
	switch respKind(prefix) {
	case respSimpleString:
		text, err := readRESPLine(reader)
		return respValue{kind: respSimpleString, text: text}, err
	case respError:
		text, err := readRESPLine(reader)
		return respValue{kind: respError, text: text}, err
	case respInteger:
		text, err := readRESPLine(reader)
		if err != nil {
			return respValue{}, err
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return respValue{}, err
		}
		return respValue{kind: respInteger, number: number}, nil
	case respBulkString:
		text, err := readRESPLine(reader)
		if err != nil {
			return respValue{}, err
		}
		size, err := strconv.Atoi(text)
		if err != nil {
			return respValue{}, err
		}
		if size < 0 {
			return respValue{kind: respBulkString, null: true}, nil
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
		text, err := readRESPLine(reader)
		if err != nil {
			return respValue{}, err
		}
		count, err := strconv.Atoi(text)
		if err != nil {
			return respValue{}, err
		}
		if count < 0 {
			return respValue{kind: respArray, null: true}, nil
		}
		items := make([]respValue, 0, count)
		for i := 0; i < count; i++ {
			item, err := readRESPValue(reader)
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

func readRESPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
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

func respStringMap(value respValue) map[string]string {
	if value.kind != respArray {
		return nil
	}
	out := map[string]string{}
	for index := 0; index+1 < len(value.array); index += 2 {
		out[respString(value.array[index])] = respString(value.array[index+1])
	}
	return out
}

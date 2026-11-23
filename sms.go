package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Message struct {
	ID      int64     `json:"id"`
	Address string    `json:"address"`
	Date    time.Time `json:"date"`
	Type    int       `json:"type"` // 1 received, 2 sent
	Read    bool      `json:"read"`
	Body    string    `json:"body"`
}

// Printable strips control characters, so a crafted SMS can't send escape
// sequences to the terminal.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

func (m Message) Dir() string {
	switch m.Type {
	case 1:
		return "<-"
	case 2:
		return "->"
	}
	return "?"
}

// Device talks to a phone through the adb binary. An empty Serial lets adb
// pick the device (or use $ANDROID_SERIAL).
type Device struct{ Serial string }

type Query struct {
	Box     string // "", "all", "inbox", "sent", "draft", "outbox"
	Limit   int    // 0 means no limit
	AfterID int64  // only messages with a greater _id
	Unread  bool   // only messages with read 0 or NULL, matching Message.Read
}

func (q Query) where() string {
	var conds []string
	if q.AfterID > 0 {
		conds = append(conds, fmt.Sprintf("_id>%d", q.AfterID))
	}
	if q.Unread {
		conds = append(conds, "ifnull(read,0)=0")
	}
	return strings.Join(conds, " AND ")
}

// Text columns are hex encoded by SQLite on the phone. content query does
// not escape values, so raw bodies could contain anything, including fake rows.
var hexColumns = []string{"_id", "hex(address)", "date", "type", "read", "hex(body)"}

// plainColumns is the fallback for providers that only accept column names.
// The order must match plainRowRe.
var plainColumns = []string{"_id", "address", "date", "type", "read", "body"}

var boxes = map[string]string{
	"":       "content://sms",
	"all":    "content://sms",
	"inbox":  "content://sms/inbox",
	"sent":   "content://sms/sent",
	"draft":  "content://sms/draft",
	"outbox": "content://sms/outbox",
}

// Mark is "*" for unread messages, so they stand out in plain text lists.
func (m Message) Mark() string {
	if m.Read {
		return " "
	}
	return "*"
}

func (d Device) Messages(ctx context.Context, q Query) ([]Message, error) {
	uri, ok := boxes[q.Box]
	if !ok {
		return nil, fmt.Errorf("unknown box %q", q.Box)
	}
	// The command string is parsed again by the device shell, so it only
	// contains fixed text and integers.
	sort := "_id DESC"
	if q.Limit > 0 {
		sort += fmt.Sprintf(" LIMIT %d", q.Limit)
	}
	where := q.where()

	out, err := d.query(ctx, uri, hexColumns, where, sort)
	if err == nil {
		return parseHexRows(out)
	}
	msgs, plainErr := d.plainMessages(ctx, uri, where, sort)
	if plainErr != nil {
		return nil, fmt.Errorf("%w (plain column fallback: %v)", err, plainErr)
	}
	return msgs, nil
}

// query runs content query and returns its output, which is either rows or
// "No result found.".
func (d Device) query(ctx context.Context, uri string, columns []string, where, sort string) (string, error) {
	cmd := fmt.Sprintf("content query --uri %s --projection '%s' --sort '%s'", uri, strings.Join(columns, ":"), sort)
	if where != "" {
		cmd += fmt.Sprintf(" --where '%s'", where)
	}
	return d.shell(ctx, cmd, func(out string) error {
		if !strings.HasPrefix(out, "Row: ") && strings.TrimSpace(out) != "No result found." {
			return fmt.Errorf("unexpected content query output: %.200q", out)
		}
		return nil
	})
}

// MarkRead sets read=1 on one message. Android drops SMS writes from apps
// other than the default SMS app without an error, so the result is checked.
func (d Device) MarkRead(ctx context.Context, id int64) error {
	cmd := fmt.Sprintf("content update --uri content://sms --bind read:i:1 --where '_id=%d'", id)
	if _, err := d.shell(ctx, cmd, nil); err != nil {
		return err
	}
	out, err := d.query(ctx, "content://sms", []string{"_id"}, fmt.Sprintf("_id=%d AND read=1", id), "_id")
	if err != nil {
		return err
	}
	ids, err := parseIDs(out)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		adb := "adb"
		if d.Serial != "" {
			adb += " -s " + d.Serial
		}
		return fmt.Errorf("phone ignored the write, allow it with: %s shell appops set com.android.shell WRITE_SMS allow", adb)
	}
	return nil
}

// shell runs cmd on the device. check, when set, validates the output.
// Without check, any stderr output counts as a failure.
func (d Device) shell(ctx context.Context, cmd string, check func(out string) error) (string, error) {
	var args []string
	if d.Serial != "" {
		args = append(args, "-s", d.Serial)
	}
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "adb", append(args, "shell", cmd)...)
	c.Stderr = &stderr
	b, err := c.Output()
	out := string(b)
	switch {
	case err != nil:
	case check != nil:
		err = check(out)
	case stderr.Len() > 0:
		err = errors.New("unexpected stderr output")
	}
	if err == nil {
		return out, nil
	}
	// content exits 0 on provider errors and explains them on stderr
	if s := strings.TrimSpace(stderr.String()); s != "" {
		return "", fmt.Errorf("adb: %w: %s", err, s)
	}
	return "", fmt.Errorf("adb: %w", err)
}

func parseHexRows(out string) ([]Message, error) {
	out = strings.TrimSpace(out)
	if out == "No result found." {
		return nil, nil
	}
	var msgs []Message
	for _, line := range strings.Split(out, "\n") {
		m, err := parseHexRow(line)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func parseHexRow(line string) (Message, error) {
	bad := fmt.Errorf("unexpected content query output: %.200q", line)
	if !strings.HasPrefix(line, "Row: ") {
		return Message{}, bad
	}
	parts := strings.Split(line, ", ")
	if len(parts) != len(hexColumns) {
		return Message{}, bad
	}
	parts[0] = parts[0][strings.LastIndex(parts[0], " ")+1:] // drop "Row: N "
	vals := make([]string, len(hexColumns))
	for i, p := range parts {
		v, ok := strings.CutPrefix(p, hexColumns[i]+"=")
		if !ok {
			return Message{}, bad
		}
		vals[i] = v
	}
	for _, i := range []int{1, 5} {
		if vals[i] == "NULL" {
			vals[i] = ""
			continue
		}
		b, err := hex.DecodeString(vals[i])
		if err != nil {
			return Message{}, fmt.Errorf("bad %s: %w", hexColumns[i], err)
		}
		vals[i] = string(b)
	}
	return toMessage(vals)
}

// plainMessages reads unencoded columns. The _id list comes first because
// that output can't be ambiguous. The full rows are then split where the
// next expected "Row: N _id=X, " starts.
func (d Device) plainMessages(ctx context.Context, uri, where, sort string) ([]Message, error) {
	out, err := d.query(ctx, uri, []string{"_id"}, where, sort)
	if err != nil {
		return nil, err
	}
	ids, err := parseIDs(out)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	// pin the set of rows, so messages arriving in between don't shift it
	bounded := fmt.Sprintf("_id<=%d", ids[0])
	if where != "" {
		bounded = where + " AND " + bounded
	}
	out, err = d.query(ctx, uri, plainColumns, bounded, sort)
	if err != nil {
		return nil, err
	}
	return parsePlainRows(out, ids, func(id int64) (string, error) {
		return d.query(ctx, uri, plainColumns, fmt.Sprintf("_id=%d", id), "_id")
	})
}

func parseIDs(out string) ([]int64, error) {
	if strings.TrimSpace(out) == "No result found." {
		return nil, nil
	}
	var ids []int64
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var n int
		var id int64
		if _, err := fmt.Sscanf(line, "Row: %d _id=%d", &n, &id); err != nil || n != len(ids) {
			return nil, fmt.Errorf("unexpected content query output: %.200q", line)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// parsePlainRows splits rows using the known ids. When a body contains the
// next row's start marker, the row is ambiguous and is fetched alone with
// fetchOne, whose single-row output ends where the body ends.
func parsePlainRows(out string, ids []int64, fetchOne func(id int64) (string, error)) ([]Message, error) {
	msgs := make([]Message, 0, len(ids))
	rest := out
	for i, id := range ids {
		prefix := fmt.Sprintf("Row: %d ", i)
		if !strings.HasPrefix(rest, fmt.Sprintf("%s_id=%d, ", prefix, id)) {
			return nil, fmt.Errorf("row %d is not _id %d, messages changed during the query", i, id)
		}
		row := rest
		if i+1 < len(ids) {
			marker := fmt.Sprintf("\nRow: %d _id=%d, ", i+1, ids[i+1])
			switch strings.Count(rest, marker) {
			case 0:
				return nil, fmt.Errorf("row %d (_id %d) not found in content query output", i+1, ids[i+1])
			case 1:
				row = rest[:strings.Index(rest, marker)+1]
			default:
				one, err := fetchOne(id)
				if err != nil {
					return nil, err
				}
				row = prefix + strings.TrimPrefix(one, "Row: 0 ")
				if !strings.HasPrefix(rest, row) {
					return nil, fmt.Errorf("_id %d changed during the query", id)
				}
			}
		}
		rest = rest[len(row):]
		m, err := parsePlainRow(strings.TrimSuffix(strings.TrimPrefix(row, prefix), "\n"))
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// The numeric fields pin the match, so ", date=" inside an address only
// matches when valid numeric fields follow it.
var plainRowRe = regexp.MustCompile(`(?s)^_id=(\d+), address=(.*?), date=(\d+|NULL), type=(\d+|NULL), read=(\d+|NULL), body=(.*)$`)

// parsePlainRow keeps the text "NULL" in address and body, since plain output
// can't tell it from SQL NULL.
func parsePlainRow(s string) (Message, error) {
	m := plainRowRe.FindStringSubmatch(s)
	if m == nil {
		return Message{}, fmt.Errorf("unexpected content query row: %.100q", s)
	}
	return toMessage(m[1:])
}

// toMessage converts decoded values in plainColumns order.
func toMessage(vals []string) (Message, error) {
	id, err := strconv.ParseInt(vals[0], 10, 64)
	if err != nil {
		return Message{}, fmt.Errorf("bad _id: %w", err)
	}
	ms, _ := strconv.ParseInt(vals[2], 10, 64)
	typ, _ := strconv.Atoi(vals[3])
	return Message{ID: id, Address: vals[1], Date: time.UnixMilli(ms), Type: typ, Read: vals[4] == "1", Body: vals[5]}, nil
}

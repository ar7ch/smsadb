package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
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
}

// Text columns are hex encoded by SQLite on the phone. content query does
// not escape values, so raw bodies could contain anything, including fake rows.
var columns = []string{"_id", "hex(address)", "date", "type", "read", "hex(body)"}

var boxes = map[string]string{
	"":       "content://sms",
	"all":    "content://sms",
	"inbox":  "content://sms/inbox",
	"sent":   "content://sms/sent",
	"draft":  "content://sms/draft",
	"outbox": "content://sms/outbox",
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
	cmd := fmt.Sprintf("content query --uri %s --projection '%s' --sort '%s'", uri, strings.Join(columns, ":"), sort)
	if q.AfterID > 0 {
		cmd += fmt.Sprintf(" --where '_id>%d'", q.AfterID)
	}

	var args []string
	if d.Serial != "" {
		args = append(args, "-s", d.Serial)
	}
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "adb", append(args, "shell", cmd)...)
	c.Stderr = &stderr
	out, err := c.Output()
	if err == nil {
		var msgs []Message
		if msgs, err = parseRows(string(out)); err == nil {
			return msgs, nil
		}
	}
	// content query exits 0 on provider errors and explains them on stderr
	if s := strings.TrimSpace(stderr.String()); s != "" {
		return nil, fmt.Errorf("adb: %w: %s", err, s)
	}
	return nil, fmt.Errorf("adb: %w", err)
}

func parseRows(out string) ([]Message, error) {
	out = strings.TrimSpace(out)
	if out == "No result found." {
		return nil, nil
	}
	var msgs []Message
	for _, line := range strings.Split(out, "\n") {
		m, err := parseRow(line)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func parseRow(line string) (Message, error) {
	bad := fmt.Errorf("unexpected content query output: %.200q", line)
	if !strings.HasPrefix(line, "Row: ") {
		return Message{}, bad
	}
	parts := strings.Split(line, ", ")
	if len(parts) != len(columns) {
		return Message{}, bad
	}
	parts[0] = parts[0][strings.LastIndex(parts[0], " ")+1:] // drop "Row: N "
	vals := make([]string, len(columns))
	for i, p := range parts {
		v, ok := strings.CutPrefix(p, columns[i]+"=")
		if !ok {
			return Message{}, bad
		}
		if v != "NULL" {
			vals[i] = v
		}
	}

	id, err := strconv.ParseInt(vals[0], 10, 64)
	if err != nil {
		return Message{}, fmt.Errorf("bad _id: %w", err)
	}
	addr, err := hex.DecodeString(vals[1])
	if err != nil {
		return Message{}, fmt.Errorf("bad address: %w", err)
	}
	body, err := hex.DecodeString(vals[5])
	if err != nil {
		return Message{}, fmt.Errorf("bad body: %w", err)
	}
	ms, _ := strconv.ParseInt(vals[2], 10, 64)
	typ, _ := strconv.Atoi(vals[3])
	return Message{ID: id, Address: string(addr), Date: time.UnixMilli(ms), Type: typ, Read: vals[4] == "1", Body: string(body)}, nil
}

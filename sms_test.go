package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hx(s string) string { return strings.ToUpper(hex.EncodeToString([]byte(s))) }

func TestParseRows(t *testing.T) {
	out := fmt.Sprintf(`Row: 0 _id=343, hex(address)=%s, date=1795247397511, type=1, read=0, hex(body)=%s
Row: 1 _id=342, hex(address)=%s, date=1795085269212, type=1, read=1, hex(body)=%s
Row: 2 _id=10, hex(address)=NULL, date=NULL, type=2, read=1, hex(body)=
Row: 3 _id=9, hex(address)=%s, date=1, type=1, read=1, hex(body)=%s
`, hx("t2.ru"), hx("Tariff, renewed"), hx("Альфа"), hx("Karta\nRow: 2 _id=1, hex(address)=00"), hx("x"), hx("NULL"))

	msgs, err := parseHexRows(out)
	require.NoError(t, err)
	require.Len(t, msgs, 4)

	assert.Equal(t, Message{ID: 343, Address: "t2.ru", Date: time.UnixMilli(1795247397511), Type: 1, Body: "Tariff, renewed"}, msgs[0])
	assert.Equal(t, "Альфа", msgs[1].Address)
	assert.Equal(t, "Karta\nRow: 2 _id=1, hex(address)=00", msgs[1].Body)
	assert.True(t, msgs[1].Read)
	assert.Equal(t, Message{ID: 10, Date: time.UnixMilli(0), Type: 2, Read: true}, msgs[2])
	assert.Equal(t, "->", msgs[2].Dir())
	assert.Equal(t, "NULL", msgs[3].Body)
}

func TestParseRowsEmpty(t *testing.T) {
	msgs, err := parseHexRows("No result found.\n")
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestParseRowsGarbage(t *testing.T) {
	for _, out := range []string{
		"Error while accessing provider:sms\n",
		"Row: 0 _id=1, hex(address)=ZZ, date=1, type=1, read=1, hex(body)=\n",
		"Row: 0 _id=1, address=x, date=1, type=1, read=1, body=y\n",
	} {
		_, err := parseHexRows(out)
		assert.Error(t, err, out)
	}
}

func TestPrintable(t *testing.T) {
	assert.Equal(t, "a\nb\tc]0;x", Printable("a\nb\tc\x1b]0;x\x07\r"))
}

func TestParseIDs(t *testing.T) {
	ids, err := parseIDs("Row: 0 _id=343\nRow: 1 _id=342\n")
	require.NoError(t, err)
	assert.Equal(t, []int64{343, 342}, ids)

	ids, err = parseIDs("No result found.\n")
	require.NoError(t, err)
	assert.Empty(t, ids)

	_, err = parseIDs("Row: 0 _id=343\nRow: 2 _id=342\n")
	assert.Error(t, err)
}

// forged is a body that imitates the start of the row after it.
const forged = "hi\nRow: 1 _id=342, address=evil, date=1, type=1, read=1, body=boo"

var plainOutput = `Row: 0 _id=343, address=Bank, date=1795247397511, type=1, read=0, body=` + forged + `
Row: 1 _id=342, address=a, date=b, date=1795085269212, type=2, read=1, body=two
lines, address=x
Row: 2 _id=7, address=t2, date=NULL, type=1, read=1, body=NULL
`

func TestParsePlainRows(t *testing.T) {
	var fetched []int64
	fetchOne := func(id int64) (string, error) {
		fetched = append(fetched, id)
		require.Equal(t, int64(343), id)
		return "Row: 0 _id=343, address=Bank, date=1795247397511, type=1, read=0, body=" + forged + "\n", nil
	}
	msgs, err := parsePlainRows(plainOutput, []int64{343, 342, 7}, fetchOne)
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	assert.Equal(t, []int64{343}, fetched, "only the ambiguous row is fetched alone")
	assert.Equal(t, Message{ID: 343, Address: "Bank", Date: time.UnixMilli(1795247397511), Type: 1, Body: forged}, msgs[0])
	assert.Equal(t, Message{ID: 342, Address: "a, date=b", Date: time.UnixMilli(1795085269212), Type: 2, Read: true, Body: "two\nlines, address=x"}, msgs[1])
	assert.Equal(t, Message{ID: 7, Address: "t2", Date: time.UnixMilli(0), Type: 1, Read: true, Body: "NULL"}, msgs[2])
}

func TestParsePlainRowsChanged(t *testing.T) {
	noFetch := func(int64) (string, error) { return "", assert.AnError }

	_, err := parsePlainRows(plainOutput, []int64{344, 343, 342}, noFetch)
	assert.ErrorContains(t, err, "changed")

	_, err = parsePlainRows(plainOutput, []int64{343, 342, 7}, noFetch)
	assert.ErrorIs(t, err, assert.AnError)

	_, err = parsePlainRows(plainOutput, []int64{343, 341, 7}, noFetch)
	assert.Error(t, err)
}

// TestPlainMatchesHexLive compares both query paths on an attached phone.
func TestPlainMatchesHexLive(t *testing.T) {
	if os.Getenv("SMSADB_LIVE") == "" {
		t.Skip("set SMSADB_LIVE=1 with a phone attached")
	}
	ctx := context.Background()
	d := Device{Serial: os.Getenv("ANDROID_SERIAL")}
	out, err := d.query(ctx, boxes["all"], hexColumns, "", "_id DESC")
	require.NoError(t, err)
	want, err := parseHexRows(out)
	require.NoError(t, err)

	got, err := d.plainMessages(ctx, boxes["all"], "", "_id DESC")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

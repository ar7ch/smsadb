package main

import (
	"encoding/hex"
	"fmt"
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
Row: 2 _id=10, hex(address)=, date=NULL, type=2, read=1, hex(body)=
`, hx("t2.ru"), hx("Tariff, renewed"), hx("Альфа"), hx("Karta\nRow: 2 _id=1, hex(address)=00"))

	msgs, err := parseRows(out)
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	assert.Equal(t, Message{ID: 343, Address: "t2.ru", Date: time.UnixMilli(1795247397511), Type: 1, Body: "Tariff, renewed"}, msgs[0])
	assert.Equal(t, "Альфа", msgs[1].Address)
	assert.Equal(t, "Karta\nRow: 2 _id=1, hex(address)=00", msgs[1].Body)
	assert.True(t, msgs[1].Read)
	assert.Equal(t, Message{ID: 10, Date: time.UnixMilli(0), Type: 2, Read: true}, msgs[2])
	assert.Equal(t, "->", msgs[2].Dir())
}

func TestParseRowsEmpty(t *testing.T) {
	msgs, err := parseRows("No result found.\n")
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestParseRowsGarbage(t *testing.T) {
	for _, out := range []string{
		"Error while accessing provider:sms\n",
		"Row: 0 _id=1, hex(address)=ZZ, date=1, type=1, read=1, hex(body)=\n",
		"Row: 0 _id=1, address=x, date=1, type=1, read=1, body=y\n",
	} {
		_, err := parseRows(out)
		assert.Error(t, err, out)
	}
}

func TestPrintable(t *testing.T) {
	assert.Equal(t, "a\nb\tc]0;x", Printable("a\nb\tc\x1b]0;x\x07\r"))
}

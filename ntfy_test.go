package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushErrorHidesTopic(t *testing.T) {
	err := push(context.Background(), "http://127.0.0.1:1/secret-topic", "", Message{Body: "x"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-topic")
}

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDevices(t *testing.T) {
	out := `* daemon started successfully
List of devices attached
a1b2c3d4e5f6           device usb:1-1 product:lava model:Redmi_9 device:lava transport_id:26
emulator-5554          unauthorized transport_id:2

`
	assert.Equal(t, []DeviceInfo{
		{Serial: "a1b2c3d4e5f6", State: "device", Name: "Redmi 9"},
		{Serial: "emulator-5554", State: "unauthorized", Name: "emulator-5554"},
	}, parseDevices(out))
}

func TestDeviceName(t *testing.T) {
	for props, want := range map[string]string{
		"\nxiaomi\nRedmi 9\n":                 "Xiaomi Redmi 9",
		"Redmi Note 10 Pro\nXiaomi\nM2101K6G": "Redmi Note 10 Pro",
		"\nGoogle\nPixel 7\n":                 "Google Pixel 7",
		"\nsamsung\nSamsung SM-G991B\n":       "Samsung SM-G991B",
		"\n\nPixel\n":                         "Pixel",
		"":                                    "",
	} {
		assert.Equal(t, want, deviceName(props), "props %q", props)
	}
}

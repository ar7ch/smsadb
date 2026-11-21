package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"
)

type DeviceInfo struct {
	Serial string `json:"serial"`
	State  string `json:"state"` // device, unauthorized, offline, ...
	Name   string `json:"name"`
}

func (d DeviceInfo) String() string {
	if d.State != "device" {
		return fmt.Sprintf("%s (%s, %s)", d.Name, d.Serial, d.State)
	}
	return fmt.Sprintf("%s (%s)", d.Name, d.Serial)
}

// Devices lists attached phones. Names come from getprop when the phone
// allows shell access, else from the model field of "adb devices -l".
func Devices(ctx context.Context) ([]DeviceInfo, error) {
	out, err := exec.CommandContext(ctx, "adb", "devices", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}
	devs := parseDevices(string(out))
	for i, d := range devs {
		if d.State != "device" {
			continue
		}
		props, err := exec.CommandContext(ctx, "adb", "-s", d.Serial, "shell",
			"getprop ro.product.marketname; getprop ro.product.manufacturer; getprop ro.product.model").Output()
		if err == nil {
			if name := Printable(deviceName(string(props))); name != "" {
				devs[i].Name = name
			}
		}
	}
	return devs, nil
}

func parseDevices(out string) []DeviceInfo {
	var devs []DeviceInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		d := DeviceInfo{Serial: Printable(f[0]), State: Printable(f[1]), Name: Printable(f[0])}
		for _, kv := range f[2:] {
			if model, ok := strings.CutPrefix(kv, "model:"); ok {
				d.Name = Printable(strings.ReplaceAll(model, "_", " "))
			}
		}
		devs = append(devs, d)
	}
	return devs
}

// deviceName takes getprop output for marketname, manufacturer and model,
// one per line. Marketname ("Redmi Note 10 Pro") beats model codes, but only
// some vendors set it.
func deviceName(props string) string {
	lines := strings.Split(props+"\n\n", "\n")
	market, maker, model := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), strings.TrimSpace(lines[2])
	if market != "" {
		return market
	}
	if r, n := utf8.DecodeRuneInString(maker); n > 0 && !strings.HasPrefix(strings.ToLower(model), strings.ToLower(maker)) {
		return strings.TrimSpace(string(unicode.ToUpper(r)) + maker[n:] + " " + model)
	}
	return model
}

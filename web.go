package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"html/template"
	"log"
	"net/http"
	"strings"
)

//go:embed web.html
var webHTML string

var webTmpl = template.Must(template.New("web").Parse(webHTML))

func runWeb(ctx context.Context, dev Device, args []string) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	// SMS carry 2FA codes, so loopback unless asked otherwise
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	fs.Parse(args)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		box, q, serial := r.FormValue("box"), r.FormValue("q"), r.FormValue("dev")
		unread := r.FormValue("unread") != ""
		if box == "" {
			box = "all"
		}
		if _, ok := boxes[box]; !ok {
			http.Error(w, "unknown box", http.StatusBadRequest)
			return
		}
		devs, err := Devices(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		// pick the requested phone, else -s, else the first usable one
		if serial == "" {
			serial = dev.Serial
		}
		known := false
		for _, d := range devs {
			known = known || (d.Serial == serial && d.State == "device")
		}
		if !known {
			serial = ""
			for _, d := range devs {
				if d.State == "device" {
					serial = d.Serial
					break
				}
			}
		}
		limit := 200
		if q != "" {
			limit = 0
		}
		var msgs []Message
		if serial != "" {
			msgs, err = Device{Serial: serial}.Messages(r.Context(), Query{Box: box, Limit: limit, Unread: unread})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if q != "" {
			lq := strings.ToLower(q)
			kept := msgs[:0]
			for _, m := range msgs {
				if strings.Contains(strings.ToLower(m.Address+"\n"+m.Body), lq) {
					kept = append(kept, m)
				}
			}
			msgs = kept
		}
		data := struct {
			Box, Q, Serial string
			Unread         bool
			Boxes          []string
			Devs           []DeviceInfo
			Msgs           []Message
		}{box, q, serial, unread, []string{"all", "inbox", "sent", "draft", "outbox"}, devs, msgs}
		if err := webTmpl.Execute(w, data); err != nil {
			log.Print(err)
		}
	})

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	log.Printf("listening on http://%s", *addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

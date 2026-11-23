# smsadb

Read SMS from an Android phone attached over adb. It queries the phone's SMS
provider with `adb shell content query`, so nothing has to be installed on
the phone.

## Requirements

- `adb` on `PATH` (the nix package brings its own if none is found)
- USB debugging enabled on the phone, and the debugging prompt accepted

## Install

```sh
nix run github:ar7ch/smsadb -- tui
# or
go install github.com/ar7ch/smsadb@latest
```

## Usage

```
smsadb [-s serial] <command> [flags]
```

`-s` picks the phone when several are attached. It defaults to
`$ANDROID_SERIAL`, else the only attached phone.

| Command   | What it does |
|-----------|--------------|
| `devices` | list attached phones |
| `list`    | print messages. `-n 20` limits the count (0 for all), `-unread` keeps unread ones, `-json` prints JSON, `-box` picks all, inbox, sent, draft or outbox |
| `tui`     | browse messages in the terminal |
| `web`     | serve a page with messages and search on `-addr` (default `127.0.0.1:8080`). It shows the `-s` phone, else the first ready one, and the page has a phone picker |
| `ntfy`    | push new incoming messages to an [ntfy](https://ntfy.sh) topic |

### tui keys

`enter` opens a message, `r` reloads, `R` marks the selected or open message
read, `u` toggles unread only, `d` picks another phone, `/` filters. Messages also reload every
15 seconds.

### ntfy

```sh
NTFY_TOKEN=tk_... smsadb ntfy -push-url https://ntfy.sh/your-secret-topic
```

`-push-url` is required, `NTFY_TOKEN` is optional. The first run starts after the
newest inbox message, so old messages are not pushed. The id of the last
handled message is kept per phone in the user cache dir (`-state` overrides
the file), and `-interval` sets the poll interval (30s by default). A message
the server refuses with a 4xx other than 401, 403 or 429 is logged and
skipped, so it does not block later ones.

## Security

SMS often carry login codes. Keep that in mind before you expose them:

- `web` has no authentication. It listens on loopback by default. Only pass
  another `-addr` on a network you trust.
- On public ntfy servers anyone who knows the topic name can read it. Use a
  long random topic, or your own server with a token.
- Marking messages read (`R` in the tui) needs SMS write access for adb
  shell, which Android blocks by default:

  ```sh
  adb shell appops set com.android.shell WRITE_SMS allow
  ```

  After that, anything running as adb shell can change or delete your SMS.
  Undo it with `adb shell appops set com.android.shell WRITE_SMS default`.

## Development

```sh
nix develop
make build test lint
```

## License

MIT

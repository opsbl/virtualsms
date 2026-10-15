# VirtualSMS

VirtualSMS is a Linux service that exposes virtual GSM modems as serial devices such as `/dev/ttySMS0` and `/dev/ttySMS1`. Each device maps to a configured SMS provider. It supports the AT text-mode submission sequence used by Zabbix's built-in SMS media type.

The service is written in Go and uses Linux PTYs. Go provider plugins are loaded from `.so` files, so the host and plugins use cgo and must be built together with the same Go toolchain and C compiler. Root is required by the included systemd unit to create links in `/dev` and assign the PTYs to the configured group.

## Build and install

```sh
make                            # host C compiler; builds the host and all plugins
sudo install -m 0755 build/native/virtualsms /usr/bin/virtualsms
sudo install -d -m 0755 /usr/lib/virtualsms/plugins
sudo install -m 0644 build/native/plugins/*.so /usr/lib/virtualsms/plugins/
sudo install -d -m 0750 /etc/virtualsms
sudo install -m 0600 config.example.toml /etc/virtualsms/config.toml
sudo install -m 0644 virtualsms.service /usr/lib/systemd/system/virtualsms.service
sudo systemctl daemon-reload
sudo systemctl enable --now virtualsms
```

Put each plugin in `plugins/<name>/main.go`. `make` discovers and builds all of them into `build/native/plugins/<name>.so`. Use `make plugin PLUGIN=ablemobile` to build only one plugin, or `make build PLUGIN=ablemobile` to build it with the host. `make musl` builds the host and all plugins with `musl-gcc` in `build/musl/`; `make musl PLUGIN=ablemobile` selects one plugin. The musl build still needs the musl dynamic loader and `libc.so` at runtime because `plugin.Open` cannot load plugins from a fully static host. Build packages with `make deb` or `make rpm`; use `make deb-musl` or `make rpm-musl` for musl variants. Packages are written to `build/dist/` and include the binary, all discovered plugins, configuration and systemd unit. Package builds require `dpkg-deb` or `rpmbuild`, respectively. Set `VERSION=...` to change the package version.

Edit `/etc/virtualsms/config.toml` before starting the service: choose the device paths, set a group that the Zabbix server belongs to, and enter the AbleMobile credentials. `dryrun` only logs submitted messages. Keep the configuration readable only by administrators because it contains a password. Set the Zabbix SMS media type's **GSM modem** field to `/dev/ttySMS0` or `/dev/ttySMS1`.

```sh
virtualsms -c /etc/virtualsms/config.toml check
virtualsms test -d /dev/ttySMS1 -t +15551234567 -m 'test message'
virtualsms -c /etc/virtualsms/config.toml test -D -d /dev/ttySMS1 -t +15551234567 -m 'test message'
curl http://127.0.0.1:9099/metrics
journalctl -u virtualsms -f
```

`test` uses the running virtual serial device and needs no configuration file. Use `-D` with `-c` to call the configured provider without a running service. The test timeout defaults to 45 seconds and can be changed with `-T`. Its flags also accept the long forms `--device`, `--to`, `--message`, `--direct`, and `--timeout`. In the default `queued` mode, serial `OK` means the message entered the batch queue; use the delivery logs and metrics to check the later provider result. In `sent` mode, a provider failure returns `+CMS ERROR: 500` to the AT client.

## Batching and logs

By default, the first message starts a 3-second window. Requests for the same provider with exactly the same message bytes during that window form one batch, including requests arriving on the same or different virtual devices. Set `[batch] wait = "3s"` to change the window. The default `ack_mode = "queued"` acknowledges each AT request as soon as it enters the queue, so consecutive submissions on one serial device can be batched. Set `ack_mode = "sent"` to wait for the provider result and return each recipient's result through AT; consecutive submissions on one device generally cannot share a window in that mode. Providers receive a list of recipients and decide how to submit it. AbleMobile submits one HTTP request per batch with all recipients joined by `;` in `Telephone`; the generic HTTP provider still submits one request per recipient. AbleMobile's single HTTP response applies to every recipient in that batch. On normal shutdown, the service submits pending batches early and waits up to 35 seconds for provider calls to finish.

The server emits structured JSON logs for startup, device readiness, queueing, batch submission, individual AT acknowledgement, failures, and shutdown. `[logging] level` accepts `debug`, `info`, `warn`, or `error`. Operational logs include a short content hash instead of the SMS text; the `log` test provider intentionally writes message text to the journal.

## Providers

`log` accepts messages and writes their content to the service journal for local testing. `http` sends an HTTP POST with a JSON object containing `to` and `text`, and optionally adds `Authorization: Bearer <token>`. AbleMobile is supplied as a Go plugin at `/usr/lib/virtualsms/plugins/ablemobile.so`. It sends `application/x-www-form-urlencoded` to AbleMobile's `SendSMS` endpoint with the same seven form fields as the supplied Python script, joining a batch's numbers into `Telephone=111;222;333` before form encoding. Its `url` option is optional and defaults to `https://api3.ablemobile.com/service/smsapi.asmx/SendSMS`. `user_define_no` defaults to `00000`; `hex` and `sender` default to empty strings. The Python script's `subject` was used only for its log entry, so the modem sends the SMS body as `Message`. HTTP 200 means the API accepted the request. AbleMobile parses and validates its own options and logs batch outcomes without message content or credentials.

To add a plugin, keep its implementation in `plugins/<name>/`, implement `pkg/pluginapi.Provider`, export `var APIVersion = pluginapi.Version` and `func New(pluginapi.Config) (pluginapi.Provider, error)` from its Go `package main`, then run `make` or `make plugin PLUGIN=<name>`. The host discovers `<name>.so` in `[plugins] dir`, passes provider options as TOML bytes and supplies a structured logger. The plugin decodes and validates its own options. Build the host and plugin from the same Go toolchain, source versions and build settings. Set `plugin = "NAME"` in a provider and put its settings under `[providers.NAME.options]`. `virtualsms check -c ...` loads configured plugins and validates their options.

The serial emulator supports the Zabbix commands `AT+CMEE=2`, `ATE0`, `AT`, `AT+CMGF=1`, and `AT+CMGS`. It also answers basic `AT+CPIN?`, `AT+CSQ`, and `AT+CREG?` probes. SMS body bytes are passed unchanged to providers; character set conversion, PDU mode, incoming SMS, and multipart SMS are not implemented.

## Metrics

The default metrics listener is `127.0.0.1:9099`. It exposes `/metrics` and `/healthz`. Metrics include `virtualsms_messages_total{device,provider,outcome}` (AT acknowledgements), `virtualsms_delivery_results_total{provider,outcome}` (actual provider results), `virtualsms_batches_total{provider,outcome}`, `virtualsms_batch_size`, `virtualsms_batch_provider_duration_seconds`, and `virtualsms_device_up`.

## Development

The Go module is `github.com/opsbl/virtualsms`. The command entry point is `cmd/virtualsms`; the `internal` packages hold AT testing, batching, configuration, delivery, serial emulation, instrumentation, and process lifecycle.

```sh
make test
make
make musl
```

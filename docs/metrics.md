# Metrics and health

Prometheus metrics are served on `observability.listen` at `/metrics`;
`/healthz` answers while the process runs and `/readyz` once the sources
stream. Labels are limited to `source`, `channel`, `protocol`, `kind`,
`target`, `outcome` and `event`.

| metric | type | meaning |
|---|---|---|
| `pagewire_build_info{version,commit,instance}` | gauge | build information |
| `pagewire_source_info{source,driver,tuner,serial,usb,center_hz,sample_rate}` | gauge | static source description |
| `pagewire_source_up{source}` | gauge | 1 while the source streams, 0 while it is reopened |
| `pagewire_source_restarts_total{source}` | counter | reopens after a failure (USB disconnect, stall) |
| `pagewire_iq_blocks_total{source}` | counter | IQ blocks received |
| `pagewire_iq_dropped_blocks_total{source}` | counter | blocks dropped because processing fell behind |
| `pagewire_iq_clipping_ratio{source}` | gauge | share of ADC samples at full scale |
| `pagewire_pipeline_load_ratio{source}` | gauge | processing time per block over its real-time duration |
| `pagewire_channel_rssi_dbfs{channel}` | gauge | channel power |
| `pagewire_channel_noise_dbfs{channel}` | gauge | noise floor learnt while idle |
| `pagewire_channel_snr_db{channel}` | gauge | power above the floor |
| `pagewire_channel_gate_open{channel}` | gauge | 1 while the squelch is open |
| `pagewire_channel_freq_offset_hz{channel}` | gauge | carrier offset of the last active block |
| `pagewire_decoder_messages_total{channel,protocol,kind}` | counter | decoded messages |
| `pagewire_decoder_events_total{channel,protocol,event}` | counter | sync_acquired, sync_lost, bits_corrected, uncorrectable, invalid_frame |
| `pagewire_queue_depth{target}` | gauge | messages waiting |
| `pagewire_queue_dead_letters{target}` | gauge | messages that exhausted their attempts |
| `pagewire_queue_dropped_total{target}` | counter | discarded because the bound was exceeded |
| `pagewire_sink_deliveries_total{target,outcome}` | counter | delivered, duplicate, filtered, skipped, rejected, error, dead |
| `pagewire_sink_latency_seconds{target}` | histogram | delivery attempt duration |
| `pagewire_sink_healthy{target}` | gauge | 1 while the last attempt succeeded |

## Grafana dashboard

`deploy/grafana/pagewire.json` visualises all metrics above plus the
Go/process collectors (CPU, RSS, goroutines). Import it via
Dashboards > New > Import, or provision it from a file.

- Layout: the top rows show the essentials; the two collapsed "Details" rows hold the
  remaining metrics.
- Filters: host, instance (Prometheus `instance` label = scrape target
  `host:port`), source, channel and target. All allow multi-selection.
- The Prometheus data source is a hidden variable (`datasource`), so it is
  not shown in the filter bar. Change it under Dashboard settings >
  Variables.
- The gateway name from `instance:` in the config is exported as
  `exported_instance` unless Prometheus scrapes with `honor_labels: true`.

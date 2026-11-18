package config

// Example is a documented configuration that exercises every section. It is
// printed by "pagewire config example" and used in tests.
const Example = `# pagewire configuration
# Values of the form ${VAR} or ${VAR:-default} are expanded from the environment.
# Any scalar can also be overridden with PAGEWIRE__SECTION__KEY, e.g. PAGEWIRE__SOURCES__0__RTLSDR__PPM=12.

instance: gw-1                  # unique per instance on a host; used in logs, metrics, {instance}

sources:                        # one or more sample sources; channels refer to them by name
  - name: vhf
    type: rtlsdr                # rtlsdr | rtltcp | audio
    rtlsdr:
      device: ""                # "" = the only dongle; "serial:00000002"; "usb:1-1.3" (physical port,
                                # see "pagewire devices"); "index:0" (unstable, discouraged)
      sample_rate: 0            # 0 = lowest rate that covers all channels of this source
      gain: auto                # auto or dB, e.g. 28.0
      ppm: 0                    # frequency correction, see "calibrate"
      bias_tee: false
      bandwidth: 0              # tuner IF filter in Hz, 0 = automatic (R820T, R828D, E4000, FC2580)
      # if_gains: [6, 9, 3, 2, 3, 3]   # per-stage IF gain in dB, E4000 only
  # - name: remote
  #   type: rtltcp
  #   rtltcp: { address: "sdr-host:1234", gain: auto, ppm: 0 }
  # - name: line-in             # already demodulated input, exactly one channel
  #   type: audio
  #   audio: { backend: pulse, format: s16le, rate: 22050, device: "" }

channels:
  - name: paging
    source: vhf                 # may be omitted when only one source exists
    frequency: 150.000M         # Hz with optional k/M/G suffix
    squelch: auto               # auto or dB above noise floor
    power_gate: true            # skip decoding while the channel is idle
    decoders:
      - protocol: pocsag1200
        pocsag:
          mode: alpha           # alpha | numeric | skyper | standard | auto (heuristic)
          charset: US           # US | FR | DE | DK | SE | SI
          bch_level: 2          # 0 (off) .. 2
          polarity: auto        # auto | normal | inverted
          show_partial: false
          prune_empty: true
          heuristics: false
  - name: flex
    source: vhf
    frequency: 150.100M
    decoders:
      - protocol: flex
        flex:
          show_partial: false   # also emit unfinished fragments
          include_system: false # emit FLEX system messages as alerts
          expand_groups: true   # one message per member capcode of a group page
  - name: trunk
    source: vhf
    frequency: 150.200M
    decoders:
      - protocol: mpt1327       # trunked radio signalling, one status message per codeword
        mpt1327:
          include_system: false # also emit the system codewords (CCSC, DCSC)
      - protocol: gsc           # Golay Sequential Code pages
        gsc:
          include_voice: false  # also emit start/end of voice messages
  - name: selcall
    source: vhf
    frequency: 150.300M
    decoders:
      - protocol: zvei1         # also zvei2, zvei3, dzvei, pzvei, ccir, eea, eia, dualtone
      - protocol: dtmf
        selcall:
          sequence_timeout: 1s  # DTMF: pause that ends a key sequence (audio time)
  - name: fms
    source: vhf
    frequency: 150.400M         # FMS telegrams are 1200 baud AFSK on a narrow FM channel
    decoders:
      - protocol: fms           # no options
  - name: same
    source: vhf
    frequency: 150.500M         # demodulated NFM audio
    decoders:
      - protocol: eas
        eas:
          end_of_message: false # also emit the end-of-message marker (NNNN)

targets:
  - name: main
    type: pagermon
    pagermon:
      url: "https://pagermon.example.org"
      api_key_file: /run/secrets/pagermon_api_key   # or api_key: "…"
      source: "GW-1/{channel}"  # PagerMon "source"; placeholders: {channel} {frequency} {protocol} {hostname} {instance}
      send_function_code: false # append the POCSAG function digit to the address (stock client option)
      use_timestamp: true       # timestamps at the start of a page become the message time (stock client option)
      tone_text: ""             # body for tone-only pages; empty drops them like the stock client
      timeout: 10s
  # - name: legacy
  #   type: pagermon-ingest     # what pagermon-ingest-core with the multimon adapter sent (docs/configuration.md)
  #   pagermon_ingest:
  #     url: "https://pagermon.example.org"
  #     api_key_file: /run/secrets/pagermon_api_key
  #     label: pagermon-ingest  # INGEST_CORE__LABEL, sent as "source"

queue:
  path: /var/lib/pagewire/gw-1.db   # default: pagewire-<instance>.db; must differ per instance
  max_per_target: 100000
  max_attempts: 10              # server errors per message before it becomes a dead letter;
                                # unreachable targets and bad credentials do not consume attempts
  max_backoff: 5m
  auto_replay: true             # re-queue dead letters at start and when the target delivers again
  max_replays: 3                # automatic replays per entry before it waits for "queue replay"
  drain_timeout: 30s            # how long a replay run waits for the queues to empty before it stops

observability:
  listen: ":9464"               # must differ per instance on the same host
  metrics: true
  pprof: false
  log:
    level: info                 # debug | info | warn | error
    format: text                # text | json
`

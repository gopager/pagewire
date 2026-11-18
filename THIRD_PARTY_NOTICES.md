# Third-party notices

pagewire is licensed under the GNU General Public License v3.0 or
later (`LICENSE`). This is the only license that is compatible with everything
the binary is built from: the embedded C sources are GPL-2.0-or-later and
LGPL-2.1-or-later, and several Go dependencies are Apache-2.0, which the GPL
accepts from version 3 on.

## Embedded C sources (`third_party/`)

| Component | Origin | License |
|---|---|---|
| multimon-ng decoders (`third_party/multimon-ng/`, modified; see its README) | https://github.com/EliasOenal/multimon-ng, commit 10c0aeb6 | GPL-2.0-or-later (`COPYING`) |
| `bch.c` (BCH code tables) | multimon-ng | Public domain (Unlicense text in the file header) |
| `cJSON.c`, `cJSON.h` | multimon-ng (Dave Gamble and contributors) | MIT (file header) |
| librtlsdr (`third_party/librtlsdr/`, one function added; see its README) | https://gitea.osmocom.org/sdr/rtl-sdr, commit 797f8143 | GPL-2.0-or-later (`COPYING`) |
| libusb 1.0.30 (`third_party/libusb/`, unmodified) | https://github.com/libusb/libusb | LGPL-2.1-or-later (`COPYING`) |

## Go modules linked into the binary

| Module | License |
|---|---|
| github.com/alecthomas/kong, github.com/alecthomas/mango-kong | MIT |
| github.com/muesli/mango, github.com/muesli/roff | MIT |
| github.com/knadh/koanf (v2, maps, parsers/yaml, providers/env, providers/rawbytes) | MIT |
| github.com/go-viper/mapstructure/v2 | MIT |
| github.com/mitchellh/copystructure, github.com/mitchellh/reflectwalk | MIT |
| github.com/jfreymuth/pulse | MIT |
| go.etcd.io/bbolt | MIT |
| github.com/beorn7/perks, github.com/cespare/xxhash/v2 | MIT |
| github.com/prometheus/client_golang, client_model, common | Apache-2.0 |
| github.com/munnerz/goautoneg | BSD-3-Clause |
| golang.org/x/sync, golang.org/x/sys, google.golang.org/protobuf | BSD-3-Clause |
| gopkg.in/yaml.v3, go.yaml.in/yaml/v3 | MIT and Apache-2.0 (see the modules' LICENSE files) |

The list follows `go list -deps ./cmd/pagewire`; module license texts
are in the Go module cache and in the modules' repositories.

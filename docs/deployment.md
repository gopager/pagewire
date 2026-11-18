# Deployment

## Binaries

Static Linux binaries for amd64, arm64 and arm/v7 (Raspberry Pi 2/3) are
built with `make binaries` (docker buildx) and published with each release;
macOS builds run natively with `make build`. No shared libraries are needed
at run time; the USB driver is compiled in.

## Windows

The Windows build uses libusb's WinUSB backend. Install the WinUSB driver
for the dongle once with Zadig (select the RTL2838 device, driver WinUSB);
afterwards `pagewire.exe devices` lists it. The service model
(systemd, udev) does not apply; run the executable from a scheduled task or
a service wrapper of your choice.

## Permissions

Copy `deploy/udev/60-pagewire-rtlsdr.rules` to `/etc/udev/rules.d/`
and add the service user to `plugdev`. The gateway detaches the DVB kernel
driver itself; blacklisting `dvb_usb_rtl28xxu` is optional.

## systemd

`deploy/systemd/pagewire@.service` runs one instance per unit
instance name, reading `/etc/pagewire/<name>.yaml` and keeping the
queue under `/var/lib/pagewire/`. Several instances may run on one
host with distinct queue paths and metrics ports.

## OpenRC (Alpine)

`deploy/openrc/pagewire` runs the gateway under `supervise-daemon`
with automatic respawn, reading `/etc/pagewire/pagewire.yaml`
and exporting the variables in `/etc/pagewire/pagewire.env`
(for `${VAR}` references such as API keys) before the start:

    cp deploy/openrc/pagewire /etc/init.d/
    rc-update add pagewire default
    rc-service pagewire start      # log: /var/log/pagewire.log

## Docker

`deploy/compose.yml` shows a container with the dongle passed through via
`/dev/bus/usb` and the queue in a volume. Images are multi-arch.

## Resource use

The cost is dominated by the capture sample rate, which is the lowest rate
that covers all channels of a source, and by the number of channels; it is
independent of the number of targets. Channels that are close together
share one dongle cheaply, widely spaced ones raise the sample rate.
`pagewire_pipeline_load_ratio` shows the processing time per block relative
to its real-time duration at run time; it must stay well below 1.

## USB disconnects

If the dongle disappears or stops delivering samples for ten seconds, the
source is closed and reopened with back-off (2–30 s) while delivery keeps
running; `/readyz` reports the source as down meanwhile and
`pagewire_source_restarts_total` counts recoveries. The first open at start must
succeed so that configuration errors are visible.

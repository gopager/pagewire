# libusb (vendored)

Unmodified sources of libusb 1.0.30 (https://github.com/libusb/libusb,
release tag `v1.0.30`, 2026-05-17), licensed under the LGPL-2.1-or-later
(see `COPYING`).

Only the files needed to build the Linux (usbfs + netlink hotplug), macOS
(IOKit) and Windows (WinUSB/UsbDk) backends are included. The platform
backends live untouched in `os/` and are pulled into the build through the
`backend_*.c` wrapper files, because cgo compiles every `.c` file in a package
directory regardless of the target OS. The Windows backend files are wrapped
one per translation unit since they define static functions of the same
name; `windows_hotplug.c` is left out like in upstream's default build
(`--enable-windows-hotplug` is off), the gateway reopens devices itself. The `config/<os>/config.h` headers
replace the autoconf-generated `config.h`.

To update: replace the files with the new release, keep the wrapper and
config files, and bump the version here.

# librtlsdr (vendored)

Library sources of rtl-sdr (https://github.com/osmocom/rtl-sdr),
commit `797f8143266d983c56d8f35d2d442527529dd8a5` (2026-08-11), licensed under
the GPL-2.0-or-later (see `COPYING`). Only the library (`librtlsdr.c`, the
tuner drivers and the public headers) is included; the command line tools
are not.

Built with `DETACH_KERNEL_DRIVER=1` so the DVB kernel driver is detached
automatically on Linux and no module blacklist is required.

## Local modifications

- `rtlsdr_get_device_usb_location()` (librtlsdr.c, rtl-sdr.h): returns the USB
  bus number and port chain of an enumerated device so that dongles can be
  selected by physical socket instead of by enumeration index.

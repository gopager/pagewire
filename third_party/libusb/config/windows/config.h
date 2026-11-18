/* Hand-written replacement for the autoconf-generated config.h (Windows, MinGW/zig). */
#ifndef LIBUSB_CONFIG_H
#define LIBUSB_CONFIG_H
#define DEFAULT_VISIBILITY
#define PRINTF_FORMAT(a, b) __attribute__ ((__format__ (__printf__, a, b)))
#define PLATFORM_WINDOWS 1
#define ENABLE_LOGGING 1
#define HAVE_STRUCT_TIMESPEC 1
#define _WIN32_WINNT 0x0601
#endif

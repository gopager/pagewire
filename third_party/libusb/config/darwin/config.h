/* Hand-written replacement for the autoconf-generated config.h (macOS). */
#ifndef LIBUSB_CONFIG_H
#define LIBUSB_CONFIG_H
#include <AvailabilityMacros.h>
#define _GNU_SOURCE 1
#define DEFAULT_VISIBILITY __attribute__ ((visibility ("hidden")))
#define PRINTF_FORMAT(a, b) __attribute__ ((__format__ (__printf__, a, b)))
#define PLATFORM_POSIX 1
#define ENABLE_LOGGING 1
#define HAVE_CLOCK_GETTIME 1
#define HAVE_NFDS_T 1
#define HAVE_PTHREAD_THREADID_NP 1
#define HAVE_SYS_TIME_H 1
#endif

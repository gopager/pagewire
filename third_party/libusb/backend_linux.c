/* cgo compiles every .c file in this directory on every OS, so the
 * OS-specific libusb backends are wrapped in preprocessor guards here
 * and the upstream files in os/ stay untouched. */
#ifdef __linux__
#include "os/events_posix.c"
#include "os/threads_posix.c"
#include "os/linux_usbfs.c"
#include "os/linux_netlink.c"
#endif

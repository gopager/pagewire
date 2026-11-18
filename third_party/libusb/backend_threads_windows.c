/* See backend_linux.c for why the backend is wrapped; each Windows backend
 * file is its own translation unit because they share static names. */
#ifdef _WIN32
#include "os/threads_windows.c"
#endif

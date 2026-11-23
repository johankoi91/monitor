//go:build linux && agora

package agoranative

/*
#cgo CFLAGS: -D_GNU_SOURCE
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
static int route_capture_status(void) {
 int (*status)(void)=(int (*)(void))dlsym(RTLD_DEFAULT,"rtc_route_capture_status");
 return status?status():-1;
}
*/
import "C"

func captureStatus() int { return int(C.route_capture_status()) }

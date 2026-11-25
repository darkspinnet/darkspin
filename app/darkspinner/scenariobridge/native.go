//go:build windows && cgo && scenario

package scenariobridge

/*
#cgo CFLAGS: -DFANG_SCENARIO=1 -I${SRCDIR}/../../fang
#include "proxy.h"
*/
import "C"

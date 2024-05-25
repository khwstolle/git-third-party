//go:build cgo

package main

// C ABI shim for the in-process bindings (Python ctypes, Node koffi). Each
// gtp_* function takes a JSON request, delegates to one of the per-command
// dispatchers in internal/core, and returns the JSON response as a heap
// C string. The caller owns the returned pointer and must release it with
// gtp_free.

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"github.com/khwstolle/git-third-party/internal/core"
)

func runWrapped(cReq *C.char, dispatch core.DispatchFunc) *C.char {
	if cReq == nil {
		return C.CString(`{"exit_code":1,"error":"bridge: nil request"}`)
	}
	return C.CString(core.RunBridgeJSON(C.GoString(cReq), dispatch))
}

//export gtp_add
func gtp_add(req *C.char) *C.char { return runWrapped(req, core.DispatchAdd) }

//export gtp_set
func gtp_set(req *C.char) *C.char { return runWrapped(req, core.DispatchSet) }

//export gtp_unset
func gtp_unset(req *C.char) *C.char { return runWrapped(req, core.DispatchUnset) }

//export gtp_update
func gtp_update(req *C.char) *C.char { return runWrapped(req, core.DispatchUpdate) }

//export gtp_list
func gtp_list(req *C.char) *C.char { return runWrapped(req, core.DispatchList) }

//export gtp_remove
func gtp_remove(req *C.char) *C.char { return runWrapped(req, core.DispatchRemove) }

//export gtp_rename
func gtp_rename(req *C.char) *C.char { return runWrapped(req, core.DispatchRename) }

//export gtp_save_patch
func gtp_save_patch(req *C.char) *C.char { return runWrapped(req, core.DispatchSavePatch) }

//export gtp_diff_patch
func gtp_diff_patch(req *C.char) *C.char { return runWrapped(req, core.DispatchDiffPatch) }

//export gtp_info
func gtp_info(req *C.char) *C.char { return runWrapped(req, core.DispatchInfo) }

//export gtp_init
func gtp_init(req *C.char) *C.char { return runWrapped(req, core.DispatchInit) }

//export gtp_version
func gtp_version() *C.char { return C.CString(core.Version) }

//export gtp_free
func gtp_free(p *C.char) { C.free(unsafe.Pointer(p)) }

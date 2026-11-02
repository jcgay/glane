//go:build ios

package main

/*
#include <stdlib.h>
*/
import "C"

import "github.com/jcgay/glane/internal/web"

// GlaneOpen serves the database at path from now on. It returns NULL, or why
// it can't, for the caller to free.
//
//export GlaneOpen
func GlaneOpen(path *C.char) *C.char {
	h, err := web.ReadOnlyHandler(C.GoString(path))
	if err != nil {
		return C.CString(err.Error())
	}
	mu.Lock()
	handler = h
	mu.Unlock()
	return nil
}

// GlaneRequest answers one request (see request and response), for the
// caller to free.
//
//export GlaneRequest
func GlaneRequest(req *C.char) *C.char {
	return C.CString(string(serve([]byte(C.GoString(req)))))
}

//go:build android

package main

import "errors"

// glaze's purego needs cgo on Android, and the APK has its own UI anyway.
func cmdApp([]string) { fatal(errors.New("glane app is not available on Android")) }

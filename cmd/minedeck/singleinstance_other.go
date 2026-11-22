//go:build !windows

package main

func ensureSingleInstance(string, bool) (func(), bool) { return func() {}, true }

//go:build !windows

package main

func ensureSingleInstance(string) (func(), bool) { return func() {}, true }

package main

// main is required for package main but never runs: this binary is built
// with -buildmode=c-shared and entered through the //export functions in
// cgo_bindings.go.
func main() {}
